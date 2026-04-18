"""API Key authentication middleware.

Validates `x-api-key` or `Authorization: Bearer` header against the database.
Supports a bootstrap key via AURION_BOOTSTRAP_KEY env var for initial setup.
"""

from __future__ import annotations

import hashlib
import hmac
import os
import secrets
from datetime import datetime

import structlog
from fastapi import Depends, HTTPException, Request, Security
from fastapi.security import APIKeyHeader

from packages.core.database import Repository
from packages.core.models import APIKey

logger = structlog.get_logger()

# Allow disabling auth for local dev
AUTH_ENABLED = os.environ.get("AUTH_ENABLED", "true").lower() != "false"
BOOTSTRAP_KEY = os.environ.get("AURION_BOOTSTRAP_KEY")

api_key_header = APIKeyHeader(name="x-api-key", auto_error=False)


def hash_key(raw_key: str) -> str:
    """SHA-256 hash for API key storage. Fast, deterministic, no timing leaks via hmac.compare_digest."""
    return hashlib.sha256(raw_key.encode()).hexdigest()


def generate_api_key() -> tuple[str, str, str]:
    """Generate a new API key. Returns (raw_key, prefix, key_hash)."""
    raw = f"aurion_{secrets.token_urlsafe(32)}"
    prefix = raw[:12]
    h = hash_key(raw)
    return raw, prefix, h


async def get_current_api_key(
    request: Request,
    key_from_header: str | None = Security(api_key_header),
) -> APIKey | None:
    """Dependency that validates the API key and returns the key object."""
    if not AUTH_ENABLED:
        return None

    # Try x-api-key header first, then Authorization: Bearer
    raw_key = key_from_header
    if not raw_key:
        auth = request.headers.get("authorization", "")
        if auth.startswith("Bearer "):
            raw_key = auth[7:]

    if not raw_key:
        raise HTTPException(status_code=401, detail="Missing API key")

    # Check bootstrap key
    if BOOTSTRAP_KEY and hmac.compare_digest(raw_key, BOOTSTRAP_KEY):
        return APIKey(
            id="bootstrap",
            name="bootstrap",
            key_hash="",
            prefix="bootstrap",
        )

    # Validate against database
    prefix = raw_key[:12]
    repo = Repository()
    stored_key = await repo.get_api_key_by_prefix(prefix)
    if not stored_key:
        raise HTTPException(status_code=401, detail="Invalid API key")

    # Timing-safe comparison
    provided_hash = hash_key(raw_key)
    if not hmac.compare_digest(provided_hash, stored_key.key_hash):
        raise HTTPException(status_code=401, detail="Invalid API key")

    # Update last_used_at (fire and forget)
    try:
        await repo.touch_api_key(stored_key.id)
    except Exception:
        pass

    return stored_key


# Health/docs endpoints are always public
PUBLIC_PATHS = {"/health", "/docs", "/openapi.json", "/redoc"}


def is_public_path(path: str) -> bool:
    return path in PUBLIC_PATHS
