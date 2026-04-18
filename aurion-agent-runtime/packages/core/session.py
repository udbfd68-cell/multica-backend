"""Session Orchestrator - the brain of the agent runtime.

Implements:
1. ReAct loop with LLM calls and tool execution
2. user.interrupt handling (cancels current LLM call)
3. Retry/error system with retrying/exhausted/terminal states
4. Observability spans (span.model_request_start/end)
5. Context compaction (agent.thread_context_compacted)
6. Custom tool and MCP tool routing
7. Tool confirmation workflow
8. Session stats and usage tracking
9. PostgreSQL event persistence via Repository
"""

from __future__ import annotations

import asyncio
import time
import uuid
from datetime import datetime
from typing import Any

import structlog

from packages.core.event_bus import EventBus
from packages.core.llm_providers import LLMProvider
from packages.core.models import (
    Agent,
    Environment,
    Event,
    EventType,
    LLMContentBlock,
    LLMMessage,
    LLMResponse,
    RetryStatus,
    Session,
    SessionStats,
    SessionStatus,
    SessionUsage,
    StopReason,
    ToolDefinition,
    ToolResult,
)
from packages.core.tool_executor import ToolExecutor
from packages.sandbox.base import Sandbox, SandboxConfig
from packages.sandbox.factory import create_sandbox

logger = structlog.get_logger()

# Retry configuration
MAX_RETRIES = 3
RETRY_BACKOFF_BASE = 2.0  # exponential backoff: 2, 4, 8 seconds

# Error types matching Anthropic's
ERROR_TYPES = {
    "billing": "billing_error",
    "overloaded": "model_overloaded",
    "rate_limit": "model_rate_limited",
    "model_failed": "model_request_failed",
    "mcp_auth": "mcp_authentication_failed",
    "mcp_connection": "mcp_connection_failed",
    "unknown": "unknown",
}


class SessionOrchestrator:
    """Manages a single agent session lifecycle."""

    def __init__(
        self,
        agent: Agent,
        environment: Environment,
        session: Session,
        llm: LLMProvider,
        tool_executor: ToolExecutor,
        event_bus: EventBus,
        repository: Any = None,
    ):
        self.agent = agent
        self.environment = environment
        self.session = session
        self.llm = llm
        self.tool_executor = tool_executor
        self.event_bus = event_bus
        self.repo = repository

        self._sandbox: Sandbox | None = None
        self._sequence = 0
        self._running = False
        self._interrupted = False
        self._start_time: float | None = None
        self._active_start: float | None = None

        # Tool confirmation futures
        self._tool_confirmations: dict[str, asyncio.Future] = {}
        # Custom tool result futures
        self._custom_tool_results: dict[str, asyncio.Future] = {}

    async def start(self) -> None:
        """Initialize the session: create sandbox, connect MCP, start loop."""
        logger.info("session_starting", session_id=self.session.id, agent=self.agent.name)
        self._start_time = time.monotonic()

        # Create sandbox based on environment config
        networking = "full"
        if hasattr(self.environment, "config") and self.environment.config:
            net_config = self.environment.config
            if hasattr(net_config, "networking") and net_config.networking:
                if net_config.networking.type == "restricted":
                    networking = "restricted"

        config = SandboxConfig(
            working_dir="/home/user",
            packages=self.environment.packages or [],
            network_policy=networking,
        )
        self._sandbox = await create_sandbox(
            config=config,
            provider_name=self.environment.sandbox_provider,
        )
        self.tool_executor.set_sandbox(self._sandbox)

        await self._emit(EventType.SESSION_STATUS_RUNNING, {})
        self._running = True

    async def process_message(self, content: str, attachments: list[dict] | None = None) -> None:
        """Process a user message - the main entry point for the ReAct loop."""
        if not self._running:
            await self.start()

        self._interrupted = False
        self._active_start = time.monotonic()

        # Emit user message event
        await self._emit(EventType.USER_MESSAGE, {
            "content": content,
            "attachments": attachments or [],
        })

        # Build message history from events
        messages = await self._build_messages()

        # Build tool list
        tools = self.tool_executor.get_builtin_tool_definitions()

        # Add MCP tools if available
        if self.tool_executor._mcp:
            tools.extend(self.tool_executor._mcp.get_all_tool_definitions())

        # Add callable agents as tools
        if self.agent.callable_agents:
            for ca in self.agent.callable_agents:
                tools.append(ToolDefinition(
                    name=f"agent_{ca.name}",
                    description=f"Delegate to sub-agent '{ca.name}': {ca.description or ''}",
                    input_schema={
                        "type": "object",
                        "properties": {
                            "message": {"type": "string", "description": "Message to send"},
                        },
                        "required": ["message"],
                    },
                ))

        system = self._build_system_prompt()
        await self._react_loop(system, messages, tools)

    async def interrupt(self) -> None:
        """Handle user.interrupt - cancel the current operation."""
        self._interrupted = True
        logger.info("session_interrupted", session_id=self.session.id)

        # Update session status to idle with requires_action stop reason
        await self._update_session_status(
            SessionStatus.IDLE,
            stop_reason=StopReason.REQUIRES_ACTION,
        )

    async def handle_tool_confirmation(
        self, tool_use_id: str, result: str, deny_message: str | None = None
    ) -> None:
        """Handle user.tool_confirmation response."""
        if tool_use_id in self._tool_confirmations:
            self._tool_confirmations[tool_use_id].set_result({
                "result": result,
                "deny_message": deny_message,
            })

    async def handle_custom_tool_result(
        self, custom_tool_use_id: str, content: Any = None, is_error: bool = False
    ) -> None:
        """Handle user.custom_tool_result response."""
        if custom_tool_use_id in self._custom_tool_results:
            self._custom_tool_results[custom_tool_use_id].set_result({
                "content": content,
                "is_error": is_error,
            })

    async def _react_loop(
        self,
        system: str,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        max_turns: int = 50,
    ) -> None:
        """Run the ReAct loop with retry logic and interrupt handling."""
        turn = 0
        pending_confirmations: list[str] = []

        while turn < max_turns and not self._interrupted:
            turn += 1

            # Call LLM with retry logic
            response = await self._call_llm_with_retry(messages, system, tools)
            if response is None:
                break  # retries exhausted or terminal error

            # Process response content blocks
            text_parts = []
            tool_calls = []
            thinking_parts = []

            for block in response.content:
                if block.type == "text":
                    text_parts.append(block.text or "")
                elif block.type == "tool_use":
                    tool_calls.append(block)
                elif block.type == "thinking":
                    thinking_parts.append(block.text or "")

            # Emit thinking event if present
            if thinking_parts:
                await self._emit(EventType.AGENT_THINKING, {
                    "content": "\n".join(thinking_parts),
                })

            # Update usage tracking
            usage = response.usage or {}
            self.session.usage.input_tokens += usage.get("input_tokens", 0)
            self.session.usage.output_tokens += usage.get("output_tokens", 0)
            self.session.usage.cache_read_input_tokens += usage.get("cache_read_input_tokens", 0)

            # Emit agent message if there is text
            if text_parts:
                await self._emit(EventType.AGENT_MESSAGE, {
                    "content": "\n".join(text_parts),
                    "model": response.model,
                    "usage": usage,
                })

            # If no tool calls, we are done
            if not tool_calls:
                break

            if self._interrupted:
                break

            # Execute tool calls
            tool_results = []
            for tc in tool_calls:
                if self._interrupted:
                    break

                # Determine if this is an MCP tool
                is_mcp = tc.name and "." in tc.name and not tc.name.startswith("agent_")
                # Determine if this is a custom tool
                is_custom = any(
                    hasattr(t, "type") and t.type == "custom" and hasattr(t, "name") and t.name == tc.name
                    for t in (self.agent.tools or [])
                    if hasattr(t, "name")
                )

                if is_mcp:
                    # Emit MCP tool use
                    await self._emit(EventType.AGENT_MCP_TOOL_USE, {
                        "call_id": tc.id,
                        "server_name": tc.name.split(".")[0] if tc.name else "",
                        "tool_name": tc.name,
                        "input": tc.input,
                    })
                elif is_custom:
                    # Emit custom tool use and wait for client result
                    await self._emit(EventType.AGENT_CUSTOM_TOOL_USE, {
                        "call_id": tc.id,
                        "name": tc.name,
                        "input": tc.input,
                    })
                    # Wait for client to provide result
                    future: asyncio.Future = asyncio.get_event_loop().create_future()
                    self._custom_tool_results[tc.id or ""] = future
                    pending_confirmations.append(tc.id or "")

                    # Go idle waiting for custom tool result
                    await self._update_session_status(
                        SessionStatus.IDLE,
                        stop_reason=StopReason.REQUIRES_ACTION,
                    )

                    try:
                        result_data = await asyncio.wait_for(future, timeout=300)
                        tool_results.append(ToolResult(
                            call_id=tc.id or "",
                            output=str(result_data.get("content", "")),
                            is_error=result_data.get("is_error", False),
                        ))
                    except asyncio.TimeoutError:
                        tool_results.append(ToolResult(
                            call_id=tc.id or "",
                            output="Custom tool timed out",
                            is_error=True,
                        ))
                    finally:
                        self._custom_tool_results.pop(tc.id or "", None)
                    continue
                else:
                    # Regular builtin tool
                    await self._emit(EventType.AGENT_TOOL_USE, {
                        "call_id": tc.id,
                        "name": tc.name,
                        "input": tc.input,
                    })

                # Check tool permissions
                needs_confirmation = self._needs_confirmation(tc.name or "")
                if needs_confirmation:
                    future = asyncio.get_event_loop().create_future()
                    self._tool_confirmations[tc.id or ""] = future
                    pending_confirmations.append(tc.id or "")

                    await self._update_session_status(
                        SessionStatus.IDLE,
                        stop_reason=StopReason.REQUIRES_ACTION,
                    )

                    try:
                        confirmation = await asyncio.wait_for(future, timeout=300)
                        if confirmation["result"] == "deny":
                            tool_results.append(ToolResult(
                                call_id=tc.id or "",
                                output=confirmation.get("deny_message", "Tool use denied by user"),
                                is_error=True,
                            ))
                            await self._emit(EventType.AGENT_TOOL_RESULT, {
                                "call_id": tc.id, "name": tc.name,
                                "output": "Denied by user", "is_error": True,
                            })
                            continue
                    except asyncio.TimeoutError:
                        tool_results.append(ToolResult(
                            call_id=tc.id or "",
                            output="Tool confirmation timed out",
                            is_error=True,
                        ))
                        continue
                    finally:
                        self._tool_confirmations.pop(tc.id or "", None)

                    # Re-enter running state
                    await self._emit(EventType.SESSION_STATUS_RUNNING, {})

                # Execute the tool
                result = await self.tool_executor.execute(
                    name=tc.name or "",
                    call_id=tc.id or str(uuid.uuid4()),
                    input_data=tc.input or {},
                )
                tool_results.append(result)

                # Emit tool result event
                result_event_type = EventType.AGENT_MCP_TOOL_RESULT if is_mcp else EventType.AGENT_TOOL_RESULT
                await self._emit(result_event_type, {
                    "call_id": tc.id,
                    "name": tc.name,
                    "output": result.output[:10000],
                    "is_error": result.is_error,
                })

            if self._interrupted:
                break

            # Add assistant message and tool results to conversation
            messages.append(LLMMessage(
                role="assistant",
                content=[b.model_dump() for b in response.content],
            ))

            messages.append(LLMMessage(
                role="user",
                content=[
                    {
                        "type": "tool_result",
                        "tool_use_id": r.call_id,
                        "content": r.output,
                        "is_error": r.is_error,
                    }
                    for r in tool_results
                ],
            ))

            # Check for context compaction
            if self._should_compact(messages):
                messages = await self._compact_context(messages, system)
                await self._emit(EventType.AGENT_THREAD_CONTEXT_COMPACTED, {
                    "original_messages": len(messages),
                })

            if response.stop_reason == "end_turn":
                break

        # Update session stats
        if self._active_start:
            active_elapsed = time.monotonic() - self._active_start
            self.session.stats.active_seconds += active_elapsed
        if self._start_time:
            self.session.stats.duration_seconds = time.monotonic() - self._start_time

        # Persist session updates
        if self.repo:
            await self.repo.update_session(self.session)

        # Session idle
        stop = StopReason.END_TURN
        if self._interrupted:
            stop = StopReason.REQUIRES_ACTION
        await self._update_session_status(SessionStatus.IDLE, stop_reason=stop)

    async def _call_llm_with_retry(
        self,
        messages: list[LLMMessage],
        system: str,
        tools: list[ToolDefinition],
    ) -> LLMResponse | None:
        """Call LLM with exponential backoff retry for transient errors."""
        model_id = self.agent.model.id if hasattr(self.agent.model, "id") else "claude-sonnet-4-20250514"

        for attempt in range(MAX_RETRIES + 1):
            if self._interrupted:
                return None

            # Emit span start
            span_id = f"span_{uuid.uuid4().hex[:24]}"
            await self._emit(EventType.SPAN_MODEL_REQUEST_START, {
                "span_id": span_id,
                "model": model_id,
                "attempt": attempt + 1,
            })

            try:
                response = await self.llm.chat(
                    messages=messages,
                    system=system,
                    tools=tools,
                    model=model_id,
                )

                # Emit span end (success)
                await self._emit(EventType.SPAN_MODEL_REQUEST_END, {
                    "model_request_start_id": span_id,
                    "is_error": False,
                    "usage": response.usage or {},
                })

                return response

            except Exception as e:
                error_str = str(e).lower()

                # Classify error
                if "rate" in error_str and "limit" in error_str:
                    error_type = ERROR_TYPES["rate_limit"]
                elif "overloaded" in error_str or "529" in error_str:
                    error_type = ERROR_TYPES["overloaded"]
                elif "billing" in error_str or "402" in error_str:
                    error_type = ERROR_TYPES["billing"]
                    # Billing errors are terminal
                    await self._emit_error(error_type, str(e), RetryStatus.TERMINAL)
                    await self._emit(EventType.SPAN_MODEL_REQUEST_END, {
                        "model_request_start_id": span_id,
                        "is_error": True,
                    })
                    await self._update_session_status(SessionStatus.TERMINATED)
                    return None
                elif "mcp" in error_str and "auth" in error_str:
                    error_type = ERROR_TYPES["mcp_auth"]
                    await self._emit_error(error_type, str(e), RetryStatus.TERMINAL)
                    await self._update_session_status(SessionStatus.TERMINATED)
                    return None
                elif "mcp" in error_str and ("connection" in error_str or "connect" in error_str):
                    error_type = ERROR_TYPES["mcp_connection"]
                else:
                    error_type = ERROR_TYPES["model_failed"]

                is_last_attempt = attempt == MAX_RETRIES
                retry_status = RetryStatus.EXHAUSTED if is_last_attempt else RetryStatus.RETRYING

                await self._emit(EventType.SPAN_MODEL_REQUEST_END, {
                    "model_request_start_id": span_id,
                    "is_error": True,
                })
                await self._emit_error(error_type, str(e), retry_status)

                if is_last_attempt:
                    await self._update_session_status(
                        SessionStatus.IDLE,
                        stop_reason=StopReason.RETRIES_EXHAUSTED,
                    )
                    return None

                # Exponential backoff
                wait = RETRY_BACKOFF_BASE ** (attempt + 1)
                await self._emit(EventType.SESSION_STATUS_RESCHEDULED, {
                    "retry_in_seconds": wait,
                    "attempt": attempt + 1,
                })
                await asyncio.sleep(wait)

        return None

    async def _emit_error(self, error_type: str, message: str, retry_status: RetryStatus) -> None:
        """Emit a session.error event."""
        await self._emit(EventType.SESSION_ERROR, {
            "type": error_type,
            "message": message,
            "retry_status": {"type": retry_status},
        })

    async def _update_session_status(
        self, status: SessionStatus, stop_reason: StopReason | None = None
    ) -> None:
        """Update session status and emit the corresponding event."""
        self.session.status = status
        self.session.stop_reason = stop_reason

        if status == SessionStatus.IDLE:
            payload = {}
            if stop_reason:
                payload["stop_reason"] = {"type": stop_reason}
            await self._emit(EventType.SESSION_STATUS_IDLE, payload)
        elif status == SessionStatus.RUNNING:
            await self._emit(EventType.SESSION_STATUS_RUNNING, {})
        elif status == SessionStatus.TERMINATED:
            payload = {}
            if stop_reason:
                payload["stop_reason"] = {"type": stop_reason}
            await self._emit(EventType.SESSION_STATUS_TERMINATED, payload)

        if self.repo:
            await self.repo.update_session(self.session)

    def _build_system_prompt(self) -> str:
        parts = [self.agent.system or "You are a helpful AI agent."]
        if self.agent.description:
            parts.append(f"\n## Description\n{self.agent.description}")
        parts.append(
            "\n## Tool Usage\n"
            "You have access to tools for interacting with the sandbox filesystem, "
            "running commands, fetching web content, and searching. "
            "Use tools proactively to accomplish tasks. "
            "Always verify your work before reporting completion."
        )
        return "\n".join(parts)

    async def _build_messages(self) -> list[LLMMessage]:
        """Build LLM messages from the session event log.

        Reconstructs the full conversation including tool_use and tool_result
        blocks so the LLM retains multi-turn tool context.
        """
        if self.repo:
            events = await self.repo.list_events(self.session.id, limit=1000)
        else:
            events = await self.event_bus.get_events(self.session.id)

        messages: list[LLMMessage] = []
        # Buffer to accumulate assistant content blocks (text + tool_use)
        assistant_blocks: list[dict[str, Any]] = []
        # Buffer to accumulate tool_result blocks (user role)
        tool_result_blocks: list[dict[str, Any]] = []

        def flush_assistant():
            nonlocal assistant_blocks
            if assistant_blocks:
                messages.append(LLMMessage(role="assistant", content=assistant_blocks))
                assistant_blocks = []

        def flush_tool_results():
            nonlocal tool_result_blocks
            if tool_result_blocks:
                messages.append(LLMMessage(role="user", content=tool_result_blocks))
                tool_result_blocks = []

        for event in events:
            payload = event.payload or {}

            if event.type == EventType.USER_MESSAGE:
                flush_tool_results()
                flush_assistant()
                messages.append(LLMMessage(
                    role="user",
                    content=payload.get("content", ""),
                ))

            elif event.type == EventType.AGENT_MESSAGE:
                # Flush any pending tool results before new assistant turn
                flush_tool_results()
                assistant_blocks.append({
                    "type": "text",
                    "text": payload.get("content", ""),
                })

            elif event.type in (EventType.AGENT_TOOL_USE, EventType.AGENT_MCP_TOOL_USE, EventType.AGENT_CUSTOM_TOOL_USE):
                assistant_blocks.append({
                    "type": "tool_use",
                    "id": payload.get("call_id", ""),
                    "name": payload.get("name", payload.get("tool_name", "")),
                    "input": payload.get("input", {}),
                })

            elif event.type in (EventType.AGENT_TOOL_RESULT, EventType.AGENT_MCP_TOOL_RESULT):
                # Flush assistant blocks before adding tool results
                flush_assistant()
                tool_result_blocks.append({
                    "type": "tool_result",
                    "tool_use_id": payload.get("call_id", ""),
                    "content": payload.get("output", ""),
                    "is_error": payload.get("is_error", False),
                })

        # Flush remaining buffers
        flush_tool_results()
        flush_assistant()

        return messages

    def _needs_confirmation(self, tool_name: str) -> bool:
        for tool in (self.agent.tools or []):
            if hasattr(tool, "name") and tool.name == tool_name:
                if hasattr(tool, "permission") and tool.permission == "always_ask":
                    return True
        return False

    def _should_compact(self, messages: list[LLMMessage]) -> bool:
        total_chars = sum(len(str(m.content)) for m in messages)
        estimated_tokens = total_chars // 4
        return estimated_tokens > 80_000

    async def _compact_context(
        self,
        messages: list[LLMMessage],
        system: str,
    ) -> list[LLMMessage]:
        if len(messages) <= 10:
            return messages

        to_summarize = messages[:-6]
        to_keep = messages[-6:]

        summary_prompt = (
            "Summarize the following conversation history concisely, "
            "preserving key facts, decisions, and file paths mentioned:\n\n"
        )
        for msg in to_summarize:
            summary_prompt += f"[{msg.role}]: {str(msg.content)[:500]}\n"

        model_id = self.agent.model.id if hasattr(self.agent.model, "id") else "claude-sonnet-4-20250514"

        try:
            response = await self.llm.chat(
                messages=[LLMMessage(role="user", content=summary_prompt)],
                system="You are a conversation summarizer. Be concise but preserve important details.",
                model=model_id,
            )
            summary_text = ""
            for block in response.content:
                if block.type == "text":
                    summary_text += block.text or ""

            compacted = [
                LLMMessage(
                    role="user",
                    content=f"[Previous conversation summary]: {summary_text}",
                ),
            ] + to_keep

            logger.info(
                "context_compacted",
                session_id=self.session.id,
                original_msgs=len(messages),
                compacted_msgs=len(compacted),
            )
            return compacted

        except Exception as e:
            logger.warning("context_compaction_failed", error=str(e))
            return messages[-10:]

    async def _emit(self, event_type: EventType, payload: dict) -> None:
        """Emit an event to the stream and persist to DB."""
        self._sequence += 1
        event = self.event_bus.create_event(
            session_id=self.session.id,
            event_type=event_type,
            payload=payload,
            thread_id=None,
            sequence_num=self._sequence,
        )
        await self.event_bus.emit(self.session.id, event)
        # Persist to PostgreSQL
        if self.repo:
            try:
                await self.repo.create_event(event)
            except Exception as e:
                logger.warning("event_persist_failed", error=str(e))

    async def stop(self) -> None:
        """Stop the session and clean up resources."""
        self._running = False
        self._interrupted = True
        if self._sandbox:
            await self._sandbox.close()
        await self._update_session_status(SessionStatus.IDLE, stop_reason=StopReason.END_TURN)
        logger.info("session_stopped", session_id=self.session.id)
