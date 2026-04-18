"""Event Bus — Redis-backed pub/sub + SSE streaming.

Implements the bidirectional event stream that is central to the Managed Agents
architecture. Events are:
1. Persisted to PostgreSQL (append-only session log)
2. Published to Redis pub/sub (real-time streaming)
3. Delivered to SSE clients via async generators
"""

from __future__ import annotations

import asyncio
import json
import uuid
from datetime import datetime
from typing import AsyncIterator

import redis.asyncio as aioredis
import structlog

from packages.core.models import Event, EventType

logger = structlog.get_logger()


class EventBus:
    """Central event bus for session event streaming."""

    def __init__(self, redis_url: str = "redis://localhost:6379/0"):
        self._redis_url = redis_url
        self._redis: aioredis.Redis | None = None
        self._subscribers: dict[str, list[asyncio.Queue]] = {}

    async def connect(self) -> None:
        """Initialize Redis connection."""
        self._redis = aioredis.from_url(
            self._redis_url,
            decode_responses=True,
        )
        logger.info("event_bus_connected", redis_url=self._redis_url)

    async def close(self) -> None:
        """Close Redis connection."""
        if self._redis:
            await self._redis.close()

    def _channel(self, session_id: str) -> str:
        return f"aurion:session:{session_id}:events"

    async def emit(self, session_id: str, event: Event) -> None:
        """Emit an event — publish to Redis and notify local subscribers."""
        event_data = event.model_dump_json()

        # Publish to Redis for cross-process delivery
        if self._redis:
            await self._redis.publish(self._channel(session_id), event_data)

            # Also append to Redis list for event replay
            await self._redis.rpush(
                f"aurion:session:{session_id}:log", event_data
            )
            # Auto-expire after 7 days
            await self._redis.expire(
                f"aurion:session:{session_id}:log", 604800
            )

        # Notify local in-process subscribers
        if session_id in self._subscribers:
            for queue in self._subscribers[session_id]:
                try:
                    queue.put_nowait(event)
                except asyncio.QueueFull:
                    logger.warning("event_queue_full", session_id=session_id)

        logger.debug(
            "event_emitted",
            session_id=session_id,
            type=event.type,
            seq=event.sequence_num,
        )

    async def subscribe(self, session_id: str) -> AsyncIterator[Event]:
        """Subscribe to events for a session. Yields events as they arrive."""
        queue: asyncio.Queue[Event | None] = asyncio.Queue(maxsize=1000)

        if session_id not in self._subscribers:
            self._subscribers[session_id] = []
        self._subscribers[session_id].append(queue)

        try:
            while True:
                event = await queue.get()
                if event is None:
                    break
                yield event
        finally:
            self._subscribers[session_id].remove(queue)
            if not self._subscribers[session_id]:
                del self._subscribers[session_id]

    async def subscribe_redis(self, session_id: str) -> AsyncIterator[Event]:
        """Subscribe via Redis pub/sub — works across processes."""
        if not self._redis:
            return

        pubsub = self._redis.pubsub()
        await pubsub.subscribe(self._channel(session_id))

        try:
            async for message in pubsub.listen():
                if message["type"] == "message":
                    data = message["data"]
                    if isinstance(data, str):
                        event = Event.model_validate_json(data)
                        yield event
        finally:
            await pubsub.unsubscribe(self._channel(session_id))
            await pubsub.close()

    async def get_events(
        self,
        session_id: str,
        start: int = 0,
        end: int = -1,
    ) -> list[Event]:
        """Retrieve events from the session log (positional slices).

        This implements getEvents() from the Anthropic architecture — the
        session log is an append-only event store that lives outside the
        context window, accessible via positional slices.
        """
        if not self._redis:
            return []

        raw_events = await self._redis.lrange(
            f"aurion:session:{session_id}:log", start, end
        )
        return [Event.model_validate_json(e) for e in raw_events]

    async def close_session_stream(self, session_id: str) -> None:
        """Signal all subscribers that the session stream is done."""
        if session_id in self._subscribers:
            for queue in self._subscribers[session_id]:
                try:
                    queue.put_nowait(None)
                except asyncio.QueueFull:
                    pass

    def create_event(
        self,
        session_id: str,
        event_type: EventType,
        payload: dict | None = None,
        thread_id: str | None = None,
        sequence_num: int = 0,
    ) -> Event:
        """Helper to create a properly formed Event."""
        return Event(
            id=str(uuid.uuid4()),
            session_id=session_id,
            thread_id=thread_id,
            type=event_type,
            payload=payload or {},
            sequence_num=sequence_num,
            created_at=datetime.utcnow(),
        )
