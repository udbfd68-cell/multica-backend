"""PostgreSQL persistence layer using SQLAlchemy async + asyncpg.

This replaces ALL in-memory dict stores with real database tables.
Tables: agents, agent_versions, environments, sessions, events, resources, api_keys.
"""

from __future__ import annotations

import json
import os
from datetime import datetime
from typing import Any

import structlog
from sqlalchemy import (
    Boolean,
    Column,
    DateTime,
    Float,
    Integer,
    String,
    Text,
    UniqueConstraint,
    func,
    text,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column

logger = structlog.get_logger()


# ══════════════════════════════════════════════════════════════════════════════
# Engine + Session factory
# ══════════════════════════════════════════════════════════════════════════════

DATABASE_URL = os.environ.get(
    "DATABASE_URL",
    "postgresql+asyncpg://aurion:aurion@localhost:5432/aurion",
)

engine = create_async_engine(
    DATABASE_URL,
    pool_size=20,
    max_overflow=10,
    pool_pre_ping=True,
    echo=os.environ.get("SQL_ECHO", "").lower() == "true",
)

async_session_factory = async_sessionmaker(engine, expire_on_commit=False)


class Base(DeclarativeBase):
    pass


# ══════════════════════════════════════════════════════════════════════════════
# ORM Models — one table per domain object
# ══════════════════════════════════════════════════════════════════════════════


class AgentRow(Base):
    __tablename__ = "agents"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    version: Mapped[int] = mapped_column(Integer, default=1)
    name: Mapped[str] = mapped_column(String(255), nullable=False)
    model: Mapped[dict] = mapped_column(JSONB, default=lambda: {"id": "claude-sonnet-4-20250514", "speed": "standard"})
    system: Mapped[str | None] = mapped_column(Text, nullable=True)
    description: Mapped[str | None] = mapped_column(Text, nullable=True)
    tools: Mapped[list] = mapped_column(JSONB, default=list)
    mcp_servers: Mapped[list] = mapped_column(JSONB, default=list)
    skills: Mapped[list] = mapped_column(JSONB, default=list)
    callable_agents: Mapped[list] = mapped_column(JSONB, default=list)
    metadata_: Mapped[dict] = mapped_column("metadata", JSONB, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())
    updated_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), onupdate=func.now())
    archived_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)


class AgentVersionRow(Base):
    __tablename__ = "agent_versions"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    agent_id: Mapped[str] = mapped_column(String(64), index=True, nullable=False)
    version: Mapped[int] = mapped_column(Integer, nullable=False)
    snapshot: Mapped[dict] = mapped_column(JSONB, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())

    __table_args__ = (
        UniqueConstraint("agent_id", "version", name="uq_agent_version"),
    )


class EnvironmentRow(Base):
    __tablename__ = "environments"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    name: Mapped[str] = mapped_column(String(255), nullable=False)
    config: Mapped[dict] = mapped_column(JSONB, default=lambda: {"type": "cloud", "networking": {"type": "unrestricted"}})
    sandbox_provider: Mapped[str] = mapped_column(String(32), default="docker")
    packages: Mapped[list] = mapped_column(JSONB, default=list)
    metadata_: Mapped[dict] = mapped_column("metadata", JSONB, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())
    updated_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), onupdate=func.now())
    archived_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)


class SessionRow(Base):
    __tablename__ = "sessions"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    agent_id: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    agent_snapshot: Mapped[dict] = mapped_column(JSONB, nullable=False)
    environment_id: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    title: Mapped[str | None] = mapped_column(String(512), nullable=True)
    status: Mapped[str] = mapped_column(String(32), default="idle")
    stop_reason: Mapped[str | None] = mapped_column(String(32), nullable=True)
    stats: Mapped[dict] = mapped_column(JSONB, default=lambda: {"active_seconds": 0.0, "duration_seconds": 0.0})
    usage: Mapped[dict] = mapped_column(JSONB, default=lambda: {"input_tokens": 0, "output_tokens": 0, "cache_read_input_tokens": 0, "cache_creation": {}})
    metadata_: Mapped[dict] = mapped_column("metadata", JSONB, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())
    updated_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), onupdate=func.now())
    archived_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)


class EventRow(Base):
    __tablename__ = "events"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    session_id: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    thread_id: Mapped[str | None] = mapped_column(String(64), nullable=True)
    type: Mapped[str] = mapped_column(String(64), nullable=False)
    payload: Mapped[dict] = mapped_column(JSONB, default=dict)
    sequence_num: Mapped[int] = mapped_column(Integer, nullable=False)
    processed_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())

    __table_args__ = (
        UniqueConstraint("session_id", "sequence_num", name="uq_session_seq"),
    )


class ResourceRow(Base):
    __tablename__ = "resources"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    session_id: Mapped[str] = mapped_column(String(64), nullable=False, index=True)
    type: Mapped[str] = mapped_column(String(32), nullable=False)
    data: Mapped[dict] = mapped_column(JSONB, nullable=False)
    status: Mapped[str] = mapped_column(String(32), default="mounted")
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())
    updated_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), onupdate=func.now())


class APIKeyRow(Base):
    __tablename__ = "api_keys"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    name: Mapped[str] = mapped_column(String(255), nullable=False)
    key_hash: Mapped[str] = mapped_column(String(256), nullable=False)
    prefix: Mapped[str] = mapped_column(String(12), nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())
    last_used_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    revoked_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)


class FileRow(Base):
    __tablename__ = "files"

    id: Mapped[str] = mapped_column(String(64), primary_key=True)
    filename: Mapped[str] = mapped_column(String(512), nullable=False)
    content_type: Mapped[str] = mapped_column(String(128), default="application/octet-stream")
    size_bytes: Mapped[int] = mapped_column(Integer, default=0)
    purpose: Mapped[str] = mapped_column(String(64), default="session_resource")
    status: Mapped[str] = mapped_column(String(32), default="uploaded")
    storage_path: Mapped[str] = mapped_column(String(1024), nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now())
    updated_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), onupdate=func.now())


# ══════════════════════════════════════════════════════════════════════════════
# Repository layer — typed async CRUD operations
# ══════════════════════════════════════════════════════════════════════════════

from packages.core.models import (
    Agent,
    AgentVersion,
    APIKey,
    DeletedResource,
    DeletedSession,
    Environment,
    Event,
    EventType,
    FileUpload,
    Session,
    SessionResource,
    SessionStats,
    SessionUsage,
    FileResource,
    GitHubRepositoryResource,
)
from sqlalchemy import select, update, delete


class Repository:
    """Async repository for all domain objects — thin layer over SQLAlchemy."""

    def __init__(self, session_factory: async_sessionmaker = async_session_factory):
        self._sf = session_factory

    # ── Agents ────────────────────────────────────────────────────────────

    async def create_agent(self, agent: Agent) -> Agent:
        async with self._sf() as db:
            row = AgentRow(
                id=agent.id,
                version=agent.version,
                name=agent.name,
                model=agent.model.model_dump() if hasattr(agent.model, "model_dump") else agent.model,
                system=agent.system,
                description=agent.description,
                tools=[t.model_dump() if hasattr(t, "model_dump") else t for t in agent.tools],
                mcp_servers=[s.model_dump() for s in agent.mcp_servers],
                skills=[s.model_dump() for s in agent.skills],
                callable_agents=[c.model_dump() for c in agent.callable_agents],
                metadata_=agent.metadata,
                created_at=agent.created_at,
                updated_at=agent.updated_at,
            )
            db.add(row)
            await db.commit()
        return agent

    async def get_agent(self, agent_id: str) -> Agent | None:
        async with self._sf() as db:
            row = await db.get(AgentRow, agent_id)
            if not row or row.archived_at is not None:
                return None
            return self._agent_from_row(row)

    async def list_agents(self, include_archived: bool = False) -> list[Agent]:
        async with self._sf() as db:
            q = select(AgentRow)
            if not include_archived:
                q = q.where(AgentRow.archived_at.is_(None))
            q = q.order_by(AgentRow.created_at.desc())
            result = await db.execute(q)
            return [self._agent_from_row(r) for r in result.scalars().all()]

    async def update_agent(self, agent: Agent) -> Agent:
        async with self._sf() as db:
            row = await db.get(AgentRow, agent.id)
            if not row:
                raise ValueError(f"Agent {agent.id} not found")
            row.version = agent.version
            row.name = agent.name
            row.model = agent.model.model_dump() if hasattr(agent.model, "model_dump") else agent.model
            row.system = agent.system
            row.description = agent.description
            row.tools = [t.model_dump() if hasattr(t, "model_dump") else t for t in agent.tools]
            row.mcp_servers = [s.model_dump() for s in agent.mcp_servers]
            row.skills = [s.model_dump() for s in agent.skills]
            row.callable_agents = [c.model_dump() for c in agent.callable_agents]
            row.metadata_ = agent.metadata
            row.updated_at = agent.updated_at
            await db.commit()
        return agent

    async def archive_agent(self, agent_id: str) -> Agent | None:
        async with self._sf() as db:
            row = await db.get(AgentRow, agent_id)
            if not row:
                return None
            row.archived_at = datetime.utcnow()
            await db.commit()
            return self._agent_from_row(row)

    async def delete_agent(self, agent_id: str) -> bool:
        async with self._sf() as db:
            result = await db.execute(delete(AgentRow).where(AgentRow.id == agent_id))
            await db.commit()
            return result.rowcount > 0

    # ── Agent Versions ────────────────────────────────────────────────────

    async def create_agent_version(self, av: AgentVersion) -> AgentVersion:
        async with self._sf() as db:
            row = AgentVersionRow(
                id=av.id,
                agent_id=av.agent_id,
                version=av.version,
                snapshot=av.snapshot,
                created_at=av.created_at,
            )
            db.add(row)
            await db.commit()
        return av

    async def list_agent_versions(self, agent_id: str) -> list[AgentVersion]:
        async with self._sf() as db:
            q = (
                select(AgentVersionRow)
                .where(AgentVersionRow.agent_id == agent_id)
                .order_by(AgentVersionRow.version.desc())
            )
            result = await db.execute(q)
            return [
                AgentVersion(
                    id=r.id,
                    agent_id=r.agent_id,
                    version=r.version,
                    snapshot=r.snapshot,
                    created_at=r.created_at,
                )
                for r in result.scalars().all()
            ]

    async def get_agent_version(self, agent_id: str, version: int) -> AgentVersion | None:
        async with self._sf() as db:
            q = (
                select(AgentVersionRow)
                .where(AgentVersionRow.agent_id == agent_id, AgentVersionRow.version == version)
            )
            result = await db.execute(q)
            row = result.scalar_one_or_none()
            if not row:
                return None
            return AgentVersion(
                id=row.id, agent_id=row.agent_id, version=row.version,
                snapshot=row.snapshot, created_at=row.created_at,
            )

    # ── Environments ──────────────────────────────────────────────────────

    async def create_environment(self, env: Environment) -> Environment:
        async with self._sf() as db:
            row = EnvironmentRow(
                id=env.id,
                name=env.name,
                config=env.config.model_dump(),
                sandbox_provider=env.sandbox_provider,
                packages=env.packages,
                metadata_=env.metadata,
                created_at=env.created_at,
                updated_at=env.updated_at,
            )
            db.add(row)
            await db.commit()
        return env

    async def get_environment(self, env_id: str) -> Environment | None:
        async with self._sf() as db:
            row = await db.get(EnvironmentRow, env_id)
            if not row:
                return None
            return self._environment_from_row(row)

    async def list_environments(self) -> list[Environment]:
        async with self._sf() as db:
            q = select(EnvironmentRow).order_by(EnvironmentRow.created_at.desc())
            result = await db.execute(q)
            return [self._environment_from_row(r) for r in result.scalars().all()]

    async def delete_environment(self, env_id: str) -> bool:
        async with self._sf() as db:
            result = await db.execute(delete(EnvironmentRow).where(EnvironmentRow.id == env_id))
            await db.commit()
            return result.rowcount > 0

    # ── Sessions ──────────────────────────────────────────────────────────

    async def create_session(self, session: Session) -> Session:
        async with self._sf() as db:
            row = SessionRow(
                id=session.id,
                agent_id=session.agent_id,
                agent_snapshot=session.agent.model_dump() if session.agent else {},
                environment_id=session.environment_id,
                title=session.title,
                status=session.status,
                stop_reason=session.stop_reason,
                stats=session.stats.model_dump(),
                usage=session.usage.model_dump(),
                metadata_=session.metadata,
                created_at=session.created_at,
                updated_at=session.updated_at,
            )
            db.add(row)
            await db.commit()
        return session

    async def get_session(self, session_id: str) -> Session | None:
        async with self._sf() as db:
            row = await db.get(SessionRow, session_id)
            if not row:
                return None
            return self._session_from_row(row)

    async def list_sessions(self, agent_id: str | None = None) -> list[Session]:
        async with self._sf() as db:
            q = select(SessionRow)
            if agent_id:
                q = q.where(SessionRow.agent_id == agent_id)
            q = q.order_by(SessionRow.created_at.desc())
            result = await db.execute(q)
            return [self._session_from_row(r) for r in result.scalars().all()]

    async def update_session(self, session: Session) -> Session:
        async with self._sf() as db:
            row = await db.get(SessionRow, session.id)
            if not row:
                raise ValueError(f"Session {session.id} not found")
            row.status = session.status
            row.stop_reason = session.stop_reason
            row.title = session.title
            row.stats = session.stats.model_dump()
            row.usage = session.usage.model_dump()
            row.metadata_ = session.metadata
            row.updated_at = datetime.utcnow()
            row.archived_at = session.archived_at
            await db.commit()
        return session

    async def delete_session(self, session_id: str) -> bool:
        async with self._sf() as db:
            result = await db.execute(delete(SessionRow).where(SessionRow.id == session_id))
            await db.commit()
            return result.rowcount > 0

    # ── Events ────────────────────────────────────────────────────────────

    async def create_event(self, event: Event) -> Event:
        async with self._sf() as db:
            row = EventRow(
                id=event.id,
                session_id=event.session_id,
                thread_id=event.thread_id,
                type=event.type,
                payload=event.payload,
                sequence_num=event.sequence_num,
                processed_at=event.processed_at,
            )
            db.add(row)
            await db.commit()
        return event

    async def list_events(
        self, session_id: str, limit: int = 100, after_id: str | None = None
    ) -> list[Event]:
        async with self._sf() as db:
            q = (
                select(EventRow)
                .where(EventRow.session_id == session_id)
                .order_by(EventRow.sequence_num.asc())
                .limit(limit)
            )
            if after_id:
                subq = select(EventRow.sequence_num).where(EventRow.id == after_id).scalar_subquery()
                q = q.where(EventRow.sequence_num > subq)
            result = await db.execute(q)
            return [
                Event(
                    id=r.id,
                    session_id=r.session_id,
                    thread_id=r.thread_id,
                    type=r.type,
                    payload=r.payload,
                    sequence_num=r.sequence_num,
                    processed_at=r.processed_at,
                )
                for r in result.scalars().all()
            ]

    async def get_max_sequence(self, session_id: str) -> int:
        async with self._sf() as db:
            q = select(func.max(EventRow.sequence_num)).where(EventRow.session_id == session_id)
            result = await db.execute(q)
            val = result.scalar()
            return val or 0

    # ── Resources ─────────────────────────────────────────────────────────

    async def create_resource(self, session_id: str, resource: SessionResource) -> SessionResource:
        async with self._sf() as db:
            row = ResourceRow(
                id=resource.id,
                session_id=session_id,
                type=resource.type,
                data=resource.model_dump(),
                status=resource.status,
                created_at=resource.created_at,
                updated_at=resource.updated_at,
            )
            db.add(row)
            await db.commit()
        return resource

    async def get_resource(self, resource_id: str) -> SessionResource | None:
        async with self._sf() as db:
            row = await db.get(ResourceRow, resource_id)
            if not row:
                return None
            return self._resource_from_row(row)

    async def list_resources(self, session_id: str) -> list[SessionResource]:
        async with self._sf() as db:
            q = select(ResourceRow).where(ResourceRow.session_id == session_id)
            result = await db.execute(q)
            return [self._resource_from_row(r) for r in result.scalars().all()]

    async def delete_resource(self, resource_id: str) -> bool:
        async with self._sf() as db:
            result = await db.execute(delete(ResourceRow).where(ResourceRow.id == resource_id))
            await db.commit()
            return result.rowcount > 0

    async def update_resource(self, resource: SessionResource) -> SessionResource:
        async with self._sf() as db:
            row = await db.get(ResourceRow, resource.id)
            if row:
                row.data = resource.model_dump(mode="json")
                row.updated_at = resource.updated_at
                await db.commit()
        return resource

    # ── API Keys ──────────────────────────────────────────────────────────

    async def create_api_key(self, key: APIKey) -> APIKey:
        async with self._sf() as db:
            row = APIKeyRow(
                id=key.id,
                name=key.name,
                key_hash=key.key_hash,
                prefix=key.prefix,
                created_at=key.created_at,
            )
            db.add(row)
            await db.commit()
        return key

    async def get_api_key_by_prefix(self, prefix: str) -> APIKey | None:
        async with self._sf() as db:
            q = select(APIKeyRow).where(
                APIKeyRow.prefix == prefix,
                APIKeyRow.revoked_at.is_(None),
            )
            result = await db.execute(q)
            row = result.scalar_one_or_none()
            if not row:
                return None
            return APIKey(
                id=row.id, name=row.name, key_hash=row.key_hash,
                prefix=row.prefix, created_at=row.created_at,
                last_used_at=row.last_used_at, revoked_at=row.revoked_at,
            )

    async def touch_api_key(self, key_id: str) -> None:
        async with self._sf() as db:
            await db.execute(
                update(APIKeyRow)
                .where(APIKeyRow.id == key_id)
                .values(last_used_at=datetime.utcnow())
            )
            await db.commit()

    async def revoke_api_key(self, key_id: str) -> bool:
        async with self._sf() as db:
            result = await db.execute(
                update(APIKeyRow)
                .where(APIKeyRow.id == key_id, APIKeyRow.revoked_at.is_(None))
                .values(revoked_at=datetime.utcnow())
            )
            await db.commit()
            return result.rowcount > 0

    async def list_api_keys(self) -> list[APIKey]:
        async with self._sf() as db:
            q = select(APIKeyRow).where(APIKeyRow.revoked_at.is_(None)).order_by(APIKeyRow.created_at.desc())
            result = await db.execute(q)
            return [
                APIKey(
                    id=r.id, name=r.name, key_hash=r.key_hash,
                    prefix=r.prefix, created_at=r.created_at,
                    last_used_at=r.last_used_at, revoked_at=r.revoked_at,
                )
                for r in result.scalars().all()
            ]

    # ── Files ─────────────────────────────────────────────────────────────

    async def create_file(self, file_obj: FileUpload, storage_path: str) -> FileUpload:
        async with self._sf() as db:
            row = FileRow(
                id=file_obj.id,
                filename=file_obj.filename,
                content_type=file_obj.content_type,
                size_bytes=file_obj.size_bytes,
                purpose=file_obj.purpose,
                status=file_obj.status,
                storage_path=storage_path,
                created_at=file_obj.created_at,
                updated_at=file_obj.updated_at,
            )
            db.add(row)
            await db.commit()
        return file_obj

    async def get_file(self, file_id: str) -> tuple[FileUpload, str] | None:
        """Returns (FileUpload, storage_path) or None."""
        async with self._sf() as db:
            row = await db.get(FileRow, file_id)
            if not row:
                return None
            return self._file_from_row(row), row.storage_path

    async def list_files(self, purpose: str | None = None) -> list[FileUpload]:
        async with self._sf() as db:
            q = select(FileRow).order_by(FileRow.created_at.desc())
            if purpose:
                q = q.where(FileRow.purpose == purpose)
            result = await db.execute(q)
            return [self._file_from_row(r) for r in result.scalars().all()]

    async def delete_file(self, file_id: str) -> str | None:
        """Delete file row and return storage_path for cleanup, or None if not found."""
        async with self._sf() as db:
            row = await db.get(FileRow, file_id)
            if not row:
                return None
            storage_path = row.storage_path
            await db.execute(delete(FileRow).where(FileRow.id == file_id))
            await db.commit()
            return storage_path

    # ── Usage aggregation (billing) ───────────────────────────────────────

    async def get_session_usage(self, session_id: str) -> dict[str, Any]:
        """Get raw usage + model info for a single session."""
        async with self._sf() as db:
            row = await db.get(SessionRow, session_id)
            if not row:
                return {}
            model_id = ""
            if row.agent_snapshot and isinstance(row.agent_snapshot, dict):
                model_data = row.agent_snapshot.get("model", {})
                if isinstance(model_data, dict):
                    model_id = model_data.get("id", "")
            return {
                "usage": row.usage or {},
                "model_id": model_id,
                "status": row.status,
                "created_at": row.created_at.isoformat() if row.created_at else "",
            }

    async def get_all_sessions_usage(
        self, agent_id: str | None = None
    ) -> list[dict[str, Any]]:
        """Get usage data for all sessions, optionally filtered by agent."""
        async with self._sf() as db:
            q = select(
                SessionRow.id,
                SessionRow.agent_id,
                SessionRow.agent_snapshot,
                SessionRow.usage,
                SessionRow.status,
                SessionRow.created_at,
            )
            if agent_id:
                q = q.where(SessionRow.agent_id == agent_id)
            result = await db.execute(q)
            rows = result.all()
            out = []
            for r in rows:
                model_id = ""
                snapshot = r.agent_snapshot or {}
                if isinstance(snapshot, dict):
                    model_data = snapshot.get("model", {})
                    if isinstance(model_data, dict):
                        model_id = model_data.get("id", "")
                out.append({
                    "session_id": r.id,
                    "agent_id": r.agent_id,
                    "model_id": model_id,
                    "usage": r.usage or {},
                    "status": r.status,
                    "created_at": r.created_at.isoformat() if r.created_at else "",
                })
            return out

    # ── Init (create tables) ─────────────────────────────────────────────

    async def init_db(self) -> None:
        """Create all tables. In production, use Alembic migrations instead."""
        async with engine.begin() as conn:
            await conn.run_sync(Base.metadata.create_all)
        logger.info("database_initialized")

    # ── Private helpers ───────────────────────────────────────────────────

    @staticmethod
    def _agent_from_row(row: AgentRow) -> Agent:
        from packages.core.models import ModelConfig
        return Agent(
            id=row.id,
            version=row.version,
            name=row.name,
            model=ModelConfig(**row.model) if isinstance(row.model, dict) else row.model,
            system=row.system,
            description=row.description,
            tools=row.tools or [],
            mcp_servers=row.mcp_servers or [],
            skills=row.skills or [],
            callable_agents=row.callable_agents or [],
            metadata=row.metadata_ or {},
            created_at=row.created_at,
            updated_at=row.updated_at,
            archived_at=row.archived_at,
        )

    @staticmethod
    def _environment_from_row(row: EnvironmentRow) -> Environment:
        from packages.core.models import EnvironmentConfig
        return Environment(
            id=row.id,
            name=row.name,
            config=EnvironmentConfig(**row.config) if isinstance(row.config, dict) else row.config,
            sandbox_provider=row.sandbox_provider,
            packages=row.packages or [],
            metadata=row.metadata_ or {},
            created_at=row.created_at,
            updated_at=row.updated_at,
            archived_at=row.archived_at,
        )

    @staticmethod
    def _session_from_row(row: SessionRow) -> Session:
        from packages.core.models import Agent as AgentModel
        agent = AgentModel(**row.agent_snapshot) if row.agent_snapshot else None
        return Session(
            id=row.id,
            agent=agent,
            agent_id=row.agent_id,
            environment_id=row.environment_id,
            title=row.title,
            status=row.status,
            stop_reason=row.stop_reason,
            stats=row.stats or {},
            usage=row.usage or {},
            metadata=row.metadata_ or {},
            created_at=row.created_at,
            updated_at=row.updated_at,
            archived_at=row.archived_at,
        )

    @staticmethod
    def _resource_from_row(row: ResourceRow) -> SessionResource:
        data = row.data or {}
        if row.type == "github_repository":
            return GitHubRepositoryResource(**data)
        return FileResource(**data)

    @staticmethod
    def _file_from_row(row: FileRow) -> FileUpload:
        return FileUpload(
            id=row.id,
            filename=row.filename,
            content_type=row.content_type,
            size_bytes=row.size_bytes,
            purpose=row.purpose,
            status=row.status,
            created_at=row.created_at,
            updated_at=row.updated_at,
        )
