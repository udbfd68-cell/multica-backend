# Aurion Agent Runtime

**Open-source Managed Agents runtime** compatible with [Anthropic's Managed Agents API](https://docs.anthropic.com/en/docs/agents/managed-agents-overview) (``managed-agents-2026-04-01`` beta).

Production-grade. PostgreSQL persistence. API key auth. Agent versioning. Resources API. Full event system. CLI.

---

## What's Implemented

| Anthropic Feature | Status | Notes |
|---|---|---|
| Agents CRUD + versioning | Done | Version auto-increments, optimistic concurrency |
| Agent archive | Done | ``POST /v1/agents/{id}/archive`` |
| Agent version history | Done | ``GET /v1/agents/{id}/versions`` |
| Environments CRUD | Done | Cloud + networking config |
| Sessions CRUD + archive | Done | Resolved agent snapshot, stats, usage tracking |
| Events send (4 types) | Done | user.message, user.interrupt, user.tool_confirmation, user.custom_tool_result |
| Events stream (SSE) | Done | ``GET /v1/sessions/{id}/events/stream`` |
| Events list (pagination) | Done | Cursor-based ``after_id`` pagination |
| Resources API | Done | Files + GitHub repositories |
| 28 event types | Done | All from Anthropic API |
| Retry/error system | Done | retrying/exhausted/terminal with exponential backoff |
| user.interrupt | Done | Cancels current LLM call |
| Observability spans | Done | span.model_request_start/end |
| Context compaction | Done | agent.thread_context_compacted |
| API key auth | Done | SHA-256 hashed keys + bootstrap key |
| PostgreSQL persistence | Done | SQLAlchemy async + asyncpg |
| Alembic migrations | Done | Initial schema migration |
| CLI tool | Done | Full CRUD for all resources |
| Multi-LLM | Done | Anthropic, OpenAI, Gemini, Groq, Ollama |
| Sandboxed execution | Done | Docker, E2B, Daytona |
| 8 built-in tools | Done | bash, read, write, edit, glob, grep, web_fetch, web_search |
| MCP integration | Done | stdio, SSE, streamable-HTTP |
| Multi-agent | Done | Coordinator + sub-agent delegation |
| Long-term memory | Done | Graphiti knowledge graph |
| Skills system | Done | YAML composable modules |
| Langfuse tracing | Done | Self-hosted or cloud |

## Quick Start

### Docker Compose (recommended)

```bash
git clone https://github.com/aurion/agent-runtime.git
cd agent-runtime
cp .env.example .env
# Edit .env with your API keys and set AURION_BOOTSTRAP_KEY

docker compose up -d
```

API at ``http://localhost:8000``.

### Local Development

```bash
pip install -e ".[dev]"

# Start PostgreSQL + Redis
docker run -d -p 5432:5432 -e POSTGRES_USER=aurion -e POSTGRES_PASSWORD=aurion -e POSTGRES_DB=aurion postgres:16-alpine
docker run -d -p 6379:6379 redis:7-alpine

# Run migrations
alembic upgrade head

# Start server
uvicorn apps.api.main:app --reload --port 8000
```

### Railway Deploy

[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/template)

## API Usage

### Create an API Key (first time)

```bash
# Using the bootstrap key
curl -X POST http://localhost:8000/v1/api-keys \
  -H "x-api-key: your-bootstrap-key" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-key"}'
```

### Create an Agent

```bash
curl -X POST http://localhost:8000/v1/agents \
  -H "x-api-key: aurion_..." \
  -H "Content-Type: application/json" \
  -d '{
    "name": "coder",
    "model": {"id": "claude-sonnet-4-20250514", "speed": "standard"},
    "system": "You are an expert software engineer.",
    "tools": [{"type": "agent_toolset_20260401"}]
  }'
```

### Update an Agent (versioned)

```bash
curl -X POST http://localhost:8000/v1/agents/<agent-id> \
  -H "x-api-key: aurion_..." \
  -H "Content-Type: application/json" \
  -d '{"version": 1, "name": "coder-v2", "system": "Updated system prompt."}'
```

### Create a Session

```bash
curl -X POST http://localhost:8000/v1/sessions \
  -H "x-api-key: aurion_..." \
  -H "Content-Type: application/json" \
  -d '{"agent": "<agent-id>", "environment_id": "<env-id>", "title": "My session"}'
```

### Send a Message

```bash
curl -X POST http://localhost:8000/v1/sessions/<session-id>/events \
  -H "x-api-key: aurion_..." \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {"type": "user.message", "content": [{"type": "text", "text": "Build a web scraper for HN"}]}
    ]
  }'
```

### Interrupt a Session

```bash
curl -X POST http://localhost:8000/v1/sessions/<session-id>/events \
  -H "x-api-key: aurion_..." \
  -H "Content-Type: application/json" \
  -d '{"events": [{"type": "user.interrupt"}]}'
```

### Stream Events (SSE)

```bash
curl -N http://localhost:8000/v1/sessions/<session-id>/events/stream \
  -H "x-api-key: aurion_..."
```

### Add a GitHub Repository Resource

```bash
curl -X POST http://localhost:8000/v1/sessions/<session-id>/resources \
  -H "x-api-key: aurion_..." \
  -H "Content-Type: application/json" \
  -d '{"type": "github_repository", "url": "https://github.com/user/repo", "checkout": {"type": "branch", "name": "main"}}'
```

## CLI

```bash
# Install
pip install -e .

# Set API key
export AURION_API_KEY=aurion_...

# Agent operations
aurion agents create --name "coder" --model claude-sonnet-4-20250514
aurion agents list
aurion agents update <id> --version 1 --name "coder-v2"
aurion agents archive <id>
aurion agents versions <id>

# Session operations
aurion sessions create --agent <id> --environment <id> --title "Test"
aurion events send <session-id> --message "Hello"
aurion events stream <session-id>
aurion events interrupt <session-id>

# Resources
aurion resources add-github <session-id> --url https://github.com/user/repo --branch main
aurion resources list <session-id>

# API keys
aurion keys create --name "dev-key"
aurion keys list
aurion keys revoke <key-id>
```

## Event Types (28 total)

| Event | Direction | Description |
|---|---|---|
| ``user.message`` | Client->Server | User sends a message |
| ``user.interrupt`` | Client->Server | Cancel current operation |
| ``user.tool_confirmation`` | Client->Server | Allow/deny tool use |
| ``user.custom_tool_result`` | Client->Server | Custom tool result |
| ``agent.message`` | Server->Client | Agent text response |
| ``agent.thinking`` | Server->Client | Extended thinking output |
| ``agent.tool_use`` | Server->Client | Agent wants to use a built-in tool |
| ``agent.tool_result`` | Server->Client | Built-in tool result |
| ``agent.mcp_tool_use`` | Server->Client | Agent uses an MCP tool |
| ``agent.mcp_tool_result`` | Server->Client | MCP tool result |
| ``agent.custom_tool_use`` | Server->Client | Agent uses a custom tool |
| ``agent.thread_context_compacted`` | Server->Client | Context window compacted |
| ``session.status_running`` | Server->Client | Session is processing |
| ``session.status_idle`` | Server->Client | Session stopped (with stop_reason) |
| ``session.status_rescheduled`` | Server->Client | Retrying after error |
| ``session.status_terminated`` | Server->Client | Session terminated |
| ``session.error`` | Server->Client | Error with retry_status |
| ``session.deleted`` | Server->Client | Session deleted |
| ``session.thread_created`` | Server->Client | Sub-agent thread created |
| ``session.thread_idle`` | Server->Client | Sub-agent thread finished |
| ``agent.thread_message_sent`` | Server->Client | Message sent to sub-agent |
| ``agent.thread_message_received`` | Server->Client | Response from sub-agent |
| ``span.model_request_start`` | Server->Client | LLM request started |
| ``span.model_request_end`` | Server->Client | LLM request completed |

## Error Types + Retry

Errors follow Anthropic's model with three retry states:

| Error Type | Retryable | Description |
|---|---|---|
| ``billing_error`` | terminal | Billing/payment issue |
| ``model_overloaded`` | retrying | Temporary overload (529) |
| ``model_rate_limited`` | retrying | Rate limit hit |
| ``model_request_failed`` | retrying | Generic model error |
| ``mcp_authentication_failed`` | terminal | MCP auth error |
| ``mcp_connection_failed`` | retrying | MCP connection error |
| ``unknown`` | terminal | Unknown error |

Retry uses exponential backoff: 2s, 4s, 8s (3 attempts max).

## Project Structure

```
aurion-agent-runtime/
+-- apps/api/main.py             # FastAPI application (all endpoints)
+-- packages/core/
|   +-- models.py                # Pydantic models (28 event types, resources, versioning)
|   +-- database.py              # PostgreSQL persistence (SQLAlchemy async)
|   +-- auth.py                  # API key authentication middleware
|   +-- session.py               # Session orchestrator (ReAct + retry + interrupt)
|   +-- event_bus.py             # Redis pub/sub + SSE
|   +-- llm_providers.py         # Multi-LLM adapter
|   +-- tool_executor.py         # Built-in tool execution
|   +-- multi_agent.py           # Multi-agent coordination
+-- packages/sandbox/            # Docker, E2B, Daytona sandboxes
+-- packages/mcp/                # MCP server connector
+-- packages/memory/             # Graphiti long-term memory
+-- packages/skills/             # YAML skills loader
+-- packages/observability/      # Langfuse tracing
+-- cli/aurion.py                # CLI tool (click-based)
+-- migrations/                  # Alembic PostgreSQL migrations
+-- tests/                       # pytest suite
+-- docker-compose.yml
+-- Dockerfile
+-- pyproject.toml
```

## Testing

```bash
pip install -e ".[dev]"
pytest -v
```

## License

MIT
