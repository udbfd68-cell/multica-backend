"""Tests for the Tool Executor."""

import pytest
from unittest.mock import AsyncMock, MagicMock

from packages.core.tool_executor import ToolExecutor
from packages.sandbox.base import ExecResult, Sandbox


class MockSandbox(Sandbox):
    """Mock sandbox for testing."""

    def __init__(self):
        self._files: dict[str, str] = {}
        self._exec_results: list[ExecResult] = []

    def add_exec_result(self, result: ExecResult):
        self._exec_results.append(result)

    async def execute(self, command: str, timeout: int = 30) -> ExecResult:
        if self._exec_results:
            return self._exec_results.pop(0)
        return ExecResult(stdout="mock output", exit_code=0)

    async def read_file(self, path: str) -> str:
        if path not in self._files:
            raise FileNotFoundError(f"Not found: {path}")
        return self._files[path]

    async def write_file(self, path: str, content: str) -> None:
        self._files[path] = content

    async def list_files(self, pattern: str = "*") -> list[str]:
        return list(self._files.keys())

    async def close(self) -> None:
        pass


class TestToolExecutor:
    @pytest.fixture
    def sandbox(self):
        return MockSandbox()

    @pytest.fixture
    def executor(self, sandbox):
        return ToolExecutor(sandbox=sandbox)

    @pytest.mark.asyncio
    async def test_bash_tool(self, executor, sandbox):
        sandbox.add_exec_result(ExecResult(stdout="hello world\n", exit_code=0))
        result = await executor.execute("bash", "call-1", {"command": "echo hello world"})
        assert "hello world" in result.output
        assert not result.is_error

    @pytest.mark.asyncio
    async def test_write_and_read(self, executor, sandbox):
        # Write
        result = await executor.execute("write", "call-1", {
            "path": "/test.txt",
            "content": "Hello, World!",
        })
        assert not result.is_error

        # Read
        result = await executor.execute("read", "call-2", {"path": "/test.txt"})
        assert result.output == "Hello, World!"

    @pytest.mark.asyncio
    async def test_edit_tool(self, executor, sandbox):
        sandbox._files["/test.py"] = "print('hello')\nprint('world')\n"

        result = await executor.execute("edit", "call-1", {
            "path": "/test.py",
            "old_string": "hello",
            "new_string": "goodbye",
        })
        assert not result.is_error
        assert sandbox._files["/test.py"] == "print('goodbye')\nprint('world')\n"

    @pytest.mark.asyncio
    async def test_edit_not_found(self, executor, sandbox):
        sandbox._files["/test.py"] = "print('hello')\n"

        result = await executor.execute("edit", "call-1", {
            "path": "/test.py",
            "old_string": "nonexistent",
            "new_string": "replacement",
        })
        assert "not found" in result.output.lower()

    @pytest.mark.asyncio
    async def test_glob_tool(self, executor, sandbox):
        sandbox._files["/a.py"] = ""
        sandbox._files["/b.py"] = ""
        result = await executor.execute("glob", "call-1", {"pattern": "*.py"})
        # glob actually calls sandbox.list_files which returns our mock files
        assert not result.is_error

    @pytest.mark.asyncio
    async def test_unknown_tool(self, executor):
        result = await executor.execute("nonexistent_tool", "call-1", {})
        assert result.is_error
        assert "Unknown tool" in result.output

    @pytest.mark.asyncio
    async def test_no_sandbox_raises(self):
        executor = ToolExecutor(sandbox=None)
        result = await executor.execute("bash", "call-1", {"command": "ls"})
        assert result.is_error
        assert "Sandbox not available" in result.output

    def test_builtin_tool_definitions(self, executor):
        tools = executor.get_builtin_tool_definitions()
        names = [t.name for t in tools]
        assert "bash" in names
        assert "read" in names
        assert "write" in names
        assert "edit" in names
        assert "glob" in names
        assert "grep" in names
        assert "web_fetch" in names
        assert "web_search" in names
        assert len(tools) == 8
