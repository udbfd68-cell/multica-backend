"""MCP Connector — connects to Model Context Protocol servers.

Manages lifecycle and communication with MCP tool servers using the
official Python MCP SDK. Supports stdio, SSE, and streamable-http transports.
Namespaces tools as "server_name.tool_name" to avoid collisions.
"""

from __future__ import annotations

import asyncio
import json
from typing import Any

import structlog

from packages.core.models import McpServerConfig, ToolDefinition

logger = structlog.get_logger()


class McpConnection:
    """A single connection to an MCP server."""

    def __init__(self, config: McpServerConfig):
        self.config = config
        self.name = config.name
        self._session = None
        self._client = None
        self._tools: list[dict[str, Any]] = []

    async def connect(self) -> None:
        """Establish connection to the MCP server."""
        from mcp import ClientSession
        from mcp.client.stdio import StdioServerParameters, stdio_client
        from mcp.client.sse import sse_client

        transport = self.config.transport or "stdio"

        if transport == "stdio":
            if not self.config.command:
                raise ValueError(f"MCP server {self.name}: stdio requires 'command'")

            params = StdioServerParameters(
                command=self.config.command,
                args=self.config.args or [],
                env=self.config.env or {},
            )
            self._read, self._write = await stdio_client(params).__aenter__()  # type: ignore
            self._session = ClientSession(self._read, self._write)  # type: ignore

        elif transport in ("sse", "streamable-http"):
            if not self.config.url:
                raise ValueError(f"MCP server {self.name}: {transport} requires 'url'")
            self._read, self._write = await sse_client(
                self.config.url,
                headers=self.config.headers or {},
            ).__aenter__()  # type: ignore
            self._session = ClientSession(self._read, self._write)  # type: ignore

        else:
            raise ValueError(f"Unknown MCP transport: {transport}")

        await self._session.initialize()

        # Discover tools
        tools_response = await self._session.list_tools()
        self._tools = [
            {
                "name": t.name,
                "description": t.description or "",
                "input_schema": t.inputSchema if hasattr(t, "inputSchema") else {},
            }
            for t in tools_response.tools
        ]

        logger.info(
            "mcp_connected",
            server=self.name,
            transport=transport,
            tools=len(self._tools),
        )

    async def call_tool(self, tool_name: str, arguments: dict[str, Any]) -> str:
        """Call a tool on this MCP server."""
        if not self._session:
            raise RuntimeError(f"MCP server {self.name} not connected")

        result = await self._session.call_tool(tool_name, arguments=arguments)

        # Flatten content blocks to string
        parts = []
        for block in result.content:
            if hasattr(block, "text"):
                parts.append(block.text)
            elif hasattr(block, "data"):
                parts.append(f"[binary: {len(block.data)} bytes]")
            else:
                parts.append(str(block))

        return "\n".join(parts)

    def get_tool_definitions(self) -> list[ToolDefinition]:
        """Return namespaced tool definitions from this server."""
        defs = []
        for t in self._tools:
            defs.append(
                ToolDefinition(
                    name=f"{self.name}.{t['name']}",
                    description=f"[{self.name}] {t['description']}",
                    input_schema=t.get("input_schema", {}),
                )
            )
        return defs

    async def close(self) -> None:
        """Close the MCP connection."""
        if self._session:
            try:
                await self._session.__aexit__(None, None, None)
            except Exception:
                pass
        logger.info("mcp_disconnected", server=self.name)


class McpConnector:
    """Manages multiple MCP server connections.

    Provides a unified namespace for tool discovery and execution across
    all connected MCP servers.
    """

    def __init__(self):
        self._connections: dict[str, McpConnection] = {}

    async def add_server(self, config: McpServerConfig) -> None:
        """Connect to an MCP server and register its tools."""
        conn = McpConnection(config)
        await conn.connect()
        self._connections[config.name] = conn
        logger.info("mcp_server_added", name=config.name, tools=len(conn._tools))

    async def add_servers(self, configs: list[McpServerConfig]) -> None:
        """Connect to multiple MCP servers concurrently."""
        tasks = [self.add_server(c) for c in configs]
        results = await asyncio.gather(*tasks, return_exceptions=True)
        for config, result in zip(configs, results):
            if isinstance(result, Exception):
                logger.error("mcp_server_failed", name=config.name, error=str(result))

    async def call_tool(self, namespaced_name: str, arguments: dict[str, Any]) -> str:
        """Call a tool by its namespaced name ("server.tool")."""
        if "." not in namespaced_name:
            raise ValueError(f"Tool name must be namespaced as 'server.tool': {namespaced_name}")

        server_name, tool_name = namespaced_name.split(".", 1)

        if server_name not in self._connections:
            raise ValueError(f"MCP server not found: {server_name}")

        return await self._connections[server_name].call_tool(tool_name, arguments)

    def get_all_tool_definitions(self) -> list[ToolDefinition]:
        """Return all tool definitions from all connected servers."""
        defs = []
        for conn in self._connections.values():
            defs.extend(conn.get_tool_definitions())
        return defs

    async def close(self) -> None:
        """Close all MCP connections."""
        for conn in self._connections.values():
            await conn.close()
        self._connections.clear()
