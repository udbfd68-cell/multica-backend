"""Observability — Langfuse integration for tracing and monitoring.

Provides structured tracing for the entire agentic loop:
- Session traces
- LLM call spans (with token usage)
- Tool execution spans
- Multi-agent delegation spans
"""

from __future__ import annotations

import os
import time
from contextlib import asynccontextmanager
from typing import Any

import structlog

logger = structlog.get_logger()


class ObservabilityTracer:
    """Langfuse-backed tracing for agent sessions."""

    def __init__(
        self,
        public_key: str | None = None,
        secret_key: str | None = None,
        host: str | None = None,
    ):
        self._public_key = public_key or os.environ.get("LANGFUSE_PUBLIC_KEY", "")
        self._secret_key = secret_key or os.environ.get("LANGFUSE_SECRET_KEY", "")
        self._host = host or os.environ.get("LANGFUSE_HOST", "https://cloud.langfuse.com")
        self._langfuse = None
        self._enabled = False

    async def connect(self) -> None:
        """Initialize Langfuse client."""
        if not self._public_key or not self._secret_key:
            logger.info("langfuse_disabled", reason="no credentials")
            return

        try:
            from langfuse import Langfuse

            self._langfuse = Langfuse(
                public_key=self._public_key,
                secret_key=self._secret_key,
                host=self._host,
            )
            self._enabled = True
            logger.info("langfuse_connected", host=self._host)
        except ImportError:
            logger.warning("langfuse not installed")
        except Exception as e:
            logger.warning("langfuse_connect_failed", error=str(e))

    def trace_session(
        self,
        session_id: str,
        agent_name: str,
        metadata: dict[str, Any] | None = None,
    ) -> SessionTrace:
        """Start a new trace for an agent session."""
        trace = None
        if self._enabled and self._langfuse:
            trace = self._langfuse.trace(
                name=f"session-{session_id}",
                session_id=session_id,
                metadata={
                    "agent": agent_name,
                    **(metadata or {}),
                },
            )
        return SessionTrace(trace=trace, enabled=self._enabled)

    async def flush(self) -> None:
        """Flush pending events to Langfuse."""
        if self._langfuse:
            self._langfuse.flush()

    async def close(self) -> None:
        """Shutdown Langfuse client."""
        if self._langfuse:
            self._langfuse.shutdown()


class SessionTrace:
    """Trace context for a single session."""

    def __init__(self, trace: Any = None, enabled: bool = False):
        self._trace = trace
        self._enabled = enabled

    def span_llm_call(
        self,
        model: str,
        messages: list[dict],
        response: dict | None = None,
        usage: dict | None = None,
    ) -> None:
        """Record an LLM call span."""
        if not self._enabled or not self._trace:
            return

        try:
            generation = self._trace.generation(
                name="llm-call",
                model=model,
                input=messages,
                output=response,
                usage=usage,
            )
            generation.end()
        except Exception as e:
            logger.debug("trace_llm_call_failed", error=str(e))

    def span_tool_execution(
        self,
        tool_name: str,
        input_data: dict,
        output: str,
        duration_ms: float,
        is_error: bool = False,
    ) -> None:
        """Record a tool execution span."""
        if not self._enabled or not self._trace:
            return

        try:
            span = self._trace.span(
                name=f"tool-{tool_name}",
                input=input_data,
                output=output,
                metadata={
                    "duration_ms": duration_ms,
                    "is_error": is_error,
                },
            )
            span.end()
        except Exception as e:
            logger.debug("trace_tool_span_failed", error=str(e))

    def span_delegation(
        self,
        agent_name: str,
        message: str,
        result: str,
        duration_ms: float,
    ) -> None:
        """Record a multi-agent delegation span."""
        if not self._enabled or not self._trace:
            return

        try:
            span = self._trace.span(
                name=f"delegate-{agent_name}",
                input={"message": message},
                output=result,
                metadata={"duration_ms": duration_ms},
            )
            span.end()
        except Exception as e:
            logger.debug("trace_delegation_failed", error=str(e))

    def event(self, name: str, metadata: dict[str, Any] | None = None) -> None:
        """Record a custom event."""
        if not self._enabled or not self._trace:
            return

        try:
            self._trace.event(name=name, metadata=metadata or {})
        except Exception as e:
            logger.debug("trace_event_failed", error=str(e))

    def score(self, name: str, value: float, comment: str | None = None) -> None:
        """Add a score to the trace."""
        if not self._enabled or not self._trace:
            return

        try:
            self._trace.score(name=name, value=value, comment=comment)
        except Exception as e:
            logger.debug("trace_score_failed", error=str(e))
