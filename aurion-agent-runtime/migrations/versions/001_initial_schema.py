"""Initial schema — all tables for Aurion Agent Runtime.

Revision ID: 001
Revises: None
Create Date: 2026-04-20
"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa
from sqlalchemy.dialects.postgresql import JSONB

revision: str = "001"
down_revision: Union[str, None] = None
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    # Agents
    op.create_table(
        "agents",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("version", sa.Integer, nullable=False, server_default="1"),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("model", JSONB, nullable=False, server_default='{"id":"claude-sonnet-4-20250514","speed":"standard"}'),
        sa.Column("system", sa.Text, nullable=True),
        sa.Column("description", sa.Text, nullable=True),
        sa.Column("tools", JSONB, nullable=False, server_default="[]"),
        sa.Column("mcp_servers", JSONB, nullable=False, server_default="[]"),
        sa.Column("skills", JSONB, nullable=False, server_default="[]"),
        sa.Column("callable_agents", JSONB, nullable=False, server_default="[]"),
        sa.Column("metadata", JSONB, nullable=False, server_default="{}"),
        sa.Column("created_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("archived_at", sa.DateTime, nullable=True),
    )

    # Agent versions
    op.create_table(
        "agent_versions",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("agent_id", sa.String(64), nullable=False, index=True),
        sa.Column("version", sa.Integer, nullable=False),
        sa.Column("snapshot", JSONB, nullable=False),
        sa.Column("created_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.UniqueConstraint("agent_id", "version", name="uq_agent_version"),
    )

    # Environments
    op.create_table(
        "environments",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("config", JSONB, nullable=False, server_default='{"type":"cloud","networking":{"type":"unrestricted"}}'),
        sa.Column("sandbox_provider", sa.String(32), nullable=False, server_default="'docker'"),
        sa.Column("packages", JSONB, nullable=False, server_default="[]"),
        sa.Column("metadata", JSONB, nullable=False, server_default="{}"),
        sa.Column("created_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("archived_at", sa.DateTime, nullable=True),
    )

    # Sessions
    op.create_table(
        "sessions",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("agent_id", sa.String(64), nullable=False, index=True),
        sa.Column("agent_snapshot", JSONB, nullable=False),
        sa.Column("environment_id", sa.String(64), nullable=False, index=True),
        sa.Column("title", sa.String(512), nullable=True),
        sa.Column("status", sa.String(32), nullable=False, server_default="'idle'"),
        sa.Column("stop_reason", sa.String(32), nullable=True),
        sa.Column("stats", JSONB, nullable=False, server_default='{"active_seconds":0,"duration_seconds":0}'),
        sa.Column("usage", JSONB, nullable=False, server_default='{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation":{}}'),
        sa.Column("metadata", JSONB, nullable=False, server_default="{}"),
        sa.Column("created_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("archived_at", sa.DateTime, nullable=True),
    )

    # Events (append-only session log)
    op.create_table(
        "events",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("session_id", sa.String(64), nullable=False, index=True),
        sa.Column("thread_id", sa.String(64), nullable=True),
        sa.Column("type", sa.String(64), nullable=False),
        sa.Column("payload", JSONB, nullable=False, server_default="{}"),
        sa.Column("sequence_num", sa.Integer, nullable=False),
        sa.Column("processed_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.UniqueConstraint("session_id", "sequence_num", name="uq_session_seq"),
    )
    op.create_index("ix_events_session_seq", "events", ["session_id", "sequence_num"])

    # Resources (files + GitHub repos mounted into sessions)
    op.create_table(
        "resources",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("session_id", sa.String(64), nullable=False, index=True),
        sa.Column("type", sa.String(32), nullable=False),
        sa.Column("data", JSONB, nullable=False),
        sa.Column("status", sa.String(32), nullable=False, server_default="'mounted'"),
        sa.Column("created_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
    )

    # API Keys
    op.create_table(
        "api_keys",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("key_hash", sa.String(256), nullable=False),
        sa.Column("prefix", sa.String(12), nullable=False),
        sa.Column("created_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("last_used_at", sa.DateTime, nullable=True),
        sa.Column("revoked_at", sa.DateTime, nullable=True),
    )
    op.create_index("ix_api_keys_prefix", "api_keys", ["prefix"])


def downgrade() -> None:
    op.drop_table("api_keys")
    op.drop_table("resources")
    op.drop_table("events")
    op.drop_table("sessions")
    op.drop_table("environments")
    op.drop_table("agent_versions")
    op.drop_table("agents")
