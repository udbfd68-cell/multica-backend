// Package agent provides a unified interface for executing prompts via
// coding agents (Claude Code, Codex, OpenCode, OpenClaw, Hermes, Pi). It mirrors the happy-cli AgentBackend
// pattern, translated to idiomatic Go.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Backend is the unified interface for executing prompts via coding agents.
type Backend interface {
	// Execute runs a prompt and returns a Session for streaming results.
	// The caller should read from Session.Messages (optional) and wait on
	// Session.Result for the final outcome.
	Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error)
}

// ExecOptions configures a single execution.
type ExecOptions struct {
	Cwd             string
	Model           string
	SystemPrompt    string
	MaxTurns        int
	Timeout         time.Duration
	ResumeSessionID string          // if non-empty, resume a previous agent session
	CustomArgs      []string        // additional CLI arguments appended to the agent command
	McpServers      []McpServerSpec // MCP servers to attach to this execution
	// ToolPermissions maps tool names to their permission policy.
	// "always_allow" (default), "always_ask" (requires confirmation), "always_deny".
	ToolPermissions map[string]string
	// CustomTools lists tools that the agent advertises but the server
	// cannot execute. When called, the loop pauses until the client sends
	// the result via CustomToolResults.
	CustomTools []string
	// McpTools are tools discovered from MCP servers, added to the tool
	// set dynamically. Execution is handled by the MCP pool outside the loop.
	McpTools []McpToolDef
}

// McpServerSpec defines an MCP server to connect during agent execution.
type McpServerSpec struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "stdio" | "sse" | "streamable-http"
	Command   string            `json:"command"`   // for stdio
	Args      []string          `json:"args"`
	URL       string            `json:"url"`       // for sse/streamable-http
	Env       map[string]string `json:"env"`       // environment variables
}

// McpToolDef describes a tool discovered from an MCP server.
type McpToolDef struct {
	Name        string // namespaced: "server.tool"
	ServerName  string // original server name
	ToolName    string // original tool name on the server
	Description string
	InputSchema any
}

// Session represents a running agent execution.
type Session struct {
	// Messages streams events as the agent works. The channel is closed
	// when the agent finishes (before Result is sent).
	Messages <-chan Message
	// Result receives exactly one value — the final outcome — then closes.
	Result <-chan Result
	// CustomToolResults is used to send custom tool results back to the
	// agentic loop when it pauses for a tool it can't execute locally.
	// The handler sends results on this channel; the loop receives them.
	CustomToolResults chan CustomToolResult
	// ToolConfirmations is used to send approval/denial for tools that
	// require permission (permission_policy == "always_ask").
	ToolConfirmations chan ToolConfirmation
}

// CustomToolResult is the result of a custom (client-side) tool execution
// sent back by the client via the user.custom_tool_result event.
type CustomToolResult struct {
	CallID  string
	Output  string
	IsError bool
}

// ToolConfirmation is the approval/denial from the client for a tool
// that has permission_policy == "always_ask".
type ToolConfirmation struct {
	CallID   string
	Approved bool
	Reason   string // optional denial reason
}

// MessageType identifies the kind of Message.
type MessageType string

const (
	MessageText            MessageType = "text"
	MessageThinking        MessageType = "thinking"
	MessageToolUse         MessageType = "tool-use"
	MessageToolResult      MessageType = "tool-result"
	MessageStatus          MessageType = "status"
	MessageError           MessageType = "error"
	MessageLog             MessageType = "log"
	MessageCustomToolUse   MessageType = "custom-tool-use"   // tool requires client execution
	MessageToolConfirmReq  MessageType = "tool-confirm-req"  // tool needs permission approval
)

// Message is a unified event emitted by an agent during execution.
type Message struct {
	Type    MessageType
	Content string         // text content (Text, Error, Log)
	Tool    string         // tool name (ToolUse, ToolResult)
	CallID  string         // tool call ID (ToolUse, ToolResult)
	Input   map[string]any // tool input (ToolUse)
	Output  string         // tool output (ToolResult)
	Status  string         // agent status string (Status)
	Level   string         // log level (Log)
}

// TokenUsage tracks token consumption for a single model.
type TokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// Result is the final outcome after an agent session completes.
type Result struct {
	Status     string // "completed", "failed", "aborted", "timeout"
	Output     string // accumulated text output
	Error      string // error message if failed
	DurationMs int64
	SessionID  string
	Usage      map[string]TokenUsage // keyed by model name
}

// Config configures a Backend instance.
type Config struct {
	ExecutablePath string            // path to CLI binary (claude, codex, copilot, opencode, openclaw, hermes, gemini, or pi)
	Env            map[string]string // extra environment variables
	Logger         *slog.Logger
}

// New creates a Backend for the given agent type.
// Supported types: "claude", "cloud-claude", "codex", "copilot", "opencode", "openclaw", "hermes", "gemini", "pi", "cursor".
func New(agentType string, cfg Config) (Backend, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	switch agentType {
	case "claude":
		return &claudeBackend{cfg: cfg}, nil
	case "cloud-claude":
		apiKey := cfg.Env["ANTHROPIC_API_KEY"]
		if apiKey == "" {
			return nil, fmt.Errorf("cloud-claude requires ANTHROPIC_API_KEY in env")
		}
		return NewCloudClaude(apiKey, cfg.Logger), nil
	case "codex":
		return &codexBackend{cfg: cfg}, nil
	case "copilot":
		return &copilotBackend{cfg: cfg}, nil
	case "opencode":
		return &opencodeBackend{cfg: cfg}, nil
	case "openclaw":
		return &openclawBackend{cfg: cfg}, nil
	case "hermes":
		return &hermesBackend{cfg: cfg}, nil
	case "gemini":
		return &geminiBackend{cfg: cfg}, nil
	case "pi":
		return &piBackend{cfg: cfg}, nil
	case "cursor":
		return &cursorBackend{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %q (supported: claude, cloud-claude, codex, copilot, opencode, openclaw, hermes, gemini, pi, cursor)", agentType)
	}
}

// DetectVersion runs the agent CLI with --version and returns the output.
func DetectVersion(ctx context.Context, executablePath string) (string, error) {
	return detectCLIVersion(ctx, executablePath)
}
