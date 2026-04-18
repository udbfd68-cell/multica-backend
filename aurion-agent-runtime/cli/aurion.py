"""Aurion CLI — command-line interface for the Aurion Agent Runtime.

Usage:
    aurion agents list
    aurion agents create --name "My Agent" --model claude-sonnet-4-20250514
    aurion agents get <agent_id>
    aurion agents update <agent_id> --version 1 --name "New Name"
    aurion agents archive <agent_id>
    aurion agents versions <agent_id>
    aurion agents delete <agent_id>

    aurion environments list
    aurion environments create --name "Default"
    aurion environments get <env_id>
    aurion environments delete <env_id>

    aurion sessions list [--agent <agent_id>]
    aurion sessions create --agent <agent_id> --environment <env_id>
    aurion sessions get <session_id>
    aurion sessions archive <session_id>
    aurion sessions delete <session_id>

    aurion events list <session_id> [--limit 100]
    aurion events send <session_id> --message "Hello"
    aurion events stream <session_id>

    aurion resources list <session_id>
    aurion resources add-file <session_id> --file-id <fid> [--mount-path /path]
    aurion resources add-github <session_id> --url <repo_url> [--branch main]
    aurion resources delete <session_id> <resource_id>

    aurion keys create --name "my-key"
    aurion keys list
    aurion keys revoke <key_id>
"""

from __future__ import annotations

import json
import os
import sys
from typing import Any

import httpx
import click

DEFAULT_BASE_URL = os.environ.get("AURION_BASE_URL", "http://localhost:8000")
API_KEY = os.environ.get("AURION_API_KEY", "")


def get_client() -> httpx.Client:
    headers = {}
    if API_KEY:
        headers["x-api-key"] = API_KEY
    return httpx.Client(base_url=DEFAULT_BASE_URL, headers=headers, timeout=30)


def pp(data: Any) -> None:
    """Pretty-print JSON data."""
    click.echo(json.dumps(data, indent=2, default=str))


def handle_response(resp: httpx.Response) -> Any:
    if resp.status_code >= 400:
        click.echo(f"Error {resp.status_code}: {resp.text}", err=True)
        sys.exit(1)
    return resp.json()


# ══════════════════════════════════════════════════════════════════════════
# Root CLI
# ══════════════════════════════════════════════════════════════════════════

@click.group()
@click.option("--base-url", envvar="AURION_BASE_URL", default=DEFAULT_BASE_URL)
@click.option("--api-key", envvar="AURION_API_KEY", default=API_KEY)
@click.pass_context
def cli(ctx: click.Context, base_url: str, api_key: str):
    """Aurion Agent Runtime CLI."""
    ctx.ensure_object(dict)
    ctx.obj["base_url"] = base_url
    ctx.obj["api_key"] = api_key


# ══════════════════════════════════════════════════════════════════════════
# AGENTS
# ══════════════════════════════════════════════════════════════════════════

@cli.group()
def agents():
    """Manage agents."""
    pass


@agents.command("list")
def agents_list():
    with get_client() as c:
        pp(handle_response(c.get("/v1/agents")))


@agents.command("create")
@click.option("--name", required=True)
@click.option("--model", default="claude-sonnet-4-20250514")
@click.option("--system", default=None)
@click.option("--description", default=None)
def agents_create(name: str, model: str, system: str | None, description: str | None):
    body: dict[str, Any] = {
        "name": name,
        "model": {"id": model, "speed": "standard"},
    }
    if system:
        body["system"] = system
    if description:
        body["description"] = description
    with get_client() as c:
        pp(handle_response(c.post("/v1/agents", json=body)))


@agents.command("get")
@click.argument("agent_id")
def agents_get(agent_id: str):
    with get_client() as c:
        pp(handle_response(c.get(f"/v1/agents/{agent_id}")))


@agents.command("update")
@click.argument("agent_id")
@click.option("--version", type=int, required=True)
@click.option("--name", default=None)
@click.option("--model", default=None)
@click.option("--system", default=None)
@click.option("--description", default=None)
def agents_update(agent_id: str, version: int, name: str | None, model: str | None, system: str | None, description: str | None):
    body: dict[str, Any] = {"version": version}
    if name:
        body["name"] = name
    if model:
        body["model"] = {"id": model, "speed": "standard"}
    if system:
        body["system"] = system
    if description:
        body["description"] = description
    with get_client() as c:
        pp(handle_response(c.post(f"/v1/agents/{agent_id}", json=body)))


@agents.command("archive")
@click.argument("agent_id")
def agents_archive(agent_id: str):
    with get_client() as c:
        pp(handle_response(c.post(f"/v1/agents/{agent_id}/archive")))


@agents.command("versions")
@click.argument("agent_id")
def agents_versions(agent_id: str):
    with get_client() as c:
        pp(handle_response(c.get(f"/v1/agents/{agent_id}/versions")))


@agents.command("delete")
@click.argument("agent_id")
@click.confirmation_option(prompt="Are you sure you want to delete this agent?")
def agents_delete(agent_id: str):
    with get_client() as c:
        pp(handle_response(c.delete(f"/v1/agents/{agent_id}")))


# ══════════════════════════════════════════════════════════════════════════
# ENVIRONMENTS
# ══════════════════════════════════════════════════════════════════════════

@cli.group()
def environments():
    """Manage environments."""
    pass


@environments.command("list")
def envs_list():
    with get_client() as c:
        pp(handle_response(c.get("/v1/environments")))


@environments.command("create")
@click.option("--name", required=True)
@click.option("--sandbox", default="docker", type=click.Choice(["docker", "e2b", "daytona"]))
def envs_create(name: str, sandbox: str):
    with get_client() as c:
        pp(handle_response(c.post("/v1/environments", json={
            "name": name,
            "sandbox_provider": sandbox,
        })))


@environments.command("get")
@click.argument("env_id")
def envs_get(env_id: str):
    with get_client() as c:
        pp(handle_response(c.get(f"/v1/environments/{env_id}")))


@environments.command("delete")
@click.argument("env_id")
@click.confirmation_option(prompt="Are you sure?")
def envs_delete(env_id: str):
    with get_client() as c:
        pp(handle_response(c.delete(f"/v1/environments/{env_id}")))


# ══════════════════════════════════════════════════════════════════════════
# SESSIONS
# ══════════════════════════════════════════════════════════════════════════

@cli.group()
def sessions():
    """Manage sessions."""
    pass


@sessions.command("list")
@click.option("--agent", default=None, help="Filter by agent ID")
def sessions_list(agent: str | None):
    with get_client() as c:
        params = {}
        if agent:
            params["agent_id"] = agent
        pp(handle_response(c.get("/v1/sessions", params=params)))


@sessions.command("create")
@click.option("--agent", required=True, help="Agent ID")
@click.option("--environment", required=True, help="Environment ID")
@click.option("--title", default=None)
def sessions_create(agent: str, environment: str, title: str | None):
    body: dict[str, Any] = {
        "agent": agent,
        "environment_id": environment,
    }
    if title:
        body["title"] = title
    with get_client() as c:
        pp(handle_response(c.post("/v1/sessions", json=body)))


@sessions.command("get")
@click.argument("session_id")
def sessions_get(session_id: str):
    with get_client() as c:
        pp(handle_response(c.get(f"/v1/sessions/{session_id}")))


@sessions.command("archive")
@click.argument("session_id")
def sessions_archive(session_id: str):
    with get_client() as c:
        pp(handle_response(c.post(f"/v1/sessions/{session_id}/archive")))


@sessions.command("delete")
@click.argument("session_id")
@click.confirmation_option(prompt="Are you sure?")
def sessions_delete(session_id: str):
    with get_client() as c:
        pp(handle_response(c.delete(f"/v1/sessions/{session_id}")))


# ══════════════════════════════════════════════════════════════════════════
# EVENTS
# ══════════════════════════════════════════════════════════════════════════

@cli.group()
def events():
    """Manage session events."""
    pass


@events.command("list")
@click.argument("session_id")
@click.option("--limit", default=100, type=int)
@click.option("--after", default=None, help="Cursor: event ID to start after")
def events_list(session_id: str, limit: int, after: str | None):
    with get_client() as c:
        params: dict[str, Any] = {"limit": limit}
        if after:
            params["after_id"] = after
        pp(handle_response(c.get(f"/v1/sessions/{session_id}/events", params=params)))


@events.command("send")
@click.argument("session_id")
@click.option("--message", required=True, help="Message text to send")
def events_send(session_id: str, message: str):
    with get_client() as c:
        pp(handle_response(c.post(f"/v1/sessions/{session_id}/events", json={
            "events": [
                {"type": "user.message", "content": [{"type": "text", "text": message}]}
            ]
        })))


@events.command("interrupt")
@click.argument("session_id")
def events_interrupt(session_id: str):
    with get_client() as c:
        pp(handle_response(c.post(f"/v1/sessions/{session_id}/events", json={
            "events": [{"type": "user.interrupt"}]
        })))


@events.command("stream")
@click.argument("session_id")
def events_stream(session_id: str):
    """Stream events via SSE (blocking)."""
    headers = {}
    if API_KEY:
        headers["x-api-key"] = API_KEY
    url = f"{DEFAULT_BASE_URL}/v1/sessions/{session_id}/events/stream"
    click.echo(f"Streaming events from {url}...")
    with httpx.stream("GET", url, headers=headers, timeout=None) as response:
        for line in response.iter_lines():
            if line.startswith("data: "):
                try:
                    data = json.loads(line[6:])
                    event_type = data.get("type", "unknown")
                    payload = data.get("payload", {})
                    if event_type == "agent.message":
                        click.echo(f"\n[Agent]: {payload.get('content', '')}")
                    elif event_type == "agent.tool_use":
                        click.echo(f"\n[Tool]: {payload.get('name', '')} — {json.dumps(payload.get('input', {}))[:200]}")
                    elif event_type == "agent.tool_result":
                        output = payload.get("output", "")[:200]
                        click.echo(f"[Result]: {output}")
                    elif event_type == "session.error":
                        click.echo(f"\n[ERROR]: {payload.get('type', '')} — {payload.get('message', '')}")
                    elif event_type in ("session.status_idle", "session.status_running"):
                        click.echo(f"\n[Status]: {event_type}")
                    else:
                        click.echo(f"\n[{event_type}]: {json.dumps(payload)[:200]}")
                except json.JSONDecodeError:
                    click.echo(line)


# ══════════════════════════════════════════════════════════════════════════
# RESOURCES
# ══════════════════════════════════════════════════════════════════════════

@cli.group()
def resources():
    """Manage session resources."""
    pass


@resources.command("list")
@click.argument("session_id")
def resources_list(session_id: str):
    with get_client() as c:
        pp(handle_response(c.get(f"/v1/sessions/{session_id}/resources")))


@resources.command("add-file")
@click.argument("session_id")
@click.option("--file-id", required=True)
@click.option("--mount-path", default=None)
def resources_add_file(session_id: str, file_id: str, mount_path: str | None):
    body: dict[str, Any] = {"type": "file", "file_id": file_id}
    if mount_path:
        body["mount_path"] = mount_path
    with get_client() as c:
        pp(handle_response(c.post(f"/v1/sessions/{session_id}/resources", json=body)))


@resources.command("add-github")
@click.argument("session_id")
@click.option("--url", required=True)
@click.option("--branch", default=None)
@click.option("--mount-path", default=None)
def resources_add_github(session_id: str, url: str, branch: str | None, mount_path: str | None):
    body: dict[str, Any] = {"type": "github_repository", "url": url}
    if branch:
        body["checkout"] = {"type": "branch", "name": branch}
    if mount_path:
        body["mount_path"] = mount_path
    with get_client() as c:
        pp(handle_response(c.post(f"/v1/sessions/{session_id}/resources", json=body)))


@resources.command("delete")
@click.argument("session_id")
@click.argument("resource_id")
@click.confirmation_option(prompt="Are you sure?")
def resources_delete(session_id: str, resource_id: str):
    with get_client() as c:
        pp(handle_response(c.delete(f"/v1/sessions/{session_id}/resources/{resource_id}")))


# ══════════════════════════════════════════════════════════════════════════
# API KEYS
# ══════════════════════════════════════════════════════════════════════════

@cli.group()
def keys():
    """Manage API keys."""
    pass


@keys.command("create")
@click.option("--name", required=True)
def keys_create(name: str):
    with get_client() as c:
        result = handle_response(c.post("/v1/api-keys", json={"name": name}))
        click.echo(f"\nAPI Key created!")
        click.echo(f"  ID:     {result['id']}")
        click.echo(f"  Key:    {result['key']}")
        click.echo(f"  Prefix: {result['prefix']}")
        click.echo(f"\n  Save this key — it will not be shown again!")


@keys.command("list")
def keys_list():
    with get_client() as c:
        pp(handle_response(c.get("/v1/api-keys")))


@keys.command("revoke")
@click.argument("key_id")
@click.confirmation_option(prompt="Are you sure?")
def keys_revoke(key_id: str):
    with get_client() as c:
        pp(handle_response(c.delete(f"/v1/api-keys/{key_id}")))


# ══════════════════════════════════════════════════════════════════════════
# HEALTH
# ══════════════════════════════════════════════════════════════════════════

@cli.command()
def health():
    """Check runtime health."""
    with get_client() as c:
        pp(handle_response(c.get("/health")))


if __name__ == "__main__":
    cli()
