"""End-to-end integration tests for the full agent flow.

Validates the complete pipeline:
  create agent → create session → send message → stream events → tool loop → result

Uses mocked LLM + EventBus to test the full orchestration without external deps.
Auth is disabled for tests.
"""

import asyncio
import os
import uuid
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

os.environ["AUTH_ENABLED"] = "false"

from httpx import ASGITransport, AsyncClient

from packages.core.models import (
    Agent,
    Environment,
    Event,
    EventType,
    LLMContentBlock,
    LLMResponse,
    Session,
    SessionStatus,
    StopReason,
)


# ─── Helpers ──────────────────────────────────────────────────────────────────


def _make_llm_text_response(text: str, model: str = "claude-sonnet-4-20250514") -> LLMResponse:
    """Simulate an LLM response that just returns text (no tool calls)."""
    return LLMResponse(
        content=[LLMContentBlock(type="text", text=text)],
        model=model,
        stop_reason="end_turn",
        usage={"input_tokens": 50, "output_tokens": 20},
    )


def _make_llm_tool_then_text(
    tool_name: str,
    tool_input: dict,
    final_text: str,
) -> list[LLMResponse]:
    """Simulate a two-turn interaction: tool call then text answer."""
    tool_call_id = f"toolu_{uuid.uuid4().hex[:24]}"
    return [
        # Turn 1: LLM wants to call a tool
        LLMResponse(
            content=[
                LLMContentBlock(
                    type="tool_use",
                    id=tool_call_id,
                    name=tool_name,
                    input=tool_input,
                ),
            ],
            model="claude-sonnet-4-20250514",
            stop_reason="tool_use",
            usage={"input_tokens": 60, "output_tokens": 30},
        ),
        # Turn 2: LLM gives the final answer
        LLMResponse(
            content=[LLMContentBlock(type="text", text=final_text)],
            model="claude-sonnet-4-20250514",
            stop_reason="end_turn",
            usage={"input_tokens": 80, "output_tokens": 40},
        ),
    ]


# ─── Fixtures ─────────────────────────────────────────────────────────────────


@pytest.fixture
def mock_repo():
    """In-memory mock repository that stores agents/sessions/events/envs."""
    agents: dict[str, Agent] = {}
    sessions: dict[str, Session] = {}
    events: dict[str, list[Event]] = {}
    environments: dict[str, Environment] = {}
    resources: dict[str, list] = {}
    api_keys: dict = {}
    agent_versions: dict = {}
    max_seqs: dict[str, int] = {}

    repo = AsyncMock()

    # Agents
    async def create_agent(agent):
        agents[agent.id] = agent
        return agent
    repo.create_agent = AsyncMock(side_effect=create_agent)
    repo.get_agent = AsyncMock(side_effect=lambda aid: agents.get(aid))
    repo.list_agents = AsyncMock(side_effect=lambda: list(agents.values()))
    repo.update_agent = AsyncMock(side_effect=lambda a: agents.update({a.id: a}) or a)
    repo.delete_agent = AsyncMock(side_effect=lambda aid: agents.pop(aid, None))
    repo.archive_agent = AsyncMock(side_effect=lambda aid: agents.get(aid))

    # Agent versions
    async def create_agent_version(av):
        agent_versions[(av.agent_id, av.version)] = av
        return av
    repo.create_agent_version = AsyncMock(side_effect=create_agent_version)
    repo.list_agent_versions = AsyncMock(
        side_effect=lambda aid: [v for k, v in agent_versions.items() if k[0] == aid]
    )
    repo.get_agent_version = AsyncMock(
        side_effect=lambda aid, ver: agent_versions.get((aid, ver))
    )

    # Environments
    async def create_environment(env):
        environments[env.id] = env
        return env
    repo.create_environment = AsyncMock(side_effect=create_environment)
    repo.get_environment = AsyncMock(side_effect=lambda eid: environments.get(eid))
    repo.list_environments = AsyncMock(side_effect=lambda: list(environments.values()))
    repo.delete_environment = AsyncMock(side_effect=lambda eid: environments.pop(eid, None) is not None)

    # Sessions
    async def create_session(session):
        sessions[session.id] = session
        events[session.id] = []
        max_seqs[session.id] = 0
        return session
    repo.create_session = AsyncMock(side_effect=create_session)
    repo.get_session = AsyncMock(side_effect=lambda sid: sessions.get(sid))
    repo.list_sessions = AsyncMock(side_effect=lambda agent_id=None: [
        s for s in sessions.values()
        if agent_id is None or s.agent_id == agent_id
    ])
    async def update_session(session):
        sessions[session.id] = session
        return session
    repo.update_session = AsyncMock(side_effect=update_session)
    repo.delete_session = AsyncMock(side_effect=lambda sid: sessions.pop(sid, None) is not None)

    # Events
    async def create_event(event):
        sid = event.session_id
        if sid not in events:
            events[sid] = []
        events[sid].append(event)
        max_seqs[sid] = max(max_seqs.get(sid, 0), event.sequence_num)
        return event
    repo.create_event = AsyncMock(side_effect=create_event)
    repo.list_events = AsyncMock(
        side_effect=lambda sid, limit=100, after_id=None: events.get(sid, [])[:limit]
    )
    repo.get_max_sequence = AsyncMock(side_effect=lambda sid: max_seqs.get(sid, 0))

    # Resources
    async def create_resource(sid, resource):
        if sid not in resources:
            resources[sid] = []
        resources[sid].append(resource)
        return resource
    repo.create_resource = AsyncMock(side_effect=create_resource)
    repo.list_resources = AsyncMock(side_effect=lambda sid: resources.get(sid, []))
    repo.get_resource = AsyncMock(return_value=None)
    repo.delete_resource = AsyncMock(return_value=True)

    # API keys
    repo.create_api_key = AsyncMock()
    repo.list_api_keys = AsyncMock(return_value=[])
    repo.revoke_api_key = AsyncMock(return_value=True)

    # Files
    files: dict[str, tuple] = {}  # {file_id: (FileUpload, storage_path)}

    async def create_file(file_obj, storage_path):
        from packages.core.models import FileUpload
        files[file_obj.id] = (file_obj, storage_path)
        return file_obj
    repo.create_file = AsyncMock(side_effect=create_file)

    async def get_file(file_id):
        return files.get(file_id)
    repo.get_file = AsyncMock(side_effect=get_file)

    async def list_files_fn(purpose=None):
        return [f for f, _ in files.values() if purpose is None or f.purpose == purpose]
    repo.list_files = AsyncMock(side_effect=list_files_fn)

    async def delete_file(file_id):
        entry = files.pop(file_id, None)
        return entry[1] if entry else None
    repo.delete_file = AsyncMock(side_effect=delete_file)

    # Usage / billing
    async def get_session_usage(session_id):
        s = sessions.get(session_id)
        if not s:
            return {}
        model_id = ""
        if s.agent and hasattr(s.agent, "model"):
            model_id = s.agent.model.id if hasattr(s.agent.model, "id") else ""
        usage = s.usage.model_dump() if hasattr(s.usage, "model_dump") else (s.usage or {})
        return {"usage": usage, "model_id": model_id, "status": s.status, "created_at": ""}
    repo.get_session_usage = AsyncMock(side_effect=get_session_usage)

    async def get_all_sessions_usage(agent_id=None):
        result = []
        for s in sessions.values():
            if agent_id and s.agent_id != agent_id:
                continue
            model_id = ""
            if s.agent and hasattr(s.agent, "model"):
                model_id = s.agent.model.id if hasattr(s.agent.model, "id") else ""
            usage = s.usage.model_dump() if hasattr(s.usage, "model_dump") else (s.usage or {})
            result.append({
                "session_id": s.id,
                "agent_id": s.agent_id,
                "model_id": model_id,
                "usage": usage,
                "status": s.status,
                "created_at": "",
            })
        return result
    repo.get_all_sessions_usage = AsyncMock(side_effect=get_all_sessions_usage)

    # DB init
    repo.init_db = AsyncMock()

    # Expose internals for assertions
    repo._agents = agents
    repo._sessions = sessions
    repo._events = events
    repo._environments = environments
    repo._files = files

    return repo


@pytest.fixture
def mock_event_bus():
    """In-memory event bus that records all emitted events."""
    bus = AsyncMock()
    emitted: list[Event] = []

    bus.connect = AsyncMock()
    bus.close = AsyncMock()

    def create_event(session_id, event_type, payload, thread_id=None, sequence_num=0):
        return Event(
            id=f"evt_{uuid.uuid4().hex[:24]}",
            session_id=session_id,
            thread_id=thread_id,
            type=event_type,
            payload=payload,
            sequence_num=sequence_num,
        )
    bus.create_event = MagicMock(side_effect=create_event)

    async def emit(session_id, event):
        emitted.append(event)
    bus.emit = AsyncMock(side_effect=emit)

    async def get_events(session_id):
        return [e for e in emitted if e.session_id == session_id]
    bus.get_events = AsyncMock(side_effect=get_events)

    async def subscribe(session_id):
        for e in emitted:
            if e.session_id == session_id:
                yield e
    bus.subscribe = subscribe

    bus._emitted = emitted
    return bus


@pytest.fixture
async def patched_app(mock_repo, mock_event_bus):
    """Patch the app's repo and event_bus with mocks, return an async client."""
    import apps.api.main as main_module

    original_repo = main_module.repo
    original_bus = main_module.event_bus

    main_module.repo = mock_repo
    main_module.event_bus = mock_event_bus
    main_module.orchestrators.clear()

    # Mock file storage
    mock_file_storage = AsyncMock()
    _stored_files: dict[str, bytes] = {}

    async def mock_save(file_id, data):
        _stored_files[file_id] = data
        return f"/mock/storage/{file_id}"
    mock_file_storage.save = AsyncMock(side_effect=mock_save)

    async def mock_read(storage_path):
        file_id = storage_path.split("/")[-1]
        if file_id in _stored_files:
            return _stored_files[file_id]
        raise FileNotFoundError(f"Not found: {storage_path}")
    mock_file_storage.read = AsyncMock(side_effect=mock_read)

    async def mock_delete(storage_path):
        file_id = storage_path.split("/")[-1]
        return _stored_files.pop(file_id, None) is not None
    mock_file_storage.delete = AsyncMock(side_effect=mock_delete)

    main_module.file_storage = mock_file_storage

    # Mock ancillary services
    main_module.memory_store = AsyncMock()
    main_module.memory_store.connect = AsyncMock()
    main_module.memory_store.close = AsyncMock()
    main_module.observability = AsyncMock()
    main_module.observability.connect = AsyncMock()
    main_module.observability.close = AsyncMock()
    main_module.skills_loader = AsyncMock()
    main_module.skills_loader.load_all = AsyncMock()
    main_module.skills_loader.list_skills = MagicMock(return_value=[])

    transport = ASGITransport(app=main_module.app)
    async with AsyncClient(transport=transport, base_url="http://test") as client:
        yield client, mock_repo, mock_event_bus, main_module

    # Restore
    main_module.repo = original_repo
    main_module.event_bus = original_bus
    main_module.orchestrators.clear()


# ─── E2E Tests ────────────────────────────────────────────────────────────────


class TestE2ESimpleTextFlow:
    """Full flow: create agent → env → session → send message → get text response."""

    @pytest.mark.asyncio
    async def test_create_agent_session_send_message(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        # 1. Create agent
        resp = await client.post("/v1/agents", json={
            "name": "e2e-agent",
            "model": {"id": "claude-sonnet-4-20250514", "speed": "standard"},
            "system": "You are a helpful agent for e2e testing.",
        })
        assert resp.status_code == 200
        agent_data = resp.json()
        agent_id = agent_data["id"]
        assert agent_data["name"] == "e2e-agent"
        assert agent_data["version"] == 1

        # 2. Create environment
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        assert resp.status_code == 200
        env_id = resp.json()["id"]

        # 3. Create session
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
            "title": "E2E Test Session",
        })
        assert resp.status_code == 200
        session_data = resp.json()
        session_id = session_data["id"]
        assert session_data["status"] == "idle"
        assert session_data["title"] == "E2E Test Session"
        assert session_data["agent"]["name"] == "e2e-agent"

        # 4. Send a user.message event (mock the LLM to return text)
        llm_response = _make_llm_text_response("Hello! I'm your test agent.")

        with patch("packages.core.session.SessionOrchestrator.start", new_callable=AsyncMock):
            with patch("packages.core.llm_providers.LLMProvider.chat", new_callable=AsyncMock, return_value=llm_response):
                with patch("packages.core.tool_executor.ToolExecutor.get_builtin_tool_definitions", return_value=[]):
                    resp = await client.post(f"/v1/sessions/{session_id}/events", json={
                        "events": [{"type": "user.message", "content": [{"type": "text", "text": "Hello agent!"}]}]
                    })
                    assert resp.status_code == 200

                    # Give the async task time to complete
                    await asyncio.sleep(0.3)

        # 5. Verify events were emitted
        emitted = mock_event_bus._emitted
        session_events = [e for e in emitted if e.session_id == session_id]
        event_types = [e.type for e in session_events]

        # Should have user.message + agent.message at minimum
        assert EventType.USER_MESSAGE in event_types, f"Missing user.message in {event_types}"
        assert EventType.AGENT_MESSAGE in event_types, f"Missing agent.message in {event_types}"

        # Verify the agent message content
        agent_msgs = [e for e in session_events if e.type == EventType.AGENT_MESSAGE]
        assert len(agent_msgs) >= 1
        assert "Hello! I'm your test agent." in agent_msgs[0].payload.get("content", "")

        # 6. List events via API
        resp = await client.get(f"/v1/sessions/{session_id}/events")
        assert resp.status_code == 200

    @pytest.mark.asyncio
    async def test_session_with_version_pinning(self, patched_app):
        """Creating a session with agent dict {id, version} pins to that version."""
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Create agent
        resp = await client.post("/v1/agents", json={"name": "versioned-agent"})
        assert resp.status_code == 200
        agent_id = resp.json()["id"]

        # Update to v2
        resp = await client.post(f"/v1/agents/{agent_id}", json={
            "version": 1,
            "name": "versioned-agent-v2",
            "system": "I am version 2",
        })
        assert resp.status_code == 200
        assert resp.json()["version"] == 2

        # Create env
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]

        # Create session pinned to version 1
        resp = await client.post("/v1/sessions", json={
            "agent": {"id": agent_id, "type": "agent", "version": 1},
            "environment_id": env_id,
        })
        assert resp.status_code == 200
        session_data = resp.json()
        # The session's agent should be the v1 snapshot (name="versioned-agent")
        assert session_data["agent"]["name"] == "versioned-agent"

    @pytest.mark.asyncio
    async def test_session_with_vault_ids(self, patched_app):
        """Creating a session with vault_ids persists them."""
        client, mock_repo, mock_event_bus, main_module = patched_app

        resp = await client.post("/v1/agents", json={"name": "vault-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]

        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
            "vault_ids": ["vault_abc123", "vault_def456"],
        })
        assert resp.status_code == 200
        assert resp.json()["vault_ids"] == ["vault_abc123", "vault_def456"]


class TestE2EToolLoop:
    """Full flow with tool calls: agent calls a tool, gets result, responds."""

    @pytest.mark.asyncio
    async def test_tool_call_and_result_flow(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Setup
        resp = await client.post("/v1/agents", json={"name": "tool-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
        })
        session_id = resp.json()["id"]

        # Mock LLM: first call returns tool_use, second returns text
        responses = _make_llm_tool_then_text(
            tool_name="bash",
            tool_input={"command": "echo hello"},
            final_text="The command output was: hello",
        )
        call_count = 0

        async def mock_chat(**kwargs):
            nonlocal call_count
            r = responses[min(call_count, len(responses) - 1)]
            call_count += 1
            return r

        from packages.core.models import ToolResult

        with patch("packages.core.session.SessionOrchestrator.start", new_callable=AsyncMock):
            with patch("packages.core.llm_providers.LLMProvider.chat", new_callable=AsyncMock, side_effect=mock_chat):
                with patch("packages.core.tool_executor.ToolExecutor.get_builtin_tool_definitions", return_value=[]):
                    with patch("packages.core.tool_executor.ToolExecutor.execute", new_callable=AsyncMock, return_value=ToolResult(call_id="test", output="hello\n", is_error=False)):
                        resp = await client.post(f"/v1/sessions/{session_id}/events", json={
                            "events": [{"type": "user.message", "content": [{"type": "text", "text": "Run echo hello"}]}]
                        })
                        assert resp.status_code == 200
                        await asyncio.sleep(0.5)

        # Verify the full tool loop events were emitted
        emitted = mock_event_bus._emitted
        session_events = [e for e in emitted if e.session_id == session_id]
        event_types = [e.type for e in session_events]

        assert EventType.USER_MESSAGE in event_types
        assert EventType.AGENT_TOOL_USE in event_types, f"Missing agent.tool_use in {event_types}"
        assert EventType.AGENT_TOOL_RESULT in event_types, f"Missing agent.tool_result in {event_types}"
        assert EventType.AGENT_MESSAGE in event_types


class TestE2EInterrupt:
    """Test interrupt flow: send message, then interrupt."""

    @pytest.mark.asyncio
    async def test_user_interrupt(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        resp = await client.post("/v1/agents", json={"name": "int-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
        })
        session_id = resp.json()["id"]

        # Send interrupt (no running orchestrator, just event recording)
        resp = await client.post(f"/v1/sessions/{session_id}/events", json={
            "events": [{"type": "user.interrupt"}]
        })
        assert resp.status_code == 200

        emitted = mock_event_bus._emitted
        session_events = [e for e in emitted if e.session_id == session_id]
        interrupt_events = [e for e in session_events if e.type == EventType.USER_INTERRUPT]
        assert len(interrupt_events) == 1


class TestE2EDefineOutcome:
    """Test user.define_outcome event."""

    @pytest.mark.asyncio
    async def test_define_outcome(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        resp = await client.post("/v1/agents", json={"name": "outcome-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
        })
        session_id = resp.json()["id"]

        resp = await client.post(f"/v1/sessions/{session_id}/events", json={
            "events": [{"type": "user.define_outcome", "outcome": "success", "metadata": {"score": "95"}}]
        })
        assert resp.status_code == 200

        emitted = mock_event_bus._emitted
        session_events = [e for e in emitted if e.session_id == session_id]
        outcome_events = [e for e in session_events if e.type == EventType.USER_DEFINE_OUTCOME]
        assert len(outcome_events) == 1
        assert outcome_events[0].payload["outcome"] == "success"


class TestE2EBetaHeader:
    """Test the beta header middleware."""

    @pytest.mark.asyncio
    async def test_valid_beta_header(self, patched_app):
        client, *_ = patched_app
        resp = await client.get(
            "/v1/agents",
            headers={"anthropic-beta": "managed-agents-2026-04-01"},
        )
        assert resp.status_code == 200

    @pytest.mark.asyncio
    async def test_no_beta_header(self, patched_app):
        client, *_ = patched_app
        resp = await client.get("/v1/agents")
        assert resp.status_code == 200  # Backwards-compatible

    @pytest.mark.asyncio
    async def test_wrong_beta_header(self, patched_app):
        client, *_ = patched_app
        resp = await client.get(
            "/v1/agents",
            headers={"anthropic-beta": "wrong-version-123"},
        )
        assert resp.status_code == 400
        assert "Unsupported beta version" in resp.json()["error"]


class TestE2EResourceFlow:
    """Test full resource lifecycle: add → list → update → delete."""

    @pytest.mark.asyncio
    async def test_add_file_resource(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        resp = await client.post("/v1/agents", json={"name": "res-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
        })
        session_id = resp.json()["id"]

        # Add file resource
        resp = await client.post(f"/v1/sessions/{session_id}/resources", json={
            "type": "file",
            "file_id": "file_abc123",
            "mount_path": "/workspace/data.csv",
        })
        assert resp.status_code == 200
        data = resp.json()
        assert data["type"] == "file"
        assert data["file_id"] == "file_abc123"

    @pytest.mark.asyncio
    async def test_add_github_resource(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        resp = await client.post("/v1/agents", json={"name": "res-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
        })
        session_id = resp.json()["id"]

        resp = await client.post(f"/v1/sessions/{session_id}/resources", json={
            "type": "github_repository",
            "url": "https://github.com/org/repo",
            "mount_path": "/workspace/repo",
        })
        assert resp.status_code == 200
        data = resp.json()
        assert data["type"] == "github_repository"
        assert data["url"] == "https://github.com/org/repo"


class TestE2ESessionLifecycle:
    """Test session CRUD: create → get → update → archive → delete."""

    @pytest.mark.asyncio
    async def test_full_session_lifecycle(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Setup
        resp = await client.post("/v1/agents", json={"name": "lifecycle-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]

        # Create
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
            "title": "Lifecycle Test",
            "metadata": {"key": "value"},
        })
        assert resp.status_code == 200
        session_id = resp.json()["id"]

        # Get
        resp = await client.get(f"/v1/sessions/{session_id}")
        assert resp.status_code == 200
        assert resp.json()["title"] == "Lifecycle Test"

        # Update
        resp = await client.post(f"/v1/sessions/{session_id}", json={
            "title": "Updated Title",
            "metadata": {"key": "new_value", "extra": "data"},
        })
        assert resp.status_code == 200
        assert resp.json()["title"] == "Updated Title"

        # List
        resp = await client.get("/v1/sessions")
        assert resp.status_code == 200
        assert any(s["id"] == session_id for s in resp.json())

        # Archive
        resp = await client.post(f"/v1/sessions/{session_id}/archive")
        assert resp.status_code == 200
        assert resp.json()["archived_at"] is not None

        # Delete
        resp = await client.delete(f"/v1/sessions/{session_id}")
        assert resp.status_code == 200


class TestE2ERateLimiting:
    """Test API rate limiting middleware."""

    @pytest.mark.asyncio
    async def test_rate_limit_headers_present(self, patched_app):
        """Responses on /v1/ should include X-RateLimit-Limit header."""
        client, *_ = patched_app
        resp = await client.get("/v1/agents")
        assert resp.status_code == 200
        assert "X-RateLimit-Limit" in resp.headers

    @pytest.mark.asyncio
    async def test_rate_limit_not_on_health(self, patched_app):
        """Health endpoint should bypass rate limiting."""
        client, *_ = patched_app
        # Fire many requests to /health — none should be rate limited
        for _ in range(50):
            resp = await client.get("/health")
            assert resp.status_code == 200

    @pytest.mark.asyncio
    async def test_rate_limit_429_response_format(self, patched_app):
        """When rate limited, 429 response has correct structure."""
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Temporarily set very aggressive rate limit
        import apps.api.main as m
        original_enabled = m.RATE_LIMIT_ENABLED
        original_burst = m._rate_limiter._burst
        original_rate = m._rate_limiter._rate

        m.RATE_LIMIT_ENABLED = True
        m._rate_limiter._burst = 2
        m._rate_limiter._rate = 0.001  # very slow refill
        # Clear existing tokens
        m._rate_limiter._buckets.clear()

        try:
            # Exhaust the bucket
            for _ in range(3):
                await client.get("/v1/agents")

            # This should be rate limited
            resp = await client.get("/v1/agents")
            assert resp.status_code == 429
            data = resp.json()
            assert data["error"] == "rate_limit_exceeded"
            assert "retry_after" in data
            assert "Retry-After" in resp.headers
        finally:
            m.RATE_LIMIT_ENABLED = original_enabled
            m._rate_limiter._burst = original_burst
            m._rate_limiter._rate = original_rate
            m._rate_limiter._buckets.clear()


class TestE2EFilesAPI:
    """Full /v1/files lifecycle: upload → list → get → download → delete."""

    @pytest.mark.asyncio
    async def test_upload_and_get_file(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Upload a file
        file_content = b"Hello, this is test file content!"
        resp = await client.post(
            "/v1/files",
            files={"file": ("test.txt", file_content, "text/plain")},
            params={"purpose": "session_resource"},
        )
        assert resp.status_code == 200
        data = resp.json()
        file_id = data["id"]
        assert data["filename"] == "test.txt"
        assert data["content_type"] == "text/plain"
        assert data["size_bytes"] == len(file_content)
        assert data["purpose"] == "session_resource"
        assert data["status"] == "uploaded"
        assert file_id.startswith("file_")

        # Get file metadata
        resp = await client.get(f"/v1/files/{file_id}")
        assert resp.status_code == 200
        assert resp.json()["filename"] == "test.txt"

    @pytest.mark.asyncio
    async def test_list_files(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Upload two files
        resp = await client.post(
            "/v1/files",
            files={"file": ("a.txt", b"aaa", "text/plain")},
        )
        assert resp.status_code == 200

        resp = await client.post(
            "/v1/files",
            files={"file": ("b.csv", b"col1,col2", "text/csv")},
        )
        assert resp.status_code == 200

        # List all
        resp = await client.get("/v1/files")
        assert resp.status_code == 200
        files_list = resp.json()
        assert len(files_list) >= 2

    @pytest.mark.asyncio
    async def test_download_file_content(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        content = b"Binary content \x00\x01\x02"
        resp = await client.post(
            "/v1/files",
            files={"file": ("data.bin", content, "application/octet-stream")},
        )
        file_id = resp.json()["id"]

        # Download content
        resp = await client.get(f"/v1/files/{file_id}/content")
        assert resp.status_code == 200
        assert resp.content == content
        assert "content-disposition" in resp.headers
        assert "data.bin" in resp.headers["content-disposition"]

    @pytest.mark.asyncio
    async def test_delete_file(self, patched_app):
        client, mock_repo, mock_event_bus, main_module = patched_app

        resp = await client.post(
            "/v1/files",
            files={"file": ("delete_me.txt", b"bye", "text/plain")},
        )
        file_id = resp.json()["id"]

        # Delete
        resp = await client.delete(f"/v1/files/{file_id}")
        assert resp.status_code == 200
        assert resp.json()["id"] == file_id
        assert resp.json()["type"] == "file.deleted"

        # Verify it's gone
        resp = await client.get(f"/v1/files/{file_id}")
        assert resp.status_code == 404

    @pytest.mark.asyncio
    async def test_get_nonexistent_file(self, patched_app):
        client, *_ = patched_app
        resp = await client.get("/v1/files/file_nonexistent999")
        assert resp.status_code == 404

    @pytest.mark.asyncio
    async def test_upload_file_then_mount_as_resource(self, patched_app):
        """Upload a file, then mount it as a session resource."""
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Upload
        resp = await client.post(
            "/v1/files",
            files={"file": ("config.json", b'{"key": "value"}', "application/json")},
        )
        file_id = resp.json()["id"]

        # Create agent + env + session
        resp = await client.post("/v1/agents", json={"name": "file-agent"})
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
        })
        session_id = resp.json()["id"]

        # Mount file as resource
        resp = await client.post(f"/v1/sessions/{session_id}/resources", json={
            "type": "file",
            "file_id": file_id,
            "mount_path": "/workspace/config.json",
        })
        assert resp.status_code == 200
        assert resp.json()["file_id"] == file_id
        assert resp.json()["mount_path"] == "/workspace/config.json"


class TestE2EBillingUsage:
    """Test billing/usage endpoints."""

    @pytest.mark.asyncio
    async def test_session_usage_endpoint(self, patched_app):
        """GET /v1/sessions/{id}/usage returns cost breakdown."""
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Create agent + env + session
        resp = await client.post("/v1/agents", json={
            "name": "billing-agent",
            "model": {"id": "claude-sonnet-4-20250514", "speed": "standard"},
        })
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]
        resp = await client.post("/v1/sessions", json={
            "agent": agent_id,
            "environment_id": env_id,
        })
        session_id = resp.json()["id"]

        # Manually set usage on the session (simulating post-LLM-call state)
        from packages.core.models import SessionUsage
        session = mock_repo._sessions[session_id]
        session.usage = SessionUsage(
            input_tokens=10000,
            output_tokens=5000,
            cache_read_input_tokens=2000,
        )

        # Get usage
        resp = await client.get(f"/v1/sessions/{session_id}/usage")
        assert resp.status_code == 200
        data = resp.json()
        assert data["session_id"] == session_id
        assert data["model_id"] == "claude-sonnet-4-20250514"
        assert data["input_tokens"] == 10000
        assert data["output_tokens"] == 5000
        assert data["cache_read_input_tokens"] == 2000
        assert data["total_cost_usd"] > 0
        assert data["currency"] == "USD"

        # Verify cost calculation: 10000/1M * $3.00 + 5000/1M * $15.00 + 2000/1M * $0.30
        expected_input = 10000 / 1_000_000 * 3.00
        expected_output = 5000 / 1_000_000 * 15.00
        expected_cache = 2000 / 1_000_000 * 0.30
        expected_total = round(expected_input + expected_output + expected_cache, 6)
        assert abs(data["total_cost_usd"] - expected_total) < 0.000001

    @pytest.mark.asyncio
    async def test_usage_not_found(self, patched_app):
        client, *_ = patched_app
        resp = await client.get("/v1/sessions/nonexistent_session/usage")
        assert resp.status_code == 404

    @pytest.mark.asyncio
    async def test_aggregate_usage_summary(self, patched_app):
        """GET /v1/usage returns aggregate across sessions."""
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Create two sessions with different usage
        resp = await client.post("/v1/agents", json={
            "name": "agg-agent",
            "model": {"id": "claude-sonnet-4-20250514", "speed": "standard"},
        })
        agent_id = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]

        from packages.core.models import SessionUsage

        for i, (inp, out) in enumerate([(5000, 2000), (8000, 3000)]):
            resp = await client.post("/v1/sessions", json={
                "agent": agent_id,
                "environment_id": env_id,
                "title": f"Session {i}",
            })
            sid = resp.json()["id"]
            mock_repo._sessions[sid].usage = SessionUsage(
                input_tokens=inp, output_tokens=out,
            )

        # Get aggregate
        resp = await client.get("/v1/usage")
        assert resp.status_code == 200
        data = resp.json()
        assert data["total_sessions"] >= 2
        assert data["total_input_tokens"] >= 13000
        assert data["total_output_tokens"] >= 5000
        assert data["total_cost_usd"] > 0
        assert "claude-sonnet-4-20250514" in data["by_model"]

    @pytest.mark.asyncio
    async def test_aggregate_usage_filter_by_agent(self, patched_app):
        """GET /v1/usage?agent_id=X filters by agent."""
        client, mock_repo, mock_event_bus, main_module = patched_app

        # Create two agents with sessions
        resp = await client.post("/v1/agents", json={"name": "agent-a"})
        agent_a = resp.json()["id"]
        resp = await client.post("/v1/agents", json={"name": "agent-b"})
        agent_b = resp.json()["id"]
        resp = await client.post("/v1/environments", json={"name": "test-env"})
        env_id = resp.json()["id"]

        from packages.core.models import SessionUsage

        # Session for agent A
        resp = await client.post("/v1/sessions", json={
            "agent": agent_a, "environment_id": env_id,
        })
        sid_a = resp.json()["id"]
        mock_repo._sessions[sid_a].usage = SessionUsage(input_tokens=1000, output_tokens=500)

        # Session for agent B
        resp = await client.post("/v1/sessions", json={
            "agent": agent_b, "environment_id": env_id,
        })
        sid_b = resp.json()["id"]
        mock_repo._sessions[sid_b].usage = SessionUsage(input_tokens=9000, output_tokens=4000)

        # Filter by agent A
        resp = await client.get(f"/v1/usage?agent_id={agent_a}")
        assert resp.status_code == 200
        data = resp.json()
        assert data["total_sessions"] == 1
        assert data["total_input_tokens"] == 1000
