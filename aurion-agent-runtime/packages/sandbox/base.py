"""Abstract sandbox interface.

Every sandbox backend (E2B, Daytona, Docker) implements this protocol.
The tool executor depends only on this interface, never on a concrete backend.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field


@dataclass
class ExecResult:
    """Result of executing a command in the sandbox."""

    stdout: str = ""
    stderr: str = ""
    exit_code: int = 0


@dataclass
class SandboxConfig:
    """Configuration for sandbox provisioning."""

    packages: list[str] = field(default_factory=list)
    network_policy: str = "restricted"  # restricted | unrestricted
    mounted_files: dict[str, str] = field(default_factory=dict)  # path → content
    timeout_seconds: int = 3600
    working_dir: str = "/home/user"


class Sandbox(ABC):
    """Abstract sandbox — every backend must implement these methods."""

    @abstractmethod
    async def execute(self, command: str, timeout: int = 30) -> ExecResult:
        """Execute a shell command and return stdout/stderr/exit_code."""
        ...

    @abstractmethod
    async def read_file(self, path: str) -> str:
        """Read a file from the sandbox filesystem."""
        ...

    @abstractmethod
    async def write_file(self, path: str, content: str) -> None:
        """Write content to a file in the sandbox."""
        ...

    @abstractmethod
    async def list_files(self, pattern: str = "*") -> list[str]:
        """List files matching a glob pattern."""
        ...

    @abstractmethod
    async def close(self) -> None:
        """Tear down the sandbox and release resources."""
        ...

    async def file_exists(self, path: str) -> bool:
        """Check if a file exists."""
        try:
            await self.read_file(path)
            return True
        except (FileNotFoundError, Exception):
            return False


class SandboxProvider(ABC):
    """Factory that creates sandbox instances."""

    @abstractmethod
    async def create(self, config: SandboxConfig) -> Sandbox:
        """Provision and return a new sandbox."""
        ...
