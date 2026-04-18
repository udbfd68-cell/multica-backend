"""File storage backend for the Aurion Agent Runtime.

Stores uploaded files on local disk under a configurable base path.
Each file is stored with its UUID-based file ID to avoid collisions.
In production, swap for S3/GCS via the same interface.
"""

from __future__ import annotations

import os
import shutil
from pathlib import Path

import structlog

logger = structlog.get_logger()

FILES_STORAGE_PATH = os.environ.get(
    "FILES_STORAGE_PATH",
    os.path.join(os.path.dirname(__file__), "..", "..", "data", "files"),
)


class FileStorage:
    """Local disk file storage."""

    def __init__(self, base_path: str = FILES_STORAGE_PATH):
        self._base = Path(base_path).resolve()
        self._base.mkdir(parents=True, exist_ok=True)

    def _safe_path(self, file_id: str) -> Path:
        """Return a safe path for a file ID, preventing directory traversal."""
        safe_id = Path(file_id).name  # strip any directory components
        return self._base / safe_id

    async def save(self, file_id: str, data: bytes) -> str:
        """Save file data and return the storage path."""
        path = self._safe_path(file_id)
        path.write_bytes(data)
        logger.info("file_saved", file_id=file_id, size=len(data), path=str(path))
        return str(path)

    async def read(self, storage_path: str) -> bytes:
        """Read file data from storage."""
        path = Path(storage_path).resolve()
        # Ensure path is within our base directory
        if not str(path).startswith(str(self._base)):
            raise PermissionError("Access denied: path outside storage directory")
        if not path.exists():
            raise FileNotFoundError(f"File not found: {storage_path}")
        return path.read_bytes()

    async def delete(self, storage_path: str) -> bool:
        """Delete a file from storage. Returns True if deleted."""
        path = Path(storage_path).resolve()
        if not str(path).startswith(str(self._base)):
            raise PermissionError("Access denied: path outside storage directory")
        if path.exists():
            path.unlink()
            logger.info("file_deleted", path=str(path))
            return True
        return False

    async def exists(self, storage_path: str) -> bool:
        """Check if a file exists in storage."""
        path = Path(storage_path).resolve()
        return path.exists() and str(path).startswith(str(self._base))
