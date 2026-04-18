"""E2B sandbox — Firecracker microVM isolation via E2B cloud."""

from __future__ import annotations

import structlog

from packages.sandbox.base import ExecResult, Sandbox, SandboxConfig, SandboxProvider

logger = structlog.get_logger()


class E2BSandbox(Sandbox):
    """Sandbox backed by E2B's Firecracker microVM."""

    def __init__(self, sandbox):
        self._sandbox = sandbox  # e2b.Sandbox instance
        self._closed = False

    async def execute(self, command: str, timeout: int = 30) -> ExecResult:
        if self._closed:
            return ExecResult(stderr="Sandbox is closed", exit_code=1)
        try:
            result = await self._sandbox.commands.run(
                command, timeout=timeout
            )
            return ExecResult(
                stdout=result.stdout or "",
                stderr=result.stderr or "",
                exit_code=result.exit_code,
            )
        except Exception as e:
            return ExecResult(stderr=str(e), exit_code=1)

    async def read_file(self, path: str) -> str:
        try:
            content = await self._sandbox.files.read(path)
            return content
        except Exception as e:
            raise FileNotFoundError(f"Cannot read {path}: {e}")

    async def write_file(self, path: str, content: str) -> None:
        await self._sandbox.files.write(path, content)

    async def list_files(self, pattern: str = "*") -> list[str]:
        result = await self.execute(f"find /home/user -name '{pattern}' -type f 2>/dev/null | head -1000")
        if result.exit_code != 0:
            return []
        return [line for line in result.stdout.strip().split("\n") if line]

    async def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        try:
            await self._sandbox.kill()
            logger.info("e2b_sandbox_closed")
        except Exception as e:
            logger.warning("e2b_sandbox_close_error", error=str(e))


class E2BSandboxProvider(SandboxProvider):
    """Creates E2B microVM sandboxes."""

    def __init__(self, template: str = "base"):
        self._template = template

    async def create(self, config: SandboxConfig) -> E2BSandbox:
        from e2b import AsyncSandbox

        sandbox = await AsyncSandbox.create(
            template=self._template,
            timeout=config.timeout_seconds,
        )

        # Install packages
        if config.packages:
            pkg_list = " ".join(config.packages)
            await sandbox.commands.run(
                f"pip install {pkg_list} 2>/dev/null || "
                f"apt-get update -qq && apt-get install -y -qq {pkg_list} 2>/dev/null",
                timeout=120,
            )

        # Mount files
        for path, content in config.mounted_files.items():
            await sandbox.files.write(path, content)

        logger.info(
            "e2b_sandbox_created",
            template=self._template,
            sandbox_id=sandbox.sandbox_id,
        )
        return E2BSandbox(sandbox)
