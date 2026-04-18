"""Daytona sandbox — Docker container isolation via Daytona API."""

from __future__ import annotations

import os

import httpx
import structlog

from packages.sandbox.base import ExecResult, Sandbox, SandboxConfig, SandboxProvider

logger = structlog.get_logger()


class DaytonaSandbox(Sandbox):
    """Sandbox backed by a Daytona workspace."""

    def __init__(self, workspace_id: str, api_url: str, api_key: str):
        self._workspace_id = workspace_id
        self._api_url = api_url.rstrip("/")
        self._api_key = api_key
        self._closed = False

    def _headers(self) -> dict[str, str]:
        return {
            "Authorization": f"Bearer {self._api_key}",
            "Content-Type": "application/json",
        }

    async def execute(self, command: str, timeout: int = 30) -> ExecResult:
        if self._closed:
            return ExecResult(stderr="Sandbox is closed", exit_code=1)
        try:
            async with httpx.AsyncClient(timeout=timeout + 5) as client:
                resp = await client.post(
                    f"{self._api_url}/workspace/{self._workspace_id}/toolbox/process/execute",
                    headers=self._headers(),
                    json={"command": command, "timeout": timeout},
                )
                resp.raise_for_status()
                data = resp.json()
                return ExecResult(
                    stdout=data.get("result", ""),
                    stderr=data.get("stderr", ""),
                    exit_code=data.get("exitCode", 0),
                )
        except httpx.TimeoutException:
            return ExecResult(stderr=f"Command timed out after {timeout}s", exit_code=124)
        except Exception as e:
            return ExecResult(stderr=str(e), exit_code=1)

    async def read_file(self, path: str) -> str:
        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.get(
                f"{self._api_url}/workspace/{self._workspace_id}/toolbox/files",
                headers=self._headers(),
                params={"path": path},
            )
            if resp.status_code != 200:
                raise FileNotFoundError(f"Cannot read {path}: {resp.text}")
            return resp.text

    async def write_file(self, path: str, content: str) -> None:
        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.post(
                f"{self._api_url}/workspace/{self._workspace_id}/toolbox/files",
                headers=self._headers(),
                json={"path": path, "content": content},
            )
            resp.raise_for_status()

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
            async with httpx.AsyncClient(timeout=15) as client:
                await client.delete(
                    f"{self._api_url}/workspace/{self._workspace_id}",
                    headers=self._headers(),
                )
            logger.info("daytona_sandbox_closed", workspace=self._workspace_id)
        except Exception as e:
            logger.warning("daytona_sandbox_close_error", error=str(e))


class DaytonaSandboxProvider(SandboxProvider):
    """Creates Daytona workspace sandboxes."""

    def __init__(
        self,
        api_url: str | None = None,
        api_key: str | None = None,
    ):
        self._api_url = (api_url or os.environ.get("DAYTONA_API_URL", "http://localhost:3986")).rstrip("/")
        self._api_key = api_key or os.environ.get("DAYTONA_API_KEY", "")

    async def create(self, config: SandboxConfig) -> DaytonaSandbox:
        async with httpx.AsyncClient(timeout=60) as client:
            resp = await client.post(
                f"{self._api_url}/workspace",
                headers={
                    "Authorization": f"Bearer {self._api_key}",
                    "Content-Type": "application/json",
                },
                json={
                    "name": f"aurion-sandbox",
                    "target": "local",
                    "projects": [{"name": "workspace", "image": "python:3.12-slim"}],
                },
            )
            resp.raise_for_status()
            data = resp.json()
            workspace_id = data.get("id", data.get("name", ""))

        sandbox = DaytonaSandbox(workspace_id, self._api_url, self._api_key)

        # Install packages
        if config.packages:
            pkg_list = " ".join(config.packages)
            await sandbox.execute(f"pip install {pkg_list}", timeout=120)

        # Mount files
        for path, content in config.mounted_files.items():
            await sandbox.write_file(path, content)

        logger.info("daytona_sandbox_created", workspace_id=workspace_id)
        return sandbox
