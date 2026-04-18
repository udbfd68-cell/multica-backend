"""Memory System — session + long-term memory via Graphiti.

Implements the two-tier memory approach:
1. Session log (short-term): Redis-backed append-only event stream
2. Long-term memory: Graphiti knowledge graph with FalkorDB

The session log is already handled by EventBus. This module provides
the long-term memory layer — extracting facts from conversations and
storing them in a knowledge graph for retrieval across sessions.
"""

from __future__ import annotations

import os
from datetime import datetime
from typing import Any

import structlog

logger = structlog.get_logger()


class MemoryStore:
    """Long-term memory backed by Graphiti + FalkorDB."""

    def __init__(
        self,
        falkordb_url: str | None = None,
        falkordb_password: str | None = None,
    ):
        self._falkordb_url = falkordb_url or os.environ.get("FALKORDB_URL", "bolt://localhost:6379")
        self._falkordb_password = falkordb_password or os.environ.get("FALKORDB_PASSWORD", "")
        self._graphiti = None

    async def connect(self) -> None:
        """Initialize the Graphiti knowledge graph client."""
        try:
            from graphiti_core import Graphiti
            from graphiti_core.llm_client import OpenAIClient

            self._graphiti = Graphiti(
                self._falkordb_url,
                OpenAIClient(),  # Graphiti uses OpenAI for entity extraction
            )
            await self._graphiti.build_indices_and_constraints()
            logger.info("memory_store_connected")
        except ImportError:
            logger.warning("graphiti not installed, memory disabled")
        except Exception as e:
            logger.warning("memory_store_connection_failed", error=str(e))

    async def add_episode(
        self,
        session_id: str,
        content: str,
        source: str = "conversation",
        metadata: dict[str, Any] | None = None,
    ) -> None:
        """Add an episode (conversation turn) to the knowledge graph.

        Graphiti will automatically extract entities and relationships.
        """
        if not self._graphiti:
            return

        try:
            await self._graphiti.add_episode(
                name=f"session-{session_id}",
                episode_body=content,
                source_description=source,
                reference_time=datetime.utcnow(),
            )
        except Exception as e:
            logger.warning("memory_add_episode_failed", error=str(e))

    async def search(
        self,
        query: str,
        limit: int = 10,
        center_node_uuid: str | None = None,
    ) -> list[dict[str, Any]]:
        """Search the knowledge graph for relevant memories."""
        if not self._graphiti:
            return []

        try:
            results = await self._graphiti.search(
                query=query,
                num_results=limit,
            )
            return [
                {
                    "fact": r.fact if hasattr(r, "fact") else str(r),
                    "uuid": r.uuid if hasattr(r, "uuid") else "",
                    "created_at": str(r.created_at) if hasattr(r, "created_at") else "",
                }
                for r in results
            ]
        except Exception as e:
            logger.warning("memory_search_failed", error=str(e))
            return []

    async def get_relevant_context(
        self,
        query: str,
        session_id: str | None = None,
        limit: int = 5,
    ) -> str:
        """Get relevant memory context formatted for injection into system prompt."""
        memories = await self.search(query, limit=limit)
        if not memories:
            return ""

        parts = ["## Relevant Memory\n"]
        for m in memories:
            parts.append(f"- {m['fact']}")

        return "\n".join(parts)

    async def close(self) -> None:
        """Close the memory store connection."""
        if self._graphiti:
            try:
                await self._graphiti.close()
            except Exception:
                pass
