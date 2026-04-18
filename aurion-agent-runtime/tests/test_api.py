"""Tests for the FastAPI application - updated for full Anthropic API compatibility.

Auth is disabled (AUTH_ENABLED=false) for tests.
Uses real DB calls via Repository (init_db creates tables in-memory or test DB).
"""

import os
import pytest
from unittest.mock import AsyncMock, patch

# Disable auth for tests
os.environ["AUTH_ENABLED"] = "false"

from httpx import ASGITransport, AsyncClient
from apps.api.main import app


@pytest.fixture
async def client():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        yield ac


class TestHealth:
    @pytest.mark.asyncio
    async def test_health(self, client):
        resp = await client.get("/health")
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "ok"
        assert data["service"] == "aurion-agent-runtime"


class TestAgentsCRUD:
    @pytest.mark.asyncio
    async def test_create_agent(self, client):
        resp = await client.post("/v1/agents", json={
            "name": "test-agent",
            "model": {"id": "claude-sonnet-4-20250514", "speed": "standard"},
            "system": "You are helpful.",
        })
        assert resp.status_code == 200
        data = resp.json()
        assert data["name"] == "test-agent"
        assert "id" in data
        assert data["version"] == 1
        assert data["type"] == "agent"

    @pytest.mark.asyncio
    async def test_create_agent_defaults(self, client):
        resp = await client.post("/v1/agents", json={"name": "minimal"})
        assert resp.status_code == 200
        data = resp.json()
        assert data["model"]["id"] == "claude-sonnet-4-20250514"

    @pytest.mark.asyncio
    async def test_list_agents(self, client):
        await client.post("/v1/agents", json={"name": "a1"})
        await client.post("/v1/agents", json={"name": "a2"})
        resp = await client.get("/v1/agents")
        assert resp.status_code == 200
        assert len(resp.json()) >= 2

    @pytest.mark.asyncio
    async def test_get_agent(self, client):
        create_resp = await client.post("/v1/agents", json={"name": "a1"})
        agent_id = create_resp.json()["id"]
        resp = await client.get(f"/v1/agents/{agent_id}")
        assert resp.status_code == 200
        assert resp.json()["name"] == "a1"

    @pytest.mark.asyncio
    async def test_get_agent_not_found(self, client):
        resp = await client.get("/v1/agents/nonexistent")
        assert resp.status_code == 404

    @pytest.mark.asyncio
    async def test_update_agent_with_version(self, client):
        create_resp = await client.post("/v1/agents", json={"name": "a1"})
        agent_id = create_resp.json()["id"]
        # Update requires version for optimistic concurrency
        resp = await client.post(f"/v1/agents/{agent_id}", json={
            "version": 1,
            "name": "a1-updated",
        })
        assert resp.status_code == 200
        assert resp.json()["name"] == "a1-updated"
        assert resp.json()["version"] == 2

    @pytest.mark.asyncio
    async def test_update_agent_version_conflict(self, client):
        create_resp = await client.post("/v1/agents", json={"name": "a1"})
        agent_id = create_resp.json()["id"]
        resp = await client.post(f"/v1/agents/{agent_id}", json={
            "version": 99,
            "name": "conflict",
        })
        assert resp.status_code == 409

    @pytest.mark.asyncio
    async def test_archive_agent(self, client):
        create_resp = await client.post("/v1/agents", json={"name": "a1"})
        agent_id = create_resp.json()["id"]
        resp = await client.post(f"/v1/agents/{agent_id}/archive")
        assert resp.status_code == 200
        assert resp.json()["archived_at"] is not None

    @pytest.mark.asyncio
    async def test_agent_versions(self, client):
        create_resp = await client.post("/v1/agents", json={"name": "a1"})
        agent_id = create_resp.json()["id"]
        # Update to create version 2
        await client.post(f"/v1/agents/{agent_id}", json={"version": 1, "name": "a1-v2"})
        resp = await client.get(f"/v1/agents/{agent_id}/versions")
        assert resp.status_code == 200
        versions = resp.json()
        assert len(versions) >= 2

    @pytest.mark.asyncio
    async def test_delete_agent(self, client):
        create_resp = await client.post("/v1/agents", json={"name": "a1"})
        agent_id = create_resp.json()["id"]
        resp = await client.delete(f"/v1/agents/{agent_id}")
        assert resp.status_code == 200
        resp = await client.get(f"/v1/agents/{agent_id}")
        assert resp.status_code == 404


class TestEnvironmentsCRUD:
    @pytest.mark.asyncio
    async def test_create_environment(self, client):
        resp = await client.post("/v1/environments", json={"name": "python-dev"})
        assert resp.status_code == 200
        assert resp.json()["name"] == "python-dev"

    @pytest.mark.asyncio
    async def test_list_environments(self, client):
        await client.post("/v1/environments", json={"name": "e1"})
        await client.post("/v1/environments", json={"name": "e2"})
        resp = await client.get("/v1/environments")
        assert resp.status_code == 200
        assert len(resp.json()) >= 2


class TestSessionsCRUD:
    @pytest.mark.asyncio
    async def test_create_session(self, client):
        agent_resp = await client.post("/v1/agents", json={"name": "a1"})
        env_resp = await client.post("/v1/environments", json={"name": "e1"})
        resp = await client.post("/v1/sessions", json={
            "agent": agent_resp.json()["id"],
            "environment_id": env_resp.json()["id"],
        })
        assert resp.status_code == 200
        data = resp.json()
        assert "id" in data
        assert data["status"] == "idle"
        assert data["agent"] is not None  # Resolved agent snapshot

    @pytest.mark.asyncio
    async def test_create_session_with_title(self, client):
        agent_resp = await client.post("/v1/agents", json={"name": "a1"})
        env_resp = await client.post("/v1/environments", json={"name": "e1"})
        resp = await client.post("/v1/sessions", json={
            "agent": agent_resp.json()["id"],
            "environment_id": env_resp.json()["id"],
            "title": "My Test Session",
        })
        assert resp.status_code == 200
        assert resp.json()["title"] == "My Test Session"

    @pytest.mark.asyncio
    async def test_create_session_agent_not_found(self, client):
        env_resp = await client.post("/v1/environments", json={"name": "e1"})
        resp = await client.post("/v1/sessions", json={
            "agent": "nonexistent",
            "environment_id": env_resp.json()["id"],
        })
        assert resp.status_code == 404

    @pytest.mark.asyncio
    async def test_archive_session(self, client):
        agent_resp = await client.post("/v1/agents", json={"name": "a1"})
        env_resp = await client.post("/v1/environments", json={"name": "e1"})
        session_resp = await client.post("/v1/sessions", json={
            "agent": agent_resp.json()["id"],
            "environment_id": env_resp.json()["id"],
        })
        session_id = session_resp.json()["id"]
        resp = await client.post(f"/v1/sessions/{session_id}/archive")
        assert resp.status_code == 200
        assert resp.json()["archived_at"] is not None


class TestToolsEndpoint:
    @pytest.mark.asyncio
    async def test_list_tools(self, client):
        resp = await client.get("/v1/tools")
        assert resp.status_code == 200
        tools = resp.json()
        names = [t["name"] for t in tools]
        assert "bash" in names
        assert "read" in names
