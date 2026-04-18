"""Sandbox factory — creates the right backend based on configuration."""

from __future__ import annotations

import os

from packages.sandbox.base import Sandbox, SandboxConfig, SandboxProvider


def get_provider(provider_name: str | None = None) -> SandboxProvider:
    """Return the configured sandbox provider."""
    name = (provider_name or os.environ.get("SANDBOX_PROVIDER", "docker")).lower()

    if name == "e2b":
        from packages.sandbox.e2b_sandbox import E2BSandboxProvider
        return E2BSandboxProvider()
    elif name == "daytona":
        from packages.sandbox.daytona_sandbox import DaytonaSandboxProvider
        return DaytonaSandboxProvider()
    elif name == "docker":
        from packages.sandbox.docker_sandbox import DockerSandboxProvider
        return DockerSandboxProvider()
    else:
        raise ValueError(f"Unknown sandbox provider: {name}. Use: e2b, daytona, docker")


async def create_sandbox(
    config: SandboxConfig | None = None,
    provider_name: str | None = None,
) -> Sandbox:
    """Create a sandbox using the configured provider."""
    provider = get_provider(provider_name)
    return await provider.create(config or SandboxConfig())
