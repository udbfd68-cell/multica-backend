# Aurion Agent Runtime — Architecture Document

**Version**: 1.0.0  
**Date**: 2026-04-18  
**Status**: Production-grade implementation  

## 1. Overview

Aurion Agent Runtime is an open-source, self-hostable alternative to Claude Managed Agents that reproduces every feature of Anthropic's hosted agent service and adds capabilities Anthropic doesn't offer.

### Design Principles (from Anthropic's engineering blog)

1. **Brain/Hands/Session separation** — The harness (brain), sandbox (hands), and session log are decoupled interfaces. Each can fail or be replaced independently.
2. **Cattle, not pets** — Sessions are stateless harnesses. If one crashes, a new one reboots from the durable session log via `wake(session_id)`.
3. **`execute(name, input) → string`** — Universal tool interface. The harness doesn't know if it's talking to a container, MCP server, or custom tool.
4. **Session ≠ Context Window** — The session is an append-only event log that lives outside the LLM context. The harness selects slices via `getEvents()`.

### Advantages over Claude Managed Agents

| Feature | Claude Managed Agents | Aurion Agent Runtime |
|---|---|---|
| LLM Provider | Anthropic only | Anthropic, OpenAI, Gemini, Groq, Ollama |
| Session Duration | 24h max | Unlimited |
| Tool Execution | Sequential | Parallel + Sequential |
| Self-hosting | No | Docker, K8s, Railway, bare metal |
| Sandbox | Proprietary | E2B / Daytona / Docker (pluggable) |
| Agent Cloning | No | Yes, with version branching |
| Token Budget | No | Per-session configurable budget |
| Skills Sharing | No marketplace | Skills marketplace |
| Observability | Limited | Full Langfuse integration |
| Governance | None | Microsoft Agent Governance Toolkit |

## 2. System Architecture

```mermaid
graph TB
    subgraph "Client Layer"
        CLI[CLI / SDK]
        WEB[Dashboard UI]
        EXT[External Systems]
    end

    subgraph "API Gateway"
        API[FastAPI Server<br/>REST + SSE]
        AUTH[Auth Middleware]
        GOV[Governance Middleware]
    end

    subgraph "Core Runtime"
        ORCH[Session Orchestrator<br/>LangGraph StateGraph]
        EXEC[Tool Executor]
        MULTI[Multi-Agent<br/>Coordinator]
        SKILLS[Skills Loader]
        BUS[Event Bus<br/>Redis Pub/Sub + SSE]
    end

    subgraph "LLM Providers"
        ANT[Anthropic]
        OAI[OpenAI]
        GEM[Gemini]
        GROQ[Groq]
        OLL[Ollama]
    end

    subgraph "Sandbox Layer"
        E2B[E2B Sandbox]
        DAY[Daytona Sandbox]
        DOCK[Docker Sandbox]
    end

    subgraph "MCP Layer"
        MCPC[MCP Connector]
        MCPS[MCP Servers<br/>filesystem, github, etc.]
    end

    subgraph "Memory Layer"
        SLOG[Session Log<br/>PostgreSQL]
        GRAPH[Graphiti<br/>Knowledge Graph]
        FALK[FalkorDB]
    end

    subgraph "Infrastructure"
        PG[(PostgreSQL)]
        RED[(Redis)]
        LF[Langfuse]
    end

    CLI --> API
    WEB --> API
    EXT --> API
    API --> AUTH --> GOV --> ORCH
    ORCH --> EXEC
    ORCH --> MULTI
    ORCH --> SKILLS
    ORCH --> BUS
    ORCH --> ANT & OAI & GEM & GROQ & OLL
    EXEC --> E2B & DAY & DOCK
    EXEC --> MCPC --> MCPS
    ORCH --> SLOG
    ORCH --> GRAPH --> FALK
    BUS --> RED
    SLOG --> PG
    ORCH --> LF
```

## 3. Session Lifecycle

```mermaid
sequenceDiagram
    participant C as Client
    participant A as API
    participant O as Orchestrator
    participant L as LLM
    participant T as ToolExecutor
    participant S as Sandbox
    participant E as EventBus

    C->>A: POST /sessions (agent_id, prompt)
    A->>O: create_session()
    O->>E: emit(session.status_running)
    E-->>C: SSE: session.status_running

    loop ReAct Loop
        O->>L: messages + tools
        L-->>O: response (text + tool_use)
        O->>E: emit(agent.message)
        E-->>C: SSE: agent.message

        opt Tool Calls
            O->>T: execute(tool_name, input)
            O->>E: emit(agent.tool_use)
            E-->>C: SSE: agent.tool_use

            alt Built-in Tool
                T->>S: run in sandbox
                S-->>T: result
            else MCP Tool
                T->>T: route to MCP server
            else Custom Tool
                T->>E: emit(agent.custom_tool_use)
                E-->>C: SSE: agent.custom_tool_use
                C->>A: POST events (user.custom_tool_result)
                A->>O: deliver result
            end

            O->>E: emit(agent.tool_result)
            E-->>C: SSE: agent.tool_result
        end
    end

    O->>E: emit(session.status_idle)
    E-->>C: SSE: session.status_idle
```

## 4. Multi-Agent Flow

```mermaid
graph LR
    subgraph "Session"
        PT[Primary Thread<br/>Coordinator]
        T1[Thread 1<br/>Code Reviewer]
        T2[Thread 2<br/>Test Writer]
        T3[Thread 3<br/>Researcher]
    end

    subgraph "Shared Resources"
        FS[Shared Filesystem<br/>Sandbox]
        LOG[Session Event Log]
    end

    PT -->|delegate| T1
    PT -->|delegate| T2
    PT -->|delegate| T3
    T1 --> FS
    T2 --> FS
    T3 --> FS
    PT --> LOG
    T1 --> LOG
    T2 --> LOG
    T3 --> LOG
    T1 -->|thread_idle| PT
    T2 -->|thread_idle| PT
    T3 -->|thread_idle| PT
```

## 5. Data Model

```mermaid
erDiagram
    AGENT {
        uuid id PK
        string name
        int version
        string model_provider
        string model_id
        text system_prompt
        jsonb tools
        jsonb mcp_servers
        jsonb callable_agents
        jsonb skills
        jsonb metadata
        timestamp created_at
        timestamp updated_at
    }

    ENVIRONMENT {
        uuid id PK
        string name
        string sandbox_provider
        jsonb packages
        string network_policy
        jsonb mounted_files
        jsonb metadata
        timestamp created_at
    }

    SESSION {
        uuid id PK
        uuid agent_id FK
        uuid environment_id FK
        string status
        int token_budget
        int tokens_used
        jsonb config
        timestamp created_at
        timestamp updated_at
    }

    SESSION_EVENT {
        uuid id PK
        uuid session_id FK
        string thread_id
        string type
        jsonb payload
        int sequence_num
        timestamp created_at
    }

    SESSION_THREAD {
        uuid id PK
        uuid session_id FK
        uuid agent_id FK
        string status
        timestamp created_at
    }

    SKILL {
        uuid id PK
        string name
        string version
        text description
        jsonb instructions
        jsonb resources
        boolean is_builtin
        timestamp created_at
    }

    AGENT ||--o{ SESSION : "runs in"
    ENVIRONMENT ||--o{ SESSION : "provides sandbox"
    SESSION ||--o{ SESSION_EVENT : "produces"
    SESSION ||--o{ SESSION_THREAD : "contains"
    AGENT ||--o{ SKILL : "uses"
```

## 6. Component Interfaces

### 6.1 Tool Executor Interface

```python
class ToolExecutor(Protocol):
    async def execute(self, name: str, input: dict) -> str:
        """execute(name, input) → string — the universal tool interface"""
        ...

    async def list_tools(self) -> list[ToolDefinition]:
        """Return all available tool definitions"""
        ...
```

### 6.2 Sandbox Interface

```python
class SandboxProvider(Protocol):
    async def create(self, config: EnvironmentConfig) -> Sandbox:
        """Provision a new sandbox"""
        ...

class Sandbox(Protocol):
    async def execute(self, command: str, timeout: int = 30) -> ExecutionResult:
        """Run a command in the sandbox"""
        ...

    async def read_file(self, path: str) -> str: ...
    async def write_file(self, path: str, content: str) -> None: ...
    async def list_files(self, pattern: str) -> list[str]: ...
    async def close(self) -> None: ...
```

### 6.3 LLM Provider Interface

```python
class LLMProvider(Protocol):
    async def create_message(
        self,
        messages: list[Message],
        tools: list[ToolDefinition],
        system: str,
        max_tokens: int,
        stream: bool = True,
    ) -> AsyncIterator[MessageChunk]:
        ...
```

### 6.4 Memory Interface

```python
class MemoryProvider(Protocol):
    async def add(self, session_id: str, content: str, metadata: dict) -> None: ...
    async def search(self, query: str, group_id: str, limit: int = 10) -> list[Memory]: ...
    async def get_session_log(self, session_id: str, start: int = 0, end: int = -1) -> list[Event]: ...
```

## 7. Event Types

| Event Type | Direction | Description |
|---|---|---|
| `user.message` | Client → Agent | User sends a message |
| `user.tool_confirmation` | Client → Agent | Tool execution approval |
| `user.custom_tool_result` | Client → Agent | Custom tool execution result |
| `agent.message` | Agent → Client | Agent text response |
| `agent.tool_use` | Agent → Client | Agent invokes a tool |
| `agent.tool_result` | Agent → Client | Tool execution result |
| `agent.custom_tool_use` | Agent → Client | Agent requests custom tool execution |
| `session.status_running` | System | Session started processing |
| `session.status_idle` | System | Session finished, waiting |
| `session.error` | System | Session error |
| `session.thread_created` | System | New agent thread spawned |
| `session.thread_idle` | System | Agent thread finished |
| `agent.thread_message_sent` | System | Cross-thread message sent |
| `agent.thread_message_received` | System | Cross-thread message received |

## 8. API Endpoints

### Agents
- `POST /v1/agents` — Create agent
- `GET /v1/agents` — List agents
- `GET /v1/agents/:id` — Get agent
- `PATCH /v1/agents/:id` — Update agent
- `DELETE /v1/agents/:id` — Delete agent
- `POST /v1/agents/:id/clone` — Clone agent

### Environments
- `POST /v1/environments` — Create environment
- `GET /v1/environments` — List environments
- `GET /v1/environments/:id` — Get environment
- `DELETE /v1/environments/:id` — Delete environment

### Sessions
- `POST /v1/sessions` — Create and start session
- `GET /v1/sessions/:id` — Get session
- `GET /v1/sessions/:id/stream` — SSE event stream
- `POST /v1/sessions/:id/events` — Send events (user messages, tool results)
- `GET /v1/sessions/:id/events` — List session events
- `POST /v1/sessions/:id/interrupt` — Interrupt running session
- `DELETE /v1/sessions/:id` — Terminate session

### Session Threads (Multi-agent)
- `GET /v1/sessions/:id/threads` — List threads
- `GET /v1/sessions/:id/threads/:tid/stream` — Stream thread events
- `GET /v1/sessions/:id/threads/:tid/events` — List thread events

### Skills
- `POST /v1/skills` — Create skill
- `GET /v1/skills` — List skills
- `GET /v1/skills/:id` — Get skill
- `DELETE /v1/skills/:id` — Delete skill

### MCP
- `POST /v1/mcp/servers` — Register MCP server
- `GET /v1/mcp/servers` — List MCP servers
- `POST /v1/mcp/servers/:id/discover` — Discover tools

## 9. Deployment

```mermaid
graph TB
    subgraph "Docker Compose / Railway"
        API_SVC[aurion-api<br/>FastAPI + Uvicorn]
        WORKER[aurion-worker<br/>Session processor]
        PG_SVC[(PostgreSQL 16)]
        REDIS_SVC[(Redis 7)]
        FALKOR[(FalkorDB)]
        LF_SVC[Langfuse]
    end

    API_SVC --> PG_SVC
    API_SVC --> REDIS_SVC
    WORKER --> PG_SVC
    WORKER --> REDIS_SVC
    WORKER --> FALKOR
    WORKER --> LF_SVC
```

## 10. Security Model

1. **Sandbox isolation** — All agent code runs in isolated sandboxes (E2B microVM / Daytona container / Docker)
2. **Credential vault** — No credentials in sandbox. MCP OAuth tokens stored in encrypted vault, accessed via proxy.
3. **Token budgets** — Per-session token limits prevent runaway agents
4. **Tool permissions** — `always_allow`, `always_ask`, `always_deny` per tool
5. **Network policies** — Configurable restricted/unrestricted per environment
6. **Governance** — Microsoft Agent Governance Toolkit integration for OWASP Agentic AI risks
