"""Add files table for /v1/files upload API.

Revision ID: 002
Revises: 001
Create Date: 2026-04-21
"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa

revision: str = "002"
down_revision: Union[str, None] = "001"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_table(
        "files",
        sa.Column("id", sa.String(64), primary_key=True),
        sa.Column("filename", sa.String(512), nullable=False),
        sa.Column("content_type", sa.String(128), nullable=False, server_default="application/octet-stream"),
        sa.Column("size_bytes", sa.Integer, nullable=False, server_default="0"),
        sa.Column("purpose", sa.String(64), nullable=False, server_default="session_resource"),
        sa.Column("status", sa.String(32), nullable=False, server_default="uploaded"),
        sa.Column("storage_path", sa.String(1024), nullable=False),
        sa.Column("created_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime, nullable=False, server_default=sa.func.now()),
    )
    op.create_index("ix_files_purpose", "files", ["purpose"])


def downgrade() -> None:
    op.drop_table("files")
