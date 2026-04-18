"""Tool Executor — executes tools in sandboxes, via MCP, or delegates to clients.

Implements the `execute(name, input) → string` universal interface from
Anthropic's architecture. The executor routes tool calls to the appropriate
backend (sandbox, MCP server, web search, or client-side custom tools).
"""

from __future__ import annotations

import json
from typing import Any

import httpx
import structlog

from packages.core.models import ToolDefinition, ToolResult
from packages.sandbox.base import Sandbox

logger = structlog.get_logger()


class ToolExecutor:
    """Executes tools on behalf of the agent harness.

    Built-in tools (equivalent to agent_toolset_20260401):
      - bash, read, write, edit, glob, grep, web_fetch, web_search

    Also routes MCP tool calls and custom tool calls.
    """

    def __init__(
        self,
        sandbox: Sandbox | None = None,
        mcp_connector: Any | None = None,
        tavily_api_key: str | None = None,
    ):
        self._sandbox = sandbox
        self._mcp = mcp_connector
        self._tavily_key = tavily_api_key
        self._custom_tool_handlers: dict[str, Any] = {}

    @property
    def sandbox(self) -> Sandbox | None:
        return self._sandbox

    def set_sandbox(self, sandbox: Sandbox) -> None:
        self._sandbox = sandbox

    async def execute(self, name: str, call_id: str, input_data: dict[str, Any]) -> ToolResult:
        """execute(name, input) → string — the universal tool interface."""
        try:
            output = await self._dispatch(name, input_data)
            return ToolResult(call_id=call_id, output=output)
        except Exception as e:
            logger.error("tool_execution_error", tool=name, error=str(e))
            return ToolResult(call_id=call_id, output=str(e), is_error=True)

    async def _dispatch(self, name: str, input_data: dict[str, Any]) -> str:
        """Route a tool call to the right executor."""
        # Built-in tools
        handlers = {
            "bash": self._bash,
            "read": self._read,
            "write": self._write,
            "edit": self._edit,
            "glob": self._glob,
            "grep": self._grep,
            "web_fetch": self._web_fetch,
            "web_search": self._web_search,
        }

        if name in handlers:
            return await handlers[name](input_data)

        # MCP tools (namespaced: "server.tool")
        if self._mcp and "." in name:
            return await self._mcp_call(name, input_data)

        raise ValueError(f"Unknown tool: {name}")

    # ── Built-in Tools ──────────────────────────────────────────────────────

    async def _bash(self, input_data: dict) -> str:
        """Execute a bash command in the sandbox."""
        self._require_sandbox()
        command = input_data.get("command", "")
        timeout = input_data.get("timeout", 30)
        result = await self._sandbox.execute(command, timeout=timeout)  # type: ignore

        output = result.stdout
        if result.stderr:
            output += f"\nSTDERR:\n{result.stderr}"
        if result.exit_code != 0:
            output += f"\n[exit code: {result.exit_code}]"
        return output

    async def _read(self, input_data: dict) -> str:
        """Read a file from the sandbox filesystem."""
        self._require_sandbox()
        path = input_data.get("path", input_data.get("file_path", ""))
        return await self._sandbox.read_file(path)  # type: ignore

    async def _write(self, input_data: dict) -> str:
        """Write content to a file in the sandbox."""
        self._require_sandbox()
        path = input_data.get("path", input_data.get("file_path", ""))
        content = input_data.get("content", "")
        await self._sandbox.write_file(path, content)  # type: ignore
        return f"File written: {path} ({len(content)} bytes)"

    async def _edit(self, input_data: dict) -> str:
        """Perform string replacement in a file."""
        self._require_sandbox()
        path = input_data.get("path", input_data.get("file_path", ""))
        old_string = input_data.get("old_string", input_data.get("old_str", ""))
        new_string = input_data.get("new_string", input_data.get("new_str", ""))

        content = await self._sandbox.read_file(path)  # type: ignore
        if old_string not in content:
            return f"Error: old_string not found in {path}"

        count = content.count(old_string)
        if count > 1:
            return f"Error: old_string found {count} times in {path} (expected exactly 1)"

        new_content = content.replace(old_string, new_string, 1)
        await self._sandbox.write_file(path, new_content)  # type: ignore
        return f"File edited: {path}"

    async def _glob(self, input_data: dict) -> str:
        """Find files matching a glob pattern."""
        self._require_sandbox()
        pattern = input_data.get("pattern", "*")
        files = await self._sandbox.list_files(pattern)  # type: ignore
        return "\n".join(files) if files else "No files found"

    async def _grep(self, input_data: dict) -> str:
        """Search for a regex pattern in files."""
        self._require_sandbox()
        pattern = input_data.get("pattern", "")
        path = input_data.get("path", "/home/user")
        include = input_data.get("include", "")

        cmd = f"grep -rn {_shell_quote(pattern)} {_shell_quote(path)}"
        if include:
            cmd += f" --include={_shell_quote(include)}"
        cmd += " 2>/dev/null | head -200"

        result = await self._sandbox.execute(cmd)  # type: ignore
        return result.stdout if result.stdout else "No matches found"

    async def _web_fetch(self, input_data: dict) -> str:
        """Fetch content from a URL."""
        url = input_data.get("url", "")
        if not url:
            return "Error: url is required"

        async with httpx.AsyncClient(
            follow_redirects=True,
            timeout=30,
            headers={"User-Agent": "Aurion-Agent/1.0"},
        ) as client:
            resp = await client.get(url)
            content_type = resp.headers.get("content-type", "")

            if "text" in content_type or "json" in content_type or "xml" in content_type:
                text = resp.text
                # Truncate large responses
                if len(text) > 100_000:
                    text = text[:100_000] + "\n\n[truncated — content exceeds 100KB]"
                return text
            else:
                return f"Binary content ({content_type}), {len(resp.content)} bytes"

    async def _web_search(self, input_data: dict) -> str:
        """Search the web using Tavily."""
        query = input_data.get("query", "")
        if not query:
            return "Error: query is required"

        if not self._tavily_key:
            return "Error: TAVILY_API_KEY not configured"

        from tavily import AsyncTavilyClient

        client = AsyncTavilyClient(api_key=self._tavily_key)
        results = await client.search(
            query=query,
            max_results=input_data.get("max_results", 5),
            include_answer=True,
        )

        output_parts = []
        if results.get("answer"):
            output_parts.append(f"Answer: {results['answer']}\n")

        for r in results.get("results", []):
            output_parts.append(f"- [{r['title']}]({r['url']})\n  {r.get('content', '')[:500]}")

        return "\n\n".join(output_parts) if output_parts else "No results found"

    # ── MCP Tools ───────────────────────────────────────────────────────────

    async def _mcp_call(self, name: str, input_data: dict) -> str:
        """Route a call to an MCP server."""
        if self._mcp:
            return await self._mcp.call_tool(name, input_data)
        raise ValueError(f"MCP not configured for tool: {name}")

    # ── Helpers ─────────────────────────────────────────────────────────────

    def _require_sandbox(self) -> None:
        if self._sandbox is None:
            raise RuntimeError("Sandbox not available. Create an environment first.")

    def get_builtin_tool_definitions(self) -> list[ToolDefinition]:
        """Return definitions for all built-in tools."""
        return [
            ToolDefinition(
                name="bash",
                description="Execute a bash command in the sandbox shell. Returns stdout, stderr, and exit code.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "command": {"type": "string", "description": "The bash command to run"},
                        "timeout": {"type": "integer", "description": "Timeout in seconds (default 30)", "default": 30},
                    },
                    "required": ["command"],
                },
            ),
            ToolDefinition(
                name="read",
                description="Read a file from the local filesystem.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "Absolute path to the file"},
                    },
                    "required": ["path"],
                },
            ),
            ToolDefinition(
                name="write",
                description="Write content to a file on the local filesystem. Creates parent directories if needed.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "Absolute path to the file"},
                        "content": {"type": "string", "description": "Content to write"},
                    },
                    "required": ["path", "content"],
                },
            ),
            ToolDefinition(
                name="edit",
                description="Perform an exact string replacement in a file. The old_string must appear exactly once.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "path": {"type": "string", "description": "Path to the file"},
                        "old_string": {"type": "string", "description": "Exact text to find (must appear once)"},
                        "new_string": {"type": "string", "description": "Replacement text"},
                    },
                    "required": ["path", "old_string", "new_string"],
                },
            ),
            ToolDefinition(
                name="glob",
                description="Find files matching a glob pattern.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "pattern": {"type": "string", "description": "Glob pattern (e.g. '*.py')"},
                    },
                    "required": ["pattern"],
                },
            ),
            ToolDefinition(
                name="grep",
                description="Search for a regex pattern in files.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "pattern": {"type": "string", "description": "Regex pattern to search for"},
                        "path": {"type": "string", "description": "Directory to search in", "default": "/home/user"},
                        "include": {"type": "string", "description": "File pattern to include (e.g. '*.py')"},
                    },
                    "required": ["pattern"],
                },
            ),
            ToolDefinition(
                name="web_fetch",
                description="Fetch content from a URL. Returns the response body.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "url": {"type": "string", "description": "The URL to fetch"},
                    },
                    "required": ["url"],
                },
            ),
            ToolDefinition(
                name="web_search",
                description="Search the web for information. Returns relevant results with snippets.",
                input_schema={
                    "type": "object",
                    "properties": {
                        "query": {"type": "string", "description": "Search query"},
                        "max_results": {"type": "integer", "description": "Max results to return", "default": 5},
                    },
                    "required": ["query"],
                },
            ),
        ]


def _shell_quote(s: str) -> str:
    """Shell-escape a string."""
    return "'" + s.replace("'", "'\"'\"'") + "'"
