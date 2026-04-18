"""Aurion Agent Runtime - FastAPI Application.

REST + SSE API implementing the Managed Agents protocol.
All endpoints match Anthropic's API shape (managed-agents-2026-04-01).
PostgreSQL persistence, API key auth, agent versioning, resources API.
"""

from __future__ import annotations

import asyncio
import os
import time
import uuid
from collections import defaultdict
from contextlib import asynccontextmanager
from datetime import datetime
from typing import Any, AsyncIterator

import structlog
from fastapi import Depends, FastAPI, HTTPException, Query, Request, UploadFile
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse, StreamingResponse, Response

from packages.core.auth import (
    generate_api_key,
    get_current_api_key,
    is_public_path,
    AUTH_ENABLED,
)
from packages.core.database import Repository
from packages.core.event_bus import EventBus
from packages.core.llm_providers import LLMProvider
from packages.core.models import (
    Agent,
    AgentCreate,
    AgentUpdate,
    AgentVersion,
    APIKey,
    DeletedFile,
    DeletedResource,
    DeletedSession,
    Environment,
    EnvironmentCreate,
    Event,
    EventSend,
    EventType,
    FileResource,
    FileResourceCreate,
    FileUpload,
    GitHubRepositoryResource,
    GitHubRepositoryResourceCreate,
    ModelConfig,
    ResourceCreateParams,
    Session,
    SessionCreate,
    SessionResource,
    SessionStats,
    SessionStatus,
    SessionUpdate,
    SessionUsage,
    StopReason,
)
from packages.core.session import SessionOrchestrator
from packages.core.tool_executor import ToolExecutor
from packages.core.file_storage import FileStorage
from packages.core.billing import compute_session_cost, compute_usage_summary
from packages.memory import MemoryStore
from packages.mcp import McpConnector
from packages.observability import ObservabilityTracer
from packages.skills import SkillsLoader

logger = structlog.get_logger()

# -- Shared services --
repo = Repository()
event_bus = EventBus(redis_url=os.environ.get("REDIS_URL", "redis://localhost:6379/0"))
memory_store = MemoryStore()
observability = ObservabilityTracer()
skills_loader = SkillsLoader()
file_storage = FileStorage()
orchestrators: dict[str, SessionOrchestrator] = {}


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifecycle."""
    await repo.init_db()
    await event_bus.connect()
    await memory_store.connect()
    await observability.connect()
    await skills_loader.load_all()
    logger.info("aurion_runtime_started")
    yield
    for orch in orchestrators.values():
        await orch.stop()
    await event_bus.close()
    await memory_store.close()
    await observability.close()
    logger.info("aurion_runtime_stopped")


app = FastAPI(
    title="Aurion Agent Runtime",
    description="Open-source Managed Agents runtime - compatible with Anthropic's protocol",
    version="1.0.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=os.environ.get("CORS_ORIGINS", "*").split(","),
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


# ======================================================================
# Beta header middleware
# ======================================================================

BETA_HEADER = "anthropic-beta"
EXPECTED_BETA = "managed-agents-2026-04-01"


@app.middleware("http")
async def beta_header_middleware(request: Request, call_next):
    """Validate the anthropic-beta header on /v1/ endpoints.

    Accepts requests with the correct beta header, or no beta header at all
    (for backwards compatibility). Rejects requests with an incorrect beta value.
    """
    if request.url.path.startswith("/v1/"):
        beta_value = request.headers.get(BETA_HEADER)
        if beta_value is not None and EXPECTED_BETA not in beta_value:
            from fastapi.responses import JSONResponse
            return JSONResponse(
                status_code=400,
                content={"error": f"Unsupported beta version: {beta_value}. Expected: {EXPECTED_BETA}"},
            )
    response = await call_next(request)
    return response


# ======================================================================
# Rate limiting middleware (token bucket per client IP)
# ======================================================================

# Configurable via env vars
RATE_LIMIT_RPM = int(os.environ.get("RATE_LIMIT_RPM", "120"))  # requests per minute
RATE_LIMIT_BURST = int(os.environ.get("RATE_LIMIT_BURST", "20"))  # burst capacity
RATE_LIMIT_ENABLED = os.environ.get("RATE_LIMIT_ENABLED", "true").lower() != "false"


class _TokenBucket:
    """Simple token bucket rate limiter — one bucket per client key."""

    __slots__ = ("_buckets", "_rate", "_burst")

    def __init__(self, rate_per_second: float, burst: int):
        self._rate = rate_per_second
        self._burst = burst
        # {key: (tokens, last_refill_time)}
        self._buckets: dict[str, tuple[float, float]] = {}

    def allow(self, key: str) -> bool:
        now = time.monotonic()
        tokens, last = self._buckets.get(key, (float(self._burst), now))

        # Refill tokens based on elapsed time
        elapsed = now - last
        tokens = min(self._burst, tokens + elapsed * self._rate)

        if tokens >= 1.0:
            self._buckets[key] = (tokens - 1.0, now)
            return True

        self._buckets[key] = (tokens, now)
        return False

    def cleanup(self, max_age: float = 300.0) -> None:
        """Remove stale entries older than max_age seconds."""
        now = time.monotonic()
        stale = [k for k, (_, t) in self._buckets.items() if now - t > max_age]
        for k in stale:
            del self._buckets[k]


_rate_limiter = _TokenBucket(
    rate_per_second=RATE_LIMIT_RPM / 60.0,
    burst=RATE_LIMIT_BURST,
)


@app.middleware("http")
async def rate_limit_middleware(request: Request, call_next):
    """Per-IP token bucket rate limiter for /v1/ endpoints.

    Returns 429 Too Many Requests with Retry-After header when exceeded.
    Disabled by setting RATE_LIMIT_ENABLED=false.
    """
    if not RATE_LIMIT_ENABLED or not request.url.path.startswith("/v1/"):
        return await call_next(request)

    # Use client IP (or API key prefix if authenticated) as bucket key
    client_ip = request.client.host if request.client else "unknown"
    auth_header = request.headers.get("x-api-key", "")
    bucket_key = auth_header[:12] if auth_header else client_ip

    if not _rate_limiter.allow(bucket_key):
        retry_after = 60.0 / RATE_LIMIT_RPM  # seconds until next token
        return JSONResponse(
            status_code=429,
            content={
                "error": "rate_limit_exceeded",
                "message": f"Rate limit exceeded. Max {RATE_LIMIT_RPM} requests/minute.",
                "retry_after": round(retry_after, 2),
            },
            headers={"Retry-After": str(int(retry_after) + 1)},
        )

    response = await call_next(request)

    # Add rate limit headers
    response.headers["X-RateLimit-Limit"] = str(RATE_LIMIT_RPM)
    return response


# ======================================================================
# AUTH dependency
# ======================================================================

async def require_auth(request: Request):
    """Dependency that enforces API key auth on non-public paths."""
    if is_public_path(request.url.path):
        return None
    return await get_current_api_key(request)


# ======================================================================
# AGENTS CRUD + Versioning + Archive
# ======================================================================

@app.post("/v1/agents", response_model=Agent, dependencies=[Depends(require_auth)])
async def create_agent(body: AgentCreate) -> Agent:
    agent = Agent(
        name=body.name,
        model=body.model,
        system=body.system,
        description=body.description,
        tools=body.tools,
        mcp_servers=body.mcp_servers,
        skills=body.skills,
        callable_agents=body.callable_agents,
        metadata=body.metadata,
    )
    await repo.create_agent(agent)
    # Save version 1 snapshot
    await repo.create_agent_version(AgentVersion(
        id=f"av_{uuid.uuid4().hex[:24]}",
        agent_id=agent.id,
        version=1,
        snapshot=agent.model_dump(mode="json"),
    ))
    logger.info("agent_created", id=agent.id, name=agent.name)
    return agent


@app.get("/v1/agents", response_model=list[Agent], dependencies=[Depends(require_auth)])
async def list_agents() -> list[Agent]:
    return await repo.list_agents()


@app.get("/v1/agents/{agent_id}", response_model=Agent, dependencies=[Depends(require_auth)])
async def get_agent(agent_id: str) -> Agent:
    agent = await repo.get_agent(agent_id)
    if not agent:
        raise HTTPException(404, "Agent not found")
    return agent


@app.post("/v1/agents/{agent_id}", response_model=Agent, dependencies=[Depends(require_auth)])
async def update_agent(agent_id: str, body: AgentUpdate) -> Agent:
    """Update an agent. Requires current version for optimistic concurrency."""
    agent = await repo.get_agent(agent_id)
    if not agent:
        raise HTTPException(404, "Agent not found")
    if agent.version != body.version:
        raise HTTPException(409, f"Version conflict: expected {agent.version}, got {body.version}")

    # Apply updates (Anthropic merge semantics)
    update_data = body.model_dump(exclude_unset=True, exclude={"version"})

    # Handle metadata merge: empty string values delete keys
    if "metadata" in update_data and update_data["metadata"] is not None:
        merged = dict(agent.metadata)
        for k, v in update_data["metadata"].items():
            if v == "":
                merged.pop(k, None)
            else:
                merged[k] = v
        update_data["metadata"] = merged

    for key, value in update_data.items():
        if value is not None:
            setattr(agent, key, value)

    agent.version += 1
    agent.updated_at = datetime.utcnow()
    await repo.update_agent(agent)

    # Save version snapshot
    await repo.create_agent_version(AgentVersion(
        id=f"av_{uuid.uuid4().hex[:24]}",
        agent_id=agent.id,
        version=agent.version,
        snapshot=agent.model_dump(mode="json"),
    ))
    logger.info("agent_updated", id=agent.id, version=agent.version)
    return agent


@app.delete("/v1/agents/{agent_id}", dependencies=[Depends(require_auth)])
async def delete_agent(agent_id: str):
    agent = await repo.get_agent(agent_id)
    if not agent:
        raise HTTPException(404, "Agent not found")
    await repo.delete_agent(agent_id)
    return {"deleted": True}


@app.post("/v1/agents/{agent_id}/archive", response_model=Agent, dependencies=[Depends(require_auth)])
async def archive_agent(agent_id: str) -> Agent:
    agent = await repo.archive_agent(agent_id)
    if not agent:
        raise HTTPException(404, "Agent not found")
    logger.info("agent_archived", id=agent_id)
    return agent


@app.get("/v1/agents/{agent_id}/versions", response_model=list[AgentVersion], dependencies=[Depends(require_auth)])
async def list_agent_versions(agent_id: str) -> list[AgentVersion]:
    agent = await repo.get_agent(agent_id)
    if not agent:
        raise HTTPException(404, "Agent not found")
    return await repo.list_agent_versions(agent_id)


@app.get("/v1/agents/{agent_id}/versions/{version}", response_model=AgentVersion, dependencies=[Depends(require_auth)])
async def get_agent_version(agent_id: str, version: int) -> AgentVersion:
    av = await repo.get_agent_version(agent_id, version)
    if not av:
        raise HTTPException(404, "Agent version not found")
    return av


# ======================================================================
# ENVIRONMENTS CRUD
# ======================================================================

@app.post("/v1/environments", response_model=Environment, dependencies=[Depends(require_auth)])
async def create_environment(body: EnvironmentCreate) -> Environment:
    env = Environment(
        name=body.name,
        config=body.config,
        sandbox_provider=body.sandbox_provider,
        packages=body.packages,
        metadata=body.metadata,
    )
    await repo.create_environment(env)
    return env


@app.get("/v1/environments", response_model=list[Environment], dependencies=[Depends(require_auth)])
async def list_environments() -> list[Environment]:
    return await repo.list_environments()


@app.get("/v1/environments/{env_id}", response_model=Environment, dependencies=[Depends(require_auth)])
async def get_environment(env_id: str) -> Environment:
    env = await repo.get_environment(env_id)
    if not env:
        raise HTTPException(404, "Environment not found")
    return env


@app.delete("/v1/environments/{env_id}", dependencies=[Depends(require_auth)])
async def delete_environment(env_id: str):
    if not await repo.delete_environment(env_id):
        raise HTTPException(404, "Environment not found")
    return {"deleted": True}


# ======================================================================
# SESSIONS
# ======================================================================

@app.post("/v1/sessions", response_model=Session, dependencies=[Depends(require_auth)])
async def create_session(body: SessionCreate) -> Session:
    # Resolve agent: can be a string ID or {id, type, version} object
    if isinstance(body.agent, str):
        agent = await repo.get_agent(body.agent)
        if not agent:
            raise HTTPException(404, "Agent not found")
    elif isinstance(body.agent, dict):
        agent_id = body.agent.get("id", "")
        agent = await repo.get_agent(agent_id)
        if not agent:
            raise HTTPException(404, "Agent not found")
        # If version is specified, pin to that agent version snapshot
        requested_version = body.agent.get("version")
        if requested_version is not None:
            av = await repo.get_agent_version(agent_id, int(requested_version))
            if not av:
                raise HTTPException(404, f"Agent version {requested_version} not found")
            agent = Agent(**av.snapshot)
    else:
        raise HTTPException(400, "Invalid agent reference")

    env = await repo.get_environment(body.environment_id)
    if not env:
        raise HTTPException(404, "Environment not found")

    session = Session(
        agent=agent,
        agent_id=agent.id,
        environment_id=body.environment_id,
        title=body.title,
        vault_ids=body.vault_ids,
        metadata=body.metadata,
    )
    await repo.create_session(session)
    logger.info("session_created", id=session.id, agent=agent.id)
    return session


@app.get("/v1/sessions", response_model=list[Session], dependencies=[Depends(require_auth)])
async def list_sessions(agent_id: str | None = Query(None)) -> list[Session]:
    return await repo.list_sessions(agent_id=agent_id)


@app.get("/v1/sessions/{session_id}", response_model=Session, dependencies=[Depends(require_auth)])
async def get_session(session_id: str) -> Session:
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")
    return session


@app.post("/v1/sessions/{session_id}", response_model=Session, dependencies=[Depends(require_auth)])
async def update_session(session_id: str, body: SessionUpdate) -> Session:
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")
    if body.title is not None:
        session.title = body.title
    if body.metadata is not None:
        merged = dict(session.metadata)
        for k, v in body.metadata.items():
            if v == "":
                merged.pop(k, None)
            else:
                merged[k] = v
        session.metadata = merged
    session.updated_at = datetime.utcnow()
    await repo.update_session(session)
    return session


@app.delete("/v1/sessions/{session_id}", dependencies=[Depends(require_auth)])
async def delete_session(session_id: str):
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")

    # Stop orchestrator if running
    if session_id in orchestrators:
        await orchestrators[session_id].stop()
        del orchestrators[session_id]

    # Emit session.deleted event
    seq = await repo.get_max_sequence(session_id) + 1
    event = event_bus.create_event(
        session_id=session_id,
        event_type=EventType.SESSION_DELETED,
        payload={},
        sequence_num=seq,
    )
    await event_bus.emit(session_id, event)
    await repo.create_event(event)

    await repo.delete_session(session_id)
    return DeletedSession(id=session_id)


@app.post("/v1/sessions/{session_id}/archive", response_model=Session, dependencies=[Depends(require_auth)])
async def archive_session(session_id: str) -> Session:
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")
    session.archived_at = datetime.utcnow()
    await repo.update_session(session)
    return session


# ======================================================================
# EVENTS - Send + Stream + List
# ======================================================================

@app.post("/v1/sessions/{session_id}/events", dependencies=[Depends(require_auth)])
async def send_event(session_id: str, body: EventSend):
    """Send events to a session (user message, interrupt, tool confirmation, etc.)."""
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")

    agent = session.agent
    if not agent:
        raise HTTPException(500, "Session has no resolved agent")

    env = await repo.get_environment(session.environment_id)
    if not env:
        raise HTTPException(500, "Session environment not found")

    for event_params in body.events:
        if event_params.type == "user.interrupt":
            # Handle interrupt: stop the current orchestrator
            if session_id in orchestrators:
                await orchestrators[session_id].interrupt()
            # Emit the interrupt event
            seq = await repo.get_max_sequence(session_id) + 1
            evt = event_bus.create_event(
                session_id=session_id,
                event_type=EventType.USER_INTERRUPT,
                payload={},
                sequence_num=seq,
            )
            await event_bus.emit(session_id, evt)
            await repo.create_event(evt)

        elif event_params.type == "user.message":
            # Get or create orchestrator
            if session_id not in orchestrators:
                llm = LLMProvider()
                tool_executor = ToolExecutor(
                    tavily_api_key=os.environ.get("TAVILY_API_KEY"),
                )
                orch = SessionOrchestrator(
                    agent=agent,
                    environment=env,
                    session=session,
                    llm=llm,
                    tool_executor=tool_executor,
                    event_bus=event_bus,
                    repository=repo,
                )
                orchestrators[session_id] = orch

            orch = orchestrators[session_id]

            # Extract content from the typed params
            content_parts = []
            for block in event_params.content:
                if hasattr(block, "text"):
                    content_parts.append(block.text)
                elif isinstance(block, dict) and "text" in block:
                    content_parts.append(block["text"])
            content_str = "\n".join(content_parts)

            # Process async
            asyncio.create_task(orch.process_message(content=content_str))

        elif event_params.type == "user.tool_confirmation":
            if session_id in orchestrators:
                await orchestrators[session_id].handle_tool_confirmation(
                    tool_use_id=event_params.tool_use_id,
                    result=event_params.result,
                    deny_message=event_params.deny_message,
                )
            seq = await repo.get_max_sequence(session_id) + 1
            evt = event_bus.create_event(
                session_id=session_id,
                event_type=EventType.USER_TOOL_CONFIRMATION,
                payload=event_params.model_dump(),
                sequence_num=seq,
            )
            await event_bus.emit(session_id, evt)
            await repo.create_event(evt)

        elif event_params.type == "user.custom_tool_result":
            if session_id in orchestrators:
                await orchestrators[session_id].handle_custom_tool_result(
                    custom_tool_use_id=event_params.custom_tool_use_id,
                    content=event_params.content,
                    is_error=event_params.is_error,
                )
            seq = await repo.get_max_sequence(session_id) + 1
            evt = event_bus.create_event(
                session_id=session_id,
                event_type=EventType.USER_CUSTOM_TOOL_RESULT,
                payload=event_params.model_dump(),
                sequence_num=seq,
            )
            await event_bus.emit(session_id, evt)
            await repo.create_event(evt)

        elif event_params.type == "user.define_outcome":
            # Record the user-defined outcome for the session
            seq = await repo.get_max_sequence(session_id) + 1
            evt = event_bus.create_event(
                session_id=session_id,
                event_type=EventType.USER_DEFINE_OUTCOME,
                payload=event_params.model_dump(),
                sequence_num=seq,
            )
            await event_bus.emit(session_id, evt)
            await repo.create_event(evt)

    return {"status": "ok"}


@app.get("/v1/sessions/{session_id}/events/stream", dependencies=[Depends(require_auth)])
async def stream_events(session_id: str, request: Request):
    """SSE endpoint - stream session events in real-time."""
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")

    async def event_generator() -> AsyncIterator[str]:
        async for event in event_bus.subscribe(session_id):
            if await request.is_disconnected():
                break
            yield f"event: {event.type}\ndata: {event.model_dump_json()}\n\n"

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


@app.get("/v1/sessions/{session_id}/events", response_model=list[Event], dependencies=[Depends(require_auth)])
async def list_events(
    session_id: str,
    limit: int = Query(100, ge=1, le=1000),
    after_id: str | None = Query(None),
) -> list[Event]:
    """List events for a session with cursor-based pagination."""
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")
    return await repo.list_events(session_id, limit=limit, after_id=after_id)


# ======================================================================
# RESOURCES - Files + GitHub repos mounted into sessions
# ======================================================================

@app.post("/v1/sessions/{session_id}/resources", dependencies=[Depends(require_auth)])
async def add_resource(session_id: str, body: ResourceCreateParams):
    """Add a resource (file or GitHub repo) to a session."""
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")

    if isinstance(body, FileResourceCreate):
        resource = FileResource(
            file_id=body.file_id,
            mount_path=body.mount_path,
        )
    elif isinstance(body, GitHubRepositoryResourceCreate):
        resource = GitHubRepositoryResource(
            url=body.url,
            mount_path=body.mount_path,
            checkout=body.checkout,
            sparse_checkout_directories=body.sparse_checkout_directories,
        )
    else:
        raise HTTPException(400, f"Unsupported resource type")

    await repo.create_resource(session_id, resource)
    return resource


@app.get("/v1/sessions/{session_id}/resources", dependencies=[Depends(require_auth)])
async def list_resources(session_id: str):
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")
    return await repo.list_resources(session_id)


@app.get("/v1/sessions/{session_id}/resources/{resource_id}", dependencies=[Depends(require_auth)])
async def get_resource(session_id: str, resource_id: str):
    resource = await repo.get_resource(resource_id)
    if not resource:
        raise HTTPException(404, "Resource not found")
    return resource


@app.post("/v1/sessions/{session_id}/resources/{resource_id}", dependencies=[Depends(require_auth)])
async def update_resource(session_id: str, resource_id: str, body: dict[str, Any]):
    """Update an existing resource (e.g. change checkout branch)."""
    session = await repo.get_session(session_id)
    if not session:
        raise HTTPException(404, "Session not found")
    resource = await repo.get_resource(resource_id)
    if not resource:
        raise HTTPException(404, "Resource not found")

    # Apply partial updates
    update_data = {k: v for k, v in body.items() if v is not None and k not in ("id", "type")}
    for key, value in update_data.items():
        if hasattr(resource, key):
            setattr(resource, key, value)
    resource.updated_at = datetime.utcnow()
    await repo.update_resource(resource)
    return resource


@app.delete("/v1/sessions/{session_id}/resources/{resource_id}", dependencies=[Depends(require_auth)])
async def delete_resource(session_id: str, resource_id: str):
    if not await repo.delete_resource(resource_id):
        raise HTTPException(404, "Resource not found")
    return DeletedResource(id=resource_id)


# ======================================================================
# FILES - Upload, download, list, delete (matching Anthropic /v1/files)
# ======================================================================

# Max file size: 512 MB
MAX_FILE_SIZE = int(os.environ.get("MAX_FILE_SIZE_BYTES", str(512 * 1024 * 1024)))


@app.post("/v1/files", response_model=FileUpload, dependencies=[Depends(require_auth)])
async def upload_file(
    file: UploadFile,
    purpose: str = Query("session_resource"),
):
    """Upload a file. Returns file metadata with an ID for use in resources."""
    if not file.filename:
        raise HTTPException(400, "Filename is required")

    # Read file content with size limit
    data = await file.read()
    if len(data) > MAX_FILE_SIZE:
        raise HTTPException(
            413,
            f"File too large. Maximum size: {MAX_FILE_SIZE} bytes",
        )

    file_obj = FileUpload(
        filename=file.filename,
        content_type=file.content_type or "application/octet-stream",
        size_bytes=len(data),
        purpose=purpose,
    )

    # Save to disk
    storage_path = await file_storage.save(file_obj.id, data)

    # Persist metadata
    await repo.create_file(file_obj, storage_path)

    logger.info("file_uploaded", id=file_obj.id, filename=file.filename, size=len(data))
    return file_obj


@app.get("/v1/files", response_model=list[FileUpload], dependencies=[Depends(require_auth)])
async def list_files(purpose: str | None = Query(None)):
    """List all uploaded files, optionally filtered by purpose."""
    return await repo.list_files(purpose=purpose)


@app.get("/v1/files/{file_id}", response_model=FileUpload, dependencies=[Depends(require_auth)])
async def get_file(file_id: str):
    """Get file metadata by ID."""
    result = await repo.get_file(file_id)
    if not result:
        raise HTTPException(404, "File not found")
    file_obj, _ = result
    return file_obj


@app.get("/v1/files/{file_id}/content", dependencies=[Depends(require_auth)])
async def download_file(file_id: str):
    """Download file content."""
    result = await repo.get_file(file_id)
    if not result:
        raise HTTPException(404, "File not found")
    file_obj, storage_path = result

    try:
        data = await file_storage.read(storage_path)
    except FileNotFoundError:
        raise HTTPException(404, "File content not found on disk")
    except PermissionError:
        raise HTTPException(403, "Access denied")

    return Response(
        content=data,
        media_type=file_obj.content_type,
        headers={
            "Content-Disposition": f'attachment; filename="{file_obj.filename}"',
            "Content-Length": str(len(data)),
        },
    )


@app.delete("/v1/files/{file_id}", dependencies=[Depends(require_auth)])
async def delete_file(file_id: str):
    """Delete an uploaded file (metadata + content)."""
    storage_path = await repo.delete_file(file_id)
    if storage_path is None:
        raise HTTPException(404, "File not found")

    # Clean up disk
    await file_storage.delete(storage_path)

    logger.info("file_deleted", id=file_id)
    return DeletedFile(id=file_id)


# ======================================================================
# BILLING & USAGE - Per-session cost + aggregate usage
# ======================================================================

@app.get("/v1/sessions/{session_id}/usage", dependencies=[Depends(require_auth)])
async def get_session_usage(session_id: str):
    """Get token usage and computed cost for a specific session."""
    session_data = await repo.get_session_usage(session_id)
    if not session_data:
        raise HTTPException(404, "Session not found")

    cost = compute_session_cost(
        session_id=session_id,
        model_id=session_data.get("model_id", ""),
        usage=session_data.get("usage", {}),
    )
    return cost


@app.get("/v1/usage", dependencies=[Depends(require_auth)])
async def get_usage_summary(agent_id: str | None = Query(None)):
    """Get aggregate usage and cost across all sessions.

    Optionally filter by agent_id.
    """
    sessions_data = await repo.get_all_sessions_usage(agent_id=agent_id)
    summary = compute_usage_summary(sessions_data)
    return summary


# ======================================================================
# API KEYS Management
# ======================================================================

@app.post("/v1/api-keys", dependencies=[Depends(require_auth)])
async def create_api_key_endpoint(body: dict[str, str]):
    """Create a new API key. Returns the raw key ONCE - store it safely."""
    name = body.get("name", "default")
    raw_key, prefix, key_hash = generate_api_key()
    key = APIKey(name=name, key_hash=key_hash, prefix=prefix)
    await repo.create_api_key(key)
    return {"id": key.id, "name": name, "key": raw_key, "prefix": prefix}


@app.get("/v1/api-keys", dependencies=[Depends(require_auth)])
async def list_api_keys():
    keys = await repo.list_api_keys()
    return [{"id": k.id, "name": k.name, "prefix": k.prefix, "created_at": k.created_at.isoformat()} for k in keys]


@app.delete("/v1/api-keys/{key_id}", dependencies=[Depends(require_auth)])
async def revoke_api_key(key_id: str):
    if not await repo.revoke_api_key(key_id):
        raise HTTPException(404, "API key not found")
    return {"revoked": True}


# ======================================================================
# SKILLS
# ======================================================================

@app.get("/v1/skills", dependencies=[Depends(require_auth)])
async def list_skills():
    return skills_loader.list_skills()


# ======================================================================
# TOOLS
# ======================================================================

@app.get("/v1/tools", dependencies=[Depends(require_auth)])
async def list_tools():
    executor = ToolExecutor()
    return executor.get_builtin_tool_definitions()


# ======================================================================
# HEALTH (public)
# ======================================================================

@app.get("/health")
async def health():
    return {
        "status": "ok",
        "service": "aurion-agent-runtime",
        "version": "1.0.0",
        "auth_enabled": AUTH_ENABLED,
    }
