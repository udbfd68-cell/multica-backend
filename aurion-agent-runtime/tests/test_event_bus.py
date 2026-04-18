"""Tests for the Event Bus."""

import asyncio
import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from packages.core.event_bus import EventBus
from packages.core.models import Event, EventType


class TestEventBusLocal:
    """Test local (in-process) event bus functionality."""

    @pytest.fixture
    def bus(self):
        bus = EventBus()
        bus._redis = None  # Disable Redis for local tests
        return bus

    @pytest.mark.asyncio
    async def test_create_event(self, bus):
        event = bus.create_event(
            session_id="session-1",
            event_type=EventType.USER_MESSAGE,
            payload={"content": "Hello"},
            sequence_num=1,
        )
        assert event.session_id == "session-1"
        assert event.type == EventType.USER_MESSAGE
        assert event.payload["content"] == "Hello"

    @pytest.mark.asyncio
    async def test_local_subscribe_and_emit(self, bus):
        received = []

        async def collect_events():
            async for event in bus.subscribe("session-1"):
                received.append(event)
                if len(received) >= 2:
                    break

        # Start subscriber
        task = asyncio.create_task(collect_events())

        # Give subscriber time to register
        await asyncio.sleep(0.01)

        # Emit events
        e1 = bus.create_event("session-1", EventType.USER_MESSAGE, {"content": "Hello"}, sequence_num=1)
        e2 = bus.create_event("session-1", EventType.AGENT_MESSAGE, {"content": "Hi"}, sequence_num=2)

        await bus.emit("session-1", e1)
        await bus.emit("session-1", e2)

        await asyncio.wait_for(task, timeout=2.0)

        assert len(received) == 2
        assert received[0].type == EventType.USER_MESSAGE
        assert received[1].type == EventType.AGENT_MESSAGE

    @pytest.mark.asyncio
    async def test_close_session_stream(self, bus):
        received = []

        async def collect_events():
            async for event in bus.subscribe("session-2"):
                received.append(event)

        task = asyncio.create_task(collect_events())
        await asyncio.sleep(0.01)

        e1 = bus.create_event("session-2", EventType.USER_MESSAGE, {"content": "Test"}, sequence_num=1)
        await bus.emit("session-2", e1)
        await asyncio.sleep(0.01)

        await bus.close_session_stream("session-2")
        await asyncio.wait_for(task, timeout=2.0)

        assert len(received) == 1


class TestEventBusRedis:
    """Test Redis-backed event bus (mocked)."""

    @pytest.mark.asyncio
    async def test_emit_publishes_to_redis(self):
        bus = EventBus()
        bus._redis = AsyncMock()
        bus._redis.publish = AsyncMock()
        bus._redis.rpush = AsyncMock()
        bus._redis.expire = AsyncMock()

        event = bus.create_event("session-1", EventType.USER_MESSAGE, {"content": "Test"}, sequence_num=1)
        await bus.emit("session-1", event)

        bus._redis.publish.assert_called_once()
        bus._redis.rpush.assert_called_once()

    @pytest.mark.asyncio
    async def test_get_events_from_redis(self):
        bus = EventBus()
        bus._redis = AsyncMock()

        event = bus.create_event("session-1", EventType.USER_MESSAGE, {"content": "Test"}, sequence_num=1)
        bus._redis.lrange = AsyncMock(return_value=[event.model_dump_json()])

        events = await bus.get_events("session-1")
        assert len(events) == 1
        assert events[0].type == EventType.USER_MESSAGE
