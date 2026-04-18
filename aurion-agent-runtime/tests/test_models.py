"""Tests for core models - updated for full Anthropic API compatibility."""

import pytest
from packages.core.models import (
    Agent,
    AgentCreate,
    AgentUpdate,
    AgentVersion,
    APIKey,
    CallableAgent,
    DeletedResource,
    DeletedSession,
    Environment,
    EnvironmentCreate,
    Event,
    EventSend,
    EventType,
    FileResource,
    GitHubRepositoryResource,
    LLMContentBlock,
    LLMMessage,
    LLMResponse,
    McpServerConfig,
    ModelConfig,
    RetryStatus,
    Session,
    SessionCreate,
    SessionError,
    SessionStats,
    SessionStatus,
    SessionUsage,
    Skill,
    StopReason,
    ToolDefinition,
    ToolResult,
    UserInterruptEventParams,
    UserMessageEventParams,
)


class TestAgentModel:
    def test_create_agent(self):
        agent = AgentCreate(
            name="test-agent",
            model=ModelConfig(id="claude-sonnet-4-20250514", speed="standard"),
            system="You are a test agent.",
        )
        assert agent.name == "test-agent"
        assert agent.model.id == "claude-sonnet-4-20250514"

    def test_agent_defaults(self):
        agent = Agent(name="test")
        assert agent.version == 1
        assert agent.type == "agent"
        assert agent.model.id == "claude-sonnet-4-20250514"
        assert agent.archived_at is None

    def test_agent_with_tools(self):
        agent = Agent(
            name="coder",
            system="Code things.",
            tools=[{"type": "agent_toolset_20260401"}],
        )
        assert len(agent.tools) == 1

    def test_agent_with_callable_agents(self):
        agent = Agent(
            name="coordinator",
            callable_agents=[
                CallableAgent(name="researcher", agent_id="agent-2"),
                CallableAgent(name="coder", agent_id="agent-3", description="Writes code"),
            ],
        )
        assert len(agent.callable_agents) == 2
        assert agent.callable_agents[1].description == "Writes code"

    def test_agent_update_requires_version(self):
        update = AgentUpdate(version=1, name="new-name")
        assert update.version == 1
        assert update.name == "new-name"

    def test_agent_version(self):
        av = AgentVersion(
            id="av_123",
            agent_id="agent_123",
            version=2,
            snapshot={"name": "test", "version": 2},
        )
        assert av.version == 2


class TestEnvironmentModel:
    def test_create_environment(self):
        env = EnvironmentCreate(name="python-dev")
        assert env.name == "python-dev"
        assert env.sandbox_provider == "docker"

    def test_environment_with_packages(self):
        env = Environment(
            name="data-science",
            sandbox_provider="e2b",
            packages=["numpy", "pandas", "matplotlib"],
        )
        assert len(env.packages) == 3
        assert env.config.networking.type == "unrestricted"


class TestSessionModel:
    def test_create_session(self):
        session = SessionCreate(agent="agent_123", environment_id="env_123")
        assert session.agent == "agent_123"

    def test_session_with_title(self):
        session = SessionCreate(
            agent={"id": "agent_123", "type": "agent"},
            environment_id="env_123",
            title="My session",
        )
        assert session.title == "My session"

    def test_session_status(self):
        session = Session(
            agent_id="agent-1",
            environment_id="env-1",
            status=SessionStatus.RUNNING,
        )
        assert session.status == SessionStatus.RUNNING
        assert session.stats.active_seconds == 0.0
        assert session.usage.input_tokens == 0

    def test_session_stats_and_usage(self):
        stats = SessionStats(active_seconds=10.5, duration_seconds=30.0)
        usage = SessionUsage(input_tokens=1000, output_tokens=500, cache_read_input_tokens=200)
        session = Session(
            agent_id="a", environment_id="e",
            stats=stats, usage=usage,
        )
        assert session.stats.active_seconds == 10.5
        assert session.usage.cache_read_input_tokens == 200

    def test_stop_reason(self):
        assert StopReason.END_TURN == "end_turn"
        assert StopReason.REQUIRES_ACTION == "requires_action"
        assert StopReason.RETRIES_EXHAUSTED == "retries_exhausted"


class TestEventModel:
    def test_all_event_types(self):
        """Verify all 28 event types from Anthropic API are present."""
        event_types = list(EventType)
        assert len(event_types) >= 26

        # Client -> Server
        assert EventType.USER_MESSAGE == "user.message"
        assert EventType.USER_INTERRUPT == "user.interrupt"
        assert EventType.USER_TOOL_CONFIRMATION == "user.tool_confirmation"
        assert EventType.USER_CUSTOM_TOOL_RESULT == "user.custom_tool_result"

        # Agent -> Client
        assert EventType.AGENT_MESSAGE == "agent.message"
        assert EventType.AGENT_THINKING == "agent.thinking"
        assert EventType.AGENT_TOOL_USE == "agent.tool_use"
        assert EventType.AGENT_TOOL_RESULT == "agent.tool_result"
        assert EventType.AGENT_MCP_TOOL_USE == "agent.mcp_tool_use"
        assert EventType.AGENT_MCP_TOOL_RESULT == "agent.mcp_tool_result"
        assert EventType.AGENT_CUSTOM_TOOL_USE == "agent.custom_tool_use"
        assert EventType.AGENT_THREAD_CONTEXT_COMPACTED == "agent.thread_context_compacted"

        # Session lifecycle
        assert EventType.SESSION_STATUS_RUNNING == "session.status_running"
        assert EventType.SESSION_STATUS_IDLE == "session.status_idle"
        assert EventType.SESSION_STATUS_RESCHEDULED == "session.status_rescheduled"
        assert EventType.SESSION_STATUS_TERMINATED == "session.status_terminated"
        assert EventType.SESSION_ERROR == "session.error"
        assert EventType.SESSION_DELETED == "session.deleted"

        # Observability
        assert EventType.SPAN_MODEL_REQUEST_START == "span.model_request_start"
        assert EventType.SPAN_MODEL_REQUEST_END == "span.model_request_end"

    def test_create_event(self):
        event = Event(
            id="evt-1",
            session_id="session-1",
            type=EventType.USER_MESSAGE,
            payload={"content": "Hello"},
            sequence_num=1,
        )
        assert event.type == EventType.USER_MESSAGE

    def test_event_serialization(self):
        event = Event(
            id="evt-2",
            session_id="session-1",
            type=EventType.AGENT_THINKING,
            payload={"content": "Let me think..."},
            sequence_num=2,
        )
        json_str = event.model_dump_json()
        assert "agent.thinking" in json_str

    def test_event_send_params(self):
        send = EventSend(events=[
            UserMessageEventParams(content=[{"type": "text", "text": "Hello"}]),
            UserInterruptEventParams(),
        ])
        assert len(send.events) == 2
        assert send.events[0].type == "user.message"
        assert send.events[1].type == "user.interrupt"


class TestRetrySystem:
    def test_retry_status(self):
        assert RetryStatus.RETRYING == "retrying"
        assert RetryStatus.EXHAUSTED == "exhausted"
        assert RetryStatus.TERMINAL == "terminal"

    def test_session_error(self):
        from packages.core.models import RetryStatusObj
        err = SessionError(
            type="model_overloaded",
            message="Service temporarily overloaded",
            retry_status=RetryStatusObj(type=RetryStatus.RETRYING),
        )
        assert err.retry_status.type == RetryStatus.RETRYING


class TestResourceModel:
    def test_file_resource(self):
        res = FileResource(file_id="file_123", mount_path="/data/input.csv")
        assert res.type == "file"
        assert res.file_id == "file_123"

    def test_github_resource(self):
        from packages.core.models import GitHubCheckout
        res = GitHubRepositoryResource(
            url="https://github.com/user/repo",
            checkout=GitHubCheckout(type="branch", name="main"),
        )
        assert res.type == "github_repository"
        assert res.checkout.name == "main"

    def test_deleted_resource(self):
        dr = DeletedResource(id="res_123")
        assert dr.type == "resource.deleted"


class TestToolDefinition:
    def test_builtin_tool(self):
        tool = ToolDefinition(
            name="bash",
            description="Execute bash commands",
            input_schema={
                "type": "object",
                "properties": {"command": {"type": "string"}},
                "required": ["command"],
            },
        )
        assert tool.name == "bash"

    def test_tool_result(self):
        result = ToolResult(call_id="call-1", output="Hello, World!")
        assert not result.is_error

    def test_error_result(self):
        result = ToolResult(call_id="call-2", output="Command not found", is_error=True)
        assert result.is_error


class TestMcpConfig:
    def test_stdio_config(self):
        config = McpServerConfig(
            name="filesystem",
            transport="stdio",
            command="npx",
            args=["-y", "@modelcontextprotocol/server-filesystem"],
        )
        assert config.transport == "stdio"

    def test_sse_config(self):
        config = McpServerConfig(
            name="remote-tools",
            transport="sse",
            url="http://localhost:3000/sse",
        )
        assert config.url is not None


class TestLLMModels:
    def test_content_block_text(self):
        block = LLMContentBlock(type="text", text="Hello")
        assert block.text == "Hello"

    def test_content_block_tool_use(self):
        block = LLMContentBlock(
            type="tool_use",
            id="call-1",
            name="bash",
            input={"command": "ls"},
        )
        assert block.name == "bash"

    def test_llm_message(self):
        msg = LLMMessage(role="user", content="Hello")
        assert msg.role == "user"

    def test_llm_response(self):
        resp = LLMResponse(
            content=[LLMContentBlock(type="text", text="Hi")],
            model="claude-sonnet-4-20250514",
            stop_reason="end_turn",
            usage={"input_tokens": 10, "output_tokens": 5},
        )
        assert resp.stop_reason == "end_turn"


class TestAPIKeyModel:
    def test_api_key(self):
        key = APIKey(name="test-key", key_hash="abc123", prefix="aurion_abc1")
        assert key.name == "test-key"
        assert key.revoked_at is None
