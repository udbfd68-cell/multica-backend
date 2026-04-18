"""Multi-Agent Coordinator — orchestrates delegation between agents.

Implements the multi-agent pattern from Anthropic's architecture:
- A coordinator agent can spawn sub-agent threads
- Sub-agents run in their own context with shared filesystem
- Communication via session events (thread_message_sent/received)
- Sub-agent sessions are fully persisted in PostgreSQL
"""

from __future__ import annotations

import uuid
from typing import Any

import structlog

from packages.core.event_bus import EventBus
from packages.core.models import (
    Agent,
    CallableAgent,
    Environment,
    EventType,
    Session,
    SessionCreate,
)

logger = structlog.get_logger()


class MultiAgentCoordinator:
    """Coordinates multi-agent delegation.

    When a coordinator agent calls `agent_<name>`, this coordinator:
    1. Creates a new session thread for the sub-agent
    2. Persists the sub-session in PostgreSQL
    3. Sends the message to the sub-agent's session
    4. Waits for the sub-agent to complete
    5. Returns the result to the coordinator
    """

    def __init__(
        self,
        event_bus: EventBus,
        agents: dict[str, Agent],
        environment: Environment,
        repository: Any = None,
    ):
        self._event_bus = event_bus
        self._agents = agents
        self._environment = environment
        self._repository = repository
        self._active_threads: dict[str, Any] = {}

    async def delegate(
        self,
        from_session_id: str,
        agent_name: str,
        message: str,
    ) -> str:
        """Delegate a task to a sub-agent and wait for completion."""
        if agent_name not in self._agents:
            return f"Error: Agent '{agent_name}' not found"

        sub_agent = self._agents[agent_name]
        thread_id = str(uuid.uuid4())

        # Emit thread created event
        event = self._event_bus.create_event(
            session_id=from_session_id,
            event_type=EventType.SESSION_THREAD_CREATED,
            payload={
                "thread_id": thread_id,
                "agent_name": agent_name,
                "message": message,
            },
            thread_id=thread_id,
        )
        await self._event_bus.emit(from_session_id, event)

        # Emit message sent event
        sent_event = self._event_bus.create_event(
            session_id=from_session_id,
            event_type=EventType.AGENT_THREAD_MESSAGE_SENT,
            payload={
                "thread_id": thread_id,
                "content": message,
                "to_agent": agent_name,
            },
            thread_id=thread_id,
        )
        await self._event_bus.emit(from_session_id, sent_event)

        # Create sub-agent session — persisted to PostgreSQL
        from packages.core.session import SessionOrchestrator
        from packages.core.llm_providers import LLMProvider
        from packages.core.tool_executor import ToolExecutor

        sub_session = Session(
            agent=sub_agent,
            agent_id=sub_agent.id,
            environment_id=self._environment.id,
            title=f"Sub-agent thread: {agent_name}",
            metadata={"parent_session_id": from_session_id, "thread_id": thread_id},
        )

        # Persist sub-session to PostgreSQL
        if self._repository:
            await self._repository.create_session(sub_session)

        llm = LLMProvider()
        tool_executor = ToolExecutor()

        orchestrator = SessionOrchestrator(
            agent=sub_agent,
            environment=self._environment,
            session=sub_session,
            llm=llm,
            tool_executor=tool_executor,
            event_bus=self._event_bus,
            repository=self._repository,
        )

        self._active_threads[thread_id] = {
            "session_id": sub_session.id,
            "orchestrator": orchestrator,
            "agent_name": agent_name,
        }

        try:
            await orchestrator.process_message(message)

            # Collect the sub-agent's final response from persisted events
            if self._repository:
                events = await self._repository.list_events(sub_session.id, limit=1000)
            else:
                events = await self._event_bus.get_events(sub_session.id)

            response_parts = []
            for e in events:
                if e.type == EventType.AGENT_MESSAGE:
                    response_parts.append(e.payload.get("content", ""))

            result = "\n".join(response_parts) if response_parts else "Sub-agent completed without text output."

        except Exception as e:
            result = f"Sub-agent error: {str(e)}"
        finally:
            await orchestrator.stop()
            self._active_threads.pop(thread_id, None)

        # Emit message received event
        received_event = self._event_bus.create_event(
            session_id=from_session_id,
            event_type=EventType.AGENT_THREAD_MESSAGE_RECEIVED,
            payload={
                "thread_id": thread_id,
                "content": result,
                "from_agent": agent_name,
            },
            thread_id=thread_id,
        )
        await self._event_bus.emit(from_session_id, received_event)

        # Emit thread idle event — sub-agent finished its work
        idle_event = self._event_bus.create_event(
            session_id=from_session_id,
            event_type=EventType.SESSION_THREAD_IDLE,
            payload={
                "thread_id": thread_id,
                "agent_name": agent_name,
            },
            thread_id=thread_id,
        )
        await self._event_bus.emit(from_session_id, idle_event)

        logger.info(
            "agent_delegation_complete",
            from_session=from_session_id,
            agent=agent_name,
            thread=thread_id,
            response_length=len(result),
        )

        return result
