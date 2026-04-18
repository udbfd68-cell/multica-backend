"""Pydantic models for the Aurion Agent Runtime.

100% aligned with the Anthropic Managed Agents API (managed-agents-2026-04-01).
Every domain object is defined here - agents, environments, sessions, events,
tools, resources, threads. These models are used across the API, the orchestrator,
and the PostgreSQL persistence layer.
"""

from __future__ import annotations

import uuid
from datetime import datetime
from enum import StrEnum
from typing import Any, Literal

from pydantic import BaseModel, Field


# ==============================================================================
# Enums - matching Anthropic's exact string values
# ==============================================================================


class SessionStatus(StrEnum):
    IDLE = "idle"
    RUNNING = "running"
    RESCHEDULING = "rescheduling"
    TERMINATED = "terminated"
    ERROR = "error"


class EventType(StrEnum):
    """All event types from Anthropic Managed Agents (April 2026)."""

    # -- Client -> Server --
    USER_MESSAGE = "user.message"
    USER_INTERRUPT = "user.interrupt"
    USER_TOOL_CONFIRMATION = "user.tool_confirmation"
    USER_CUSTOM_TOOL_RESULT = "user.custom_tool_result"
    USER_DEFINE_OUTCOME = "user.define_outcome"

    # -- Agent -> Client --
    AGENT_MESSAGE = "agent.message"
    AGENT_THINKING = "agent.thinking"
    AGENT_TOOL_USE = "agent.tool_use"
    AGENT_TOOL_RESULT = "agent.tool_result"
    AGENT_MCP_TOOL_USE = "agent.mcp_tool_use"
    AGENT_MCP_TOOL_RESULT = "agent.mcp_tool_result"
    AGENT_CUSTOM_TOOL_USE = "agent.custom_tool_use"
    AGENT_THREAD_CONTEXT_COMPACTED = "agent.thread_context_compacted"

    # -- Session Lifecycle --
    SESSION_STATUS_RUNNING = "session.status_running"
    SESSION_STATUS_IDLE = "session.status_idle"
    SESSION_STATUS_RESCHEDULED = "session.status_rescheduled"
    SESSION_STATUS_TERMINATED = "session.status_terminated"
    SESSION_ERROR = "session.error"
    SESSION_DELETED = "session.deleted"

    # -- Multi-agent --
    SESSION_THREAD_CREATED = "session.thread_created"
    SESSION_THREAD_IDLE = "session.thread_idle"
    AGENT_THREAD_MESSAGE_SENT = "agent.thread_message_sent"
    AGENT_THREAD_MESSAGE_RECEIVED = "agent.thread_message_received"

    # -- Observability spans --
    SPAN_MODEL_REQUEST_START = "span.model_request_start"
    SPAN_MODEL_REQUEST_END = "span.model_request_end"


class RetryStatus(StrEnum):
    RETRYING = "retrying"
    EXHAUSTED = "exhausted"
    TERMINAL = "terminal"


class StopReason(StrEnum):
    END_TURN = "end_turn"
    REQUIRES_ACTION = "requires_action"
    RETRIES_EXHAUSTED = "retries_exhausted"


class ToolPermission(StrEnum):
    ALWAYS_ALLOW = "always_allow"
    ALWAYS_ASK = "always_ask"
    ALWAYS_DENY = "always_deny"


class SandboxProviderType(StrEnum):
    E2B = "e2b"
    DAYTONA = "daytona"
    DOCKER = "docker"


class LLMProviderType(StrEnum):
    ANTHROPIC = "anthropic"
    OPENAI = "openai"
    GEMINI = "gemini"
    GROQ = "groq"
    OLLAMA = "ollama"


class ResourceType(StrEnum):
    FILE = "file"
    GITHUB_REPOSITORY = "github_repository"


# ==============================================================================
# Tool Definitions
# ==============================================================================


class ToolPermissionPolicy(BaseModel):
    type: ToolPermission = ToolPermission.ALWAYS_ALLOW


class AgentToolset(BaseModel):
    type: str = "agent_toolset_20260401"
    default_config: dict[str, Any] = Field(default_factory=lambda: {
        "permission_policy": {"type": "always_allow"}
    })


class CustomToolDefinition(BaseModel):
    type: str = "custom"
    name: str
    description: str
    input_schema: dict[str, Any] = Field(default_factory=lambda: {
        "type": "object", "properties": {}, "required": []
    })


class ToolDefinition(BaseModel):
    name: str
    description: str = ""
    input_schema: dict[str, Any] = Field(default_factory=lambda: {
        "type": "object", "properties": {}, "required": []
    })
    type: str = "builtin"
    permission: ToolPermission = ToolPermission.ALWAYS_ALLOW
    enabled: bool = True


class McpServerConfig(BaseModel):
    name: str
    transport: str = "stdio"
    command: str | None = None
    args: list[str] = Field(default_factory=list)
    url: str | None = None
    env: dict[str, str] = Field(default_factory=dict)
    headers: dict[str, str] = Field(default_factory=dict)
    auth_type: str | None = None
    auth_config: dict[str, str] = Field(default_factory=dict)


class CallableAgent(BaseModel):
    name: str
    agent_id: str
    description: str | None = None
    version: int | None = None


class SkillRef(BaseModel):
    skill_id: str
    version: str | None = None


# ==============================================================================
# Agent - versioned, archivable (matching Anthropic's exact shape)
# ==============================================================================


class ModelConfig(BaseModel):
    id: str = "claude-sonnet-4-20250514"
    speed: str = "standard"


class AgentCreate(BaseModel):
    name: str
    model: ModelConfig = Field(default_factory=ModelConfig)
    system: str | None = None
    description: str | None = None
    tools: list[AgentToolset | CustomToolDefinition | dict[str, Any]] = Field(default_factory=list)
    mcp_servers: list[McpServerConfig] = Field(default_factory=list)
    skills: list[SkillRef] = Field(default_factory=list)
    callable_agents: list[CallableAgent] = Field(default_factory=list)
    metadata: dict[str, str] = Field(default_factory=dict)


class AgentUpdate(BaseModel):
    version: int
    name: str | None = None
    model: ModelConfig | None = None
    system: str | None = None
    description: str | None = None
    tools: list[AgentToolset | CustomToolDefinition | dict[str, Any]] | None = None
    mcp_servers: list[McpServerConfig] | None = None
    skills: list[SkillRef] | None = None
    callable_agents: list[CallableAgent] | None = None
    metadata: dict[str, str] | None = None


class Agent(BaseModel):
    id: str = Field(default_factory=lambda: f"agent_{uuid.uuid4().hex[:24]}")
    type: str = "agent"
    version: int = 1
    name: str
    model: ModelConfig = Field(default_factory=ModelConfig)
    system: str | None = None
    description: str | None = None
    tools: list[AgentToolset | CustomToolDefinition | dict[str, Any]] = Field(default_factory=list)
    mcp_servers: list[McpServerConfig] = Field(default_factory=list)
    skills: list[SkillRef] = Field(default_factory=list)
    callable_agents: list[CallableAgent] = Field(default_factory=list)
    metadata: dict[str, str] = Field(default_factory=dict)
    created_at: datetime = Field(default_factory=datetime.utcnow)
    updated_at: datetime = Field(default_factory=datetime.utcnow)
    archived_at: datetime | None = None


class AgentVersion(BaseModel):
    id: str
    agent_id: str
    version: int
    snapshot: dict[str, Any]
    created_at: datetime = Field(default_factory=datetime.utcnow)


# ==============================================================================
# Environment - container config
# ==============================================================================


class NetworkingConfig(BaseModel):
    type: str = "unrestricted"


class EnvironmentConfig(BaseModel):
    type: str = "cloud"
    networking: NetworkingConfig = Field(default_factory=NetworkingConfig)


class EnvironmentCreate(BaseModel):
    name: str
    config: EnvironmentConfig = Field(default_factory=EnvironmentConfig)
    sandbox_provider: SandboxProviderType = SandboxProviderType.DOCKER
    packages: list[str] = Field(default_factory=list)
    metadata: dict[str, str] = Field(default_factory=dict)


class Environment(BaseModel):
    id: str = Field(default_factory=lambda: f"env_{uuid.uuid4().hex[:24]}")
    type: str = "environment"
    name: str
    config: EnvironmentConfig = Field(default_factory=EnvironmentConfig)
    sandbox_provider: SandboxProviderType = SandboxProviderType.DOCKER
    packages: list[str] = Field(default_factory=list)
    metadata: dict[str, str] = Field(default_factory=dict)
    created_at: datetime = Field(default_factory=datetime.utcnow)
    updated_at: datetime = Field(default_factory=datetime.utcnow)
    archived_at: datetime | None = None


# ==============================================================================
# Session - stateful running instance
# ==============================================================================


class SessionStats(BaseModel):
    active_seconds: float = 0.0
    duration_seconds: float = 0.0


class SessionUsage(BaseModel):
    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_input_tokens: int = 0
    cache_creation: dict[str, int] = Field(default_factory=dict)


class SessionCreate(BaseModel):
    agent: str | dict[str, Any]
    environment_id: str
    title: str | None = None
    vault_ids: list[str] = Field(default_factory=list)
    metadata: dict[str, str] = Field(default_factory=dict)


class SessionUpdate(BaseModel):
    title: str | None = None
    metadata: dict[str, str] | None = None


class Session(BaseModel):
    id: str = Field(default_factory=lambda: f"session_{uuid.uuid4().hex[:24]}")
    type: str = "session"
    agent: Agent | None = None
    agent_id: str = ""
    environment_id: str = ""
    title: str | None = None
    status: SessionStatus = SessionStatus.IDLE
    stop_reason: StopReason | None = None
    vault_ids: list[str] = Field(default_factory=list)
    stats: SessionStats = Field(default_factory=SessionStats)
    usage: SessionUsage = Field(default_factory=SessionUsage)
    metadata: dict[str, str] = Field(default_factory=dict)
    created_at: datetime = Field(default_factory=datetime.utcnow)
    updated_at: datetime = Field(default_factory=datetime.utcnow)
    archived_at: datetime | None = None


class DeletedSession(BaseModel):
    id: str
    type: str = "session.deleted"


# ==============================================================================
# Resources - files + GitHub repos mounted into sessions
# ==============================================================================


class GitHubCheckout(BaseModel):
    type: str = "branch"
    name: str | None = None
    sha: str | None = None


class FileResourceCreate(BaseModel):
    type: Literal["file"] = "file"
    file_id: str
    mount_path: str | None = None


class GitHubRepositoryResourceCreate(BaseModel):
    type: Literal["github_repository"] = "github_repository"
    url: str
    authorization_token: str | None = None
    checkout: GitHubCheckout | None = None
    mount_path: str | None = None
    sparse_checkout_directories: list[str] | None = None


ResourceCreateParams = FileResourceCreate | GitHubRepositoryResourceCreate


class FileResource(BaseModel):
    id: str = Field(default_factory=lambda: f"res_{uuid.uuid4().hex[:24]}")
    type: Literal["file"] = "file"
    file_id: str
    mount_path: str | None = None
    status: str = "mounted"
    created_at: datetime = Field(default_factory=datetime.utcnow)
    updated_at: datetime = Field(default_factory=datetime.utcnow)


class GitHubRepositoryResource(BaseModel):
    id: str = Field(default_factory=lambda: f"res_{uuid.uuid4().hex[:24]}")
    type: Literal["github_repository"] = "github_repository"
    url: str
    mount_path: str | None = None
    checkout: GitHubCheckout | None = None
    sparse_checkout_directories: list[str] | None = None
    status: str = "mounted"
    created_at: datetime = Field(default_factory=datetime.utcnow)
    updated_at: datetime = Field(default_factory=datetime.utcnow)


SessionResource = FileResource | GitHubRepositoryResource


class DeletedResource(BaseModel):
    id: str
    type: str = "resource.deleted"


# ==============================================================================
# Events - the full event type system
# ==============================================================================


class Event(BaseModel):
    id: str = Field(default_factory=lambda: f"evt_{uuid.uuid4().hex[:24]}")
    session_id: str
    thread_id: str | None = None
    type: EventType
    payload: dict[str, Any] = Field(default_factory=dict)
    processed_at: datetime = Field(default_factory=datetime.utcnow)
    sequence_num: int = 0


# -- Event send params (client -> server) --


class TextBlock(BaseModel):
    type: Literal["text"] = "text"
    text: str


class ImageBlock(BaseModel):
    type: Literal["image"] = "image"
    source: dict[str, Any]


class DocumentBlock(BaseModel):
    type: Literal["document"] = "document"
    source: dict[str, Any]
    title: str | None = None
    context: str | None = None


ContentBlockUnion = TextBlock | ImageBlock | DocumentBlock


class UserMessageEventParams(BaseModel):
    type: Literal["user.message"] = "user.message"
    content: list[ContentBlockUnion | dict[str, Any]]


class UserInterruptEventParams(BaseModel):
    type: Literal["user.interrupt"] = "user.interrupt"


class UserToolConfirmationEventParams(BaseModel):
    type: Literal["user.tool_confirmation"] = "user.tool_confirmation"
    tool_use_id: str
    result: str
    deny_message: str | None = None


class UserCustomToolResultEventParams(BaseModel):
    type: Literal["user.custom_tool_result"] = "user.custom_tool_result"
    custom_tool_use_id: str
    content: str | list[dict[str, Any]] | None = None
    is_error: bool = False


class UserDefineOutcomeEventParams(BaseModel):
    type: Literal["user.define_outcome"] = "user.define_outcome"
    outcome: str
    metadata: dict[str, str] = Field(default_factory=dict)


EventSendParams = (
    UserMessageEventParams
    | UserInterruptEventParams
    | UserToolConfirmationEventParams
    | UserCustomToolResultEventParams
    | UserDefineOutcomeEventParams
)


class EventSend(BaseModel):
    events: list[EventSendParams]


# -- Error events --


class RetryStatusObj(BaseModel):
    type: RetryStatus


class SessionError(BaseModel):
    type: str
    message: str
    retry_status: RetryStatusObj
    mcp_server_name: str | None = None


# -- Idle stop reasons --


class SessionEndTurn(BaseModel):
    type: Literal["end_turn"] = "end_turn"


class SessionRequiresAction(BaseModel):
    type: Literal["requires_action"] = "requires_action"
    event_ids: list[str] = Field(default_factory=list)


class SessionRetriesExhausted(BaseModel):
    type: Literal["retries_exhausted"] = "retries_exhausted"


IdleStopReason = SessionEndTurn | SessionRequiresAction | SessionRetriesExhausted


# ==============================================================================
# LLM Messages (internal use - not exposed via API)
# ==============================================================================


class LLMContentBlock(BaseModel):
    type: str
    text: str | None = None
    id: str | None = None
    name: str | None = None
    input: dict[str, Any] | None = None
    content: str | None = None
    is_error: bool = False
    tool_use_id: str | None = None


class LLMMessage(BaseModel):
    role: str
    content: str | list[LLMContentBlock | dict[str, Any]]


class LLMResponse(BaseModel):
    content: list[LLMContentBlock]
    model: str = ""
    stop_reason: str | None = None
    usage: dict[str, int] = Field(default_factory=dict)


# ==============================================================================
# Execution Results
# ==============================================================================


class ToolResult(BaseModel):
    call_id: str
    output: str
    is_error: bool = False


class ExecutionResult(BaseModel):
    stdout: str = ""
    stderr: str = ""
    exit_code: int = 0


# ==============================================================================
# Skill
# ==============================================================================


class SkillCreate(BaseModel):
    name: str
    description: str = ""
    instructions: str = ""
    resources: dict[str, str] = Field(default_factory=dict)
    is_builtin: bool = False


class Skill(BaseModel):
    id: str = Field(default_factory=lambda: str(uuid.uuid4()))
    name: str
    version: str = "1.0.0"
    description: str = ""
    instructions: str = ""
    resources: dict[str, str] = Field(default_factory=dict)
    is_builtin: bool = False
    created_at: datetime = Field(default_factory=datetime.utcnow)


# ==============================================================================
# API Key
# ==============================================================================


class APIKey(BaseModel):
    id: str = Field(default_factory=lambda: f"key_{uuid.uuid4().hex[:24]}")
    name: str
    key_hash: str
    prefix: str
    created_at: datetime = Field(default_factory=datetime.utcnow)
    last_used_at: datetime | None = None
    revoked_at: datetime | None = None


# ==============================================================================
# Span models (observability events)
# ==============================================================================


class SpanModelUsage(BaseModel):
    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_input_tokens: int = 0
    cache_creation_input_tokens: int = 0


# ==============================================================================
# Uploaded Files (matching Anthropic's /v1/files API)
# ==============================================================================


class FileUpload(BaseModel):
    """Metadata for an uploaded file — returned by POST /v1/files."""
    id: str = Field(default_factory=lambda: f"file_{uuid.uuid4().hex[:24]}")
    type: str = "file"
    filename: str
    content_type: str = "application/octet-stream"
    size_bytes: int = 0
    purpose: str = "session_resource"
    status: str = "uploaded"
    created_at: datetime = Field(default_factory=datetime.utcnow)
    updated_at: datetime = Field(default_factory=datetime.utcnow)


class DeletedFile(BaseModel):
    id: str
    type: str = "file.deleted"


# ==============================================================================
# Billing & Usage Tracking
# ==============================================================================

# Per-model pricing (USD per 1M tokens) — updated April 2026
MODEL_PRICING: dict[str, dict[str, float]] = {
    # Anthropic
    "claude-sonnet-4-20250514": {"input": 3.00, "output": 15.00, "cache_read": 0.30},
    "claude-opus-4-20250514": {"input": 15.00, "output": 75.00, "cache_read": 1.50},
    "claude-haiku-3-5-20241022": {"input": 0.80, "output": 4.00, "cache_read": 0.08},
    # OpenAI
    "gpt-4o": {"input": 2.50, "output": 10.00, "cache_read": 1.25},
    "gpt-4o-mini": {"input": 0.15, "output": 0.60, "cache_read": 0.075},
    "gpt-4.1": {"input": 2.00, "output": 8.00, "cache_read": 0.50},
    "gpt-4.1-mini": {"input": 0.40, "output": 1.60, "cache_read": 0.10},
    "gpt-4.1-nano": {"input": 0.10, "output": 0.40, "cache_read": 0.025},
    "o3": {"input": 10.00, "output": 40.00, "cache_read": 2.50},
    "o3-mini": {"input": 1.10, "output": 4.40, "cache_read": 0.275},
    "o4-mini": {"input": 1.10, "output": 4.40, "cache_read": 0.275},
    # Google
    "gemini-2.5-pro": {"input": 1.25, "output": 10.00, "cache_read": 0.315},
    "gemini-2.5-flash": {"input": 0.15, "output": 0.60, "cache_read": 0.0375},
    "gemini-2.0-flash": {"input": 0.10, "output": 0.40, "cache_read": 0.025},
    # Groq (estimated, varies)
    "llama-3.3-70b-versatile": {"input": 0.59, "output": 0.79, "cache_read": 0.0},
    "llama-4-scout-17b-16e-instruct": {"input": 0.11, "output": 0.34, "cache_read": 0.0},
    # Ollama (local — zero cost)
    "ollama/*": {"input": 0.0, "output": 0.0, "cache_read": 0.0},
}


class SessionCost(BaseModel):
    """Computed cost for a session based on token usage and model pricing."""
    session_id: str
    model_id: str = ""
    input_tokens: int = 0
    output_tokens: int = 0
    cache_read_input_tokens: int = 0
    input_cost_usd: float = 0.0
    output_cost_usd: float = 0.0
    cache_read_cost_usd: float = 0.0
    total_cost_usd: float = 0.0
    currency: str = "USD"


class UsageSummary(BaseModel):
    """Aggregate usage across sessions."""
    total_sessions: int = 0
    total_input_tokens: int = 0
    total_output_tokens: int = 0
    total_cache_read_input_tokens: int = 0
    total_cost_usd: float = 0.0
    by_model: dict[str, dict[str, float]] = Field(default_factory=dict)
    period_start: datetime | None = None
    period_end: datetime | None = None


class SpanModelRequestEnd(BaseModel):
    model_request_start_id: str
    is_error: bool = False
    usage: SpanModelUsage = Field(default_factory=SpanModelUsage)
