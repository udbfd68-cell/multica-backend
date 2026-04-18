"""Docker sandbox — local container-based isolation.

This is the default sandbox provider, requiring only Docker on the host.
Each sandbox runs in an isolated container with configurable network access.
"""

from __future__ import annotations

import asyncio
import uuid

import structlog

from packages.sandbox.base import ExecResult, Sandbox, SandboxConfig, SandboxProvider

logger = structlog.get_logger()


class DockerSandbox(Sandbox):
    """Sandbox backed by a local Docker container."""

    def __init__(self, container_id: str, container_name: str):
        self._container_id = container_id
        self._container_name = container_name
        self._closed = False

    @property
    def container_id(self) -> str:
        return self._container_id

    async def execute(self, command: str, timeout: int = 30) -> ExecResult:
        if self._closed:
            return ExecResult(stderr="Sandbox is closed", exit_code=1)

        try:
            proc = await asyncio.create_subprocess_exec(
                "docker", "exec", self._container_id,
                "bash", "-c", command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await asyncio.wait_for(
                proc.communicate(), timeout=timeout
            )
            return ExecResult(
                stdout=stdout.decode("utf-8", errors="replace"),
                stderr=stderr.decode("utf-8", errors="replace"),
                exit_code=proc.returncode or 0,
            )
        except asyncio.TimeoutError:
            return ExecResult(stderr=f"Command timed out after {timeout}s", exit_code=124)
        except Exception as e:
            return ExecResult(stderr=str(e), exit_code=1)

    async def read_file(self, path: str) -> str:
        result = await self.execute(f"cat {_shell_quote(path)}")
        if result.exit_code != 0:
            raise FileNotFoundError(f"Cannot read {path}: {result.stderr}")
        return result.stdout

    async def write_file(self, path: str, content: str) -> None:
        # Ensure parent directory exists
        import os
        parent = os.path.dirname(path)
        if parent:
            await self.execute(f"mkdir -p {_shell_quote(parent)}")

        # Write via stdin pipe to handle arbitrary content safely
        proc = await asyncio.create_subprocess_exec(
            "docker", "exec", "-i", self._container_id,
            "bash", "-c", f"cat > {_shell_quote(path)}",
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        _, stderr = await proc.communicate(input=content.encode("utf-8"))
        if proc.returncode != 0:
            raise IOError(f"Cannot write {path}: {stderr.decode()}")

    async def list_files(self, pattern: str = "*") -> list[str]:
        result = await self.execute(f"find /home/user -name {_shell_quote(pattern)} -type f 2>/dev/null | head -1000")
        if result.exit_code != 0:
            return []
        return [line for line in result.stdout.strip().split("\n") if line]

    async def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        try:
            proc = await asyncio.create_subprocess_exec(
                "docker", "rm", "-f", self._container_id,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            await proc.communicate()
            logger.info("docker_sandbox_closed", container=self._container_name)
        except Exception as e:
            logger.warning("docker_sandbox_close_error", error=str(e))


class DockerSandboxProvider(SandboxProvider):
    """Creates Docker-based sandboxes."""

    def __init__(self, image: str = "python:3.12-slim"):
        self._image = image

    async def create(self, config: SandboxConfig) -> DockerSandbox:
        name = f"aurion-sandbox-{uuid.uuid4().hex[:12]}"

        cmd = [
            "docker", "run", "-d",
            "--name", name,
            "--workdir", config.working_dir,
            "--memory", "512m",
            "--cpus", "1.0",
            "--pids-limit", "256",
            "--read-only",
            "--tmpfs", "/tmp:rw,noexec,nosuid,size=256m",
            "--tmpfs", "/home/user:rw,size=512m",
        ]

        # Network policy
        if config.network_policy == "restricted":
            cmd.extend(["--network", "none"])

        cmd.extend([self._image, "sleep", "infinity"])

        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await proc.communicate()

        if proc.returncode != 0:
            raise RuntimeError(f"Failed to create Docker sandbox: {stderr.decode()}")

        container_id = stdout.decode().strip()

        sandbox = DockerSandbox(container_id, name)

        # Install packages if requested
        if config.packages:
            pkg_list = " ".join(config.packages)
            await sandbox.execute(
                f"apt-get update -qq && apt-get install -y -qq {pkg_list} 2>/dev/null",
                timeout=120,
            )

        # Mount files
        for path, content in config.mounted_files.items():
            await sandbox.write_file(path, content)

        logger.info(
            "docker_sandbox_created",
            name=name,
            container_id=container_id[:12],
            network=config.network_policy,
        )
        return sandbox


def _shell_quote(s: str) -> str:
    """Shell-escape a string to prevent injection."""
    return "'" + s.replace("'", "'\"'\"'") + "'"
