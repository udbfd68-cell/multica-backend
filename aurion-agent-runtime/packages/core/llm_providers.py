"""Multi-provider LLM adapter.

Supports Anthropic, OpenAI, Gemini, Groq, and Ollama through a unified
interface. Each provider streams responses in a normalized format.
"""

from __future__ import annotations

import os
from typing import AsyncIterator

import structlog

from packages.core.models import (
    ContentBlock,
    LLMMessage,
    LLMProviderType,
    LLMResponse,
    ToolDefinition,
)

logger = structlog.get_logger()


class LLMProvider:
    """Unified LLM provider that delegates to the appropriate backend."""

    def __init__(self, provider: LLMProviderType, model_id: str):
        self.provider = provider
        self.model_id = model_id
        self._client = None

    def _get_anthropic(self):
        import anthropic
        if not self._client:
            self._client = anthropic.AsyncAnthropic(
                api_key=os.environ["ANTHROPIC_API_KEY"]
            )
        return self._client

    def _get_openai(self):
        import openai
        if not self._client:
            self._client = openai.AsyncOpenAI(api_key=os.environ["OPENAI_API_KEY"])
        return self._client

    def _get_groq(self):
        import groq
        if not self._client:
            self._client = groq.AsyncGroq(api_key=os.environ["GROQ_API_KEY"])
        return self._client

    def _get_ollama(self):
        import openai
        base_url = os.environ.get("OLLAMA_BASE_URL", "http://localhost:11434")
        if not self._client:
            self._client = openai.AsyncOpenAI(
                base_url=f"{base_url}/v1", api_key="ollama"
            )
        return self._client

    async def create_message(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str = "",
        max_tokens: int = 16384,
    ) -> LLMResponse:
        """Send messages to the LLM and return a complete response."""
        if self.provider == LLMProviderType.ANTHROPIC:
            return await self._anthropic_message(messages, tools, system, max_tokens)
        elif self.provider in (
            LLMProviderType.OPENAI,
            LLMProviderType.GROQ,
            LLMProviderType.OLLAMA,
        ):
            return await self._openai_message(messages, tools, system, max_tokens)
        elif self.provider == LLMProviderType.GEMINI:
            return await self._gemini_message(messages, tools, system, max_tokens)
        else:
            raise ValueError(f"Unsupported provider: {self.provider}")

    async def stream_message(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str = "",
        max_tokens: int = 16384,
    ) -> AsyncIterator[ContentBlock]:
        """Stream response blocks from the LLM."""
        if self.provider == LLMProviderType.ANTHROPIC:
            async for block in self._anthropic_stream(messages, tools, system, max_tokens):
                yield block
        elif self.provider in (
            LLMProviderType.OPENAI,
            LLMProviderType.GROQ,
            LLMProviderType.OLLAMA,
        ):
            async for block in self._openai_stream(messages, tools, system, max_tokens):
                yield block
        elif self.provider == LLMProviderType.GEMINI:
            async for block in self._gemini_stream(messages, tools, system, max_tokens):
                yield block

    # ── Anthropic ───────────────────────────────────────────────────────────

    def _to_anthropic_messages(self, messages: list[LLMMessage]) -> list[dict]:
        result = []
        for msg in messages:
            if isinstance(msg.content, str):
                result.append({"role": msg.role, "content": msg.content})
            else:
                blocks = []
                for b in msg.content:
                    if b.type == "text":
                        blocks.append({"type": "text", "text": b.text or ""})
                    elif b.type == "tool_use":
                        blocks.append({
                            "type": "tool_use",
                            "id": b.tool_use_id,
                            "name": b.tool_name,
                            "input": b.input or {},
                        })
                    elif b.type == "tool_result":
                        blocks.append({
                            "type": "tool_result",
                            "tool_use_id": b.tool_use_id,
                            "content": b.content or "",
                            "is_error": b.is_error,
                        })
                result.append({"role": msg.role, "content": blocks})
        return result

    def _to_anthropic_tools(self, tools: list[ToolDefinition]) -> list[dict]:
        return [
            {
                "name": t.name,
                "description": t.description,
                "input_schema": t.input_schema,
            }
            for t in tools
            if t.enabled
        ]

    async def _anthropic_message(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str,
        max_tokens: int,
    ) -> LLMResponse:
        client = self._get_anthropic()
        kwargs: dict = {
            "model": self.model_id,
            "max_tokens": max_tokens,
            "messages": self._to_anthropic_messages(messages),
        }
        if system:
            kwargs["system"] = system
        ant_tools = self._to_anthropic_tools(tools)
        if ant_tools:
            kwargs["tools"] = ant_tools

        resp = await client.messages.create(**kwargs)

        blocks = []
        for b in resp.content:
            if b.type == "text":
                blocks.append(ContentBlock(type="text", text=b.text))
            elif b.type == "tool_use":
                blocks.append(ContentBlock(
                    type="tool_use",
                    tool_use_id=b.id,
                    tool_name=b.name,
                    input=b.input,
                ))

        return LLMResponse(
            content=blocks,
            stop_reason=resp.stop_reason,
            usage={
                "input_tokens": resp.usage.input_tokens,
                "output_tokens": resp.usage.output_tokens,
            },
        )

    async def _anthropic_stream(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str,
        max_tokens: int,
    ) -> AsyncIterator[ContentBlock]:
        client = self._get_anthropic()
        kwargs: dict = {
            "model": self.model_id,
            "max_tokens": max_tokens,
            "messages": self._to_anthropic_messages(messages),
        }
        if system:
            kwargs["system"] = system
        ant_tools = self._to_anthropic_tools(tools)
        if ant_tools:
            kwargs["tools"] = ant_tools

        async with client.messages.stream(**kwargs) as stream:
            async for event in stream:
                if hasattr(event, "type"):
                    if event.type == "content_block_start":
                        block = event.content_block
                        if block.type == "text":
                            yield ContentBlock(type="text", text="")
                        elif block.type == "tool_use":
                            yield ContentBlock(
                                type="tool_use",
                                tool_use_id=block.id,
                                tool_name=block.name,
                                input={},
                            )
                    elif event.type == "content_block_delta":
                        delta = event.delta
                        if hasattr(delta, "text"):
                            yield ContentBlock(type="text_delta", text=delta.text)

    # ── OpenAI / Groq / Ollama ──────────────────────────────────────────────

    def _get_openai_client(self):
        if self.provider == LLMProviderType.GROQ:
            return self._get_groq()
        elif self.provider == LLMProviderType.OLLAMA:
            return self._get_ollama()
        return self._get_openai()

    def _to_openai_messages(
        self, messages: list[LLMMessage], system: str
    ) -> list[dict]:
        result = []
        if system:
            result.append({"role": "system", "content": system})
        for msg in messages:
            if isinstance(msg.content, str):
                result.append({"role": msg.role, "content": msg.content})
            else:
                parts = []
                tool_calls = []
                tool_results = []
                for b in msg.content:
                    if b.type == "text":
                        parts.append({"type": "text", "text": b.text or ""})
                    elif b.type == "tool_use":
                        import json
                        tool_calls.append({
                            "id": b.tool_use_id,
                            "type": "function",
                            "function": {
                                "name": b.tool_name,
                                "arguments": json.dumps(b.input or {}),
                            },
                        })
                    elif b.type == "tool_result":
                        tool_results.append({
                            "role": "tool",
                            "tool_call_id": b.tool_use_id,
                            "content": b.content or "",
                        })

                if tool_calls:
                    result.append({
                        "role": "assistant",
                        "content": parts if parts else None,
                        "tool_calls": tool_calls,
                    })
                elif parts:
                    result.append({"role": msg.role, "content": parts})

                result.extend(tool_results)
        return result

    def _to_openai_tools(self, tools: list[ToolDefinition]) -> list[dict]:
        return [
            {
                "type": "function",
                "function": {
                    "name": t.name,
                    "description": t.description,
                    "parameters": t.input_schema,
                },
            }
            for t in tools
            if t.enabled
        ]

    async def _openai_message(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str,
        max_tokens: int,
    ) -> LLMResponse:
        import json
        client = self._get_openai_client()
        kwargs: dict = {
            "model": self.model_id,
            "max_tokens": max_tokens,
            "messages": self._to_openai_messages(messages, system),
        }
        oai_tools = self._to_openai_tools(tools)
        if oai_tools:
            kwargs["tools"] = oai_tools

        resp = await client.chat.completions.create(**kwargs)
        choice = resp.choices[0]

        blocks = []
        if choice.message.content:
            blocks.append(ContentBlock(type="text", text=choice.message.content))

        if choice.message.tool_calls:
            for tc in choice.message.tool_calls:
                blocks.append(ContentBlock(
                    type="tool_use",
                    tool_use_id=tc.id,
                    tool_name=tc.function.name,
                    input=json.loads(tc.function.arguments),
                ))

        stop = "end_turn" if choice.finish_reason == "stop" else choice.finish_reason
        return LLMResponse(
            content=blocks,
            stop_reason=stop,
            usage={
                "input_tokens": resp.usage.prompt_tokens if resp.usage else 0,
                "output_tokens": resp.usage.completion_tokens if resp.usage else 0,
            },
        )

    async def _openai_stream(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str,
        max_tokens: int,
    ) -> AsyncIterator[ContentBlock]:
        # For simplicity, fall back to non-streaming and yield blocks
        resp = await self._openai_message(messages, tools, system, max_tokens)
        for block in resp.content:
            yield block

    # ── Gemini ──────────────────────────────────────────────────────────────

    async def _gemini_message(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str,
        max_tokens: int,
    ) -> LLMResponse:
        from google import genai

        client = genai.Client(api_key=os.environ["GOOGLE_API_KEY"])

        # Build contents
        contents = []
        for msg in messages:
            text = msg.content if isinstance(msg.content, str) else str(msg.content)
            contents.append(genai.types.Content(
                role="user" if msg.role == "user" else "model",
                parts=[genai.types.Part(text=text)],
            ))

        config = genai.types.GenerateContentConfig(
            system_instruction=system if system else None,
            max_output_tokens=max_tokens,
        )

        response = await client.aio.models.generate_content(
            model=self.model_id,
            contents=contents,
            config=config,
        )

        blocks = []
        if response.text:
            blocks.append(ContentBlock(type="text", text=response.text))

        usage_meta = response.usage_metadata
        return LLMResponse(
            content=blocks,
            stop_reason="end_turn",
            usage={
                "input_tokens": usage_meta.prompt_token_count if usage_meta else 0,
                "output_tokens": usage_meta.candidates_token_count if usage_meta else 0,
            },
        )

    async def _gemini_stream(
        self,
        messages: list[LLMMessage],
        tools: list[ToolDefinition],
        system: str,
        max_tokens: int,
    ) -> AsyncIterator[ContentBlock]:
        resp = await self._gemini_message(messages, tools, system, max_tokens)
        for block in resp.content:
            yield block
