package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/crypto"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/executor"
	"github.com/multica-ai/multica/server/internal/mcpclient"
	"github.com/multica-ai/multica/server/internal/oauth"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/session"
	"github.com/multica-ai/multica/server/internal/stealth"
	"github.com/multica-ai/multica/server/internal/stream"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ManagedSessionService handles executing managed agent sessions. It connects
// CreateManagedSession to actual agentic execution via the cloud-claude backend
// with server-side tool execution.
type ManagedSessionService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
	Bus     *events.Bus
	Logger  *slog.Logger

	// Session Store — Anthropic Managed Agents architecture
	Store       *session.Store
	CostTracker *session.CostTracker

	// activeSessions tracks running sessions so we can send them custom tool
	// results and tool confirmations from event handlers.
	mu             sync.Mutex
	activeSessions map[string]*agent.Session // keyed by session UUID string
}

// NewManagedSessionService creates a new session execution service.
func NewManagedSessionService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *ManagedSessionService {
	return &ManagedSessionService{
		Queries:        q,
		Hub:            hub,
		Bus:            bus,
		Logger:         slog.Default(),
		Store:          session.NewStore(q, hub, slog.Default()),
		CostTracker:    session.NewCostTracker(q),
		activeSessions: make(map[string]*agent.Session),
	}
}

// ExecuteOptions contains optional configuration for session execution.
type ExecuteOptions struct {
	StealthMode   bool
	ProxyURLs     []string
	Model         string
	ExecutionMode string // "browser" | "routine" | "hybrid" (empty => read from agent metadata)
}

// Execution modes supported by managed agents.
const (
	ExecModeBrowser = "browser" // Real-user browser automation via Playwright MCP
	ExecModeRoutine = "routine" // Headless / direct-API mode for repetitive tasks
	ExecModeHybrid  = "hybrid"  // Routine by default, escalate to browser when needed
)

// resolveExecutionMode picks the execution mode: explicit opt override wins,
// otherwise reads metadata.execution_mode from the agent, defaulting to
// "browser" (the user-facing "real AI agent driving the browser" default).
func resolveExecutionMode(a db.ManagedAgent, override string) string {
	if override != "" {
		return normalizeExecMode(override)
	}
	if a.Metadata != nil {
		var meta struct {
			ExecutionMode string `json:"execution_mode"`
		}
		if json.Unmarshal(a.Metadata, &meta) == nil && meta.ExecutionMode != "" {
			return normalizeExecMode(meta.ExecutionMode)
		}
	}
	return ExecModeBrowser
}

func normalizeExecMode(m string) string {
	switch m {
	case ExecModeBrowser, ExecModeRoutine, ExecModeHybrid:
		return m
	default:
		return ExecModeBrowser
	}
}

// ExecuteSession launches an agentic session in a goroutine. The session runs
// asynchronously, streaming events via SSE and WebSocket, executing tools
// server-side, and saving all results to the database.
func (s *ManagedSessionService) ExecuteSession(
	ctx context.Context,
	session db.ManagedSession,
	agentRow db.ManagedAgent,
	prompt string,
	opts ...ExecuteOptions,
) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	// Mark session as running
	s.Queries.UpdateManagedSessionStatus(ctx, db.UpdateManagedSessionStatusParams{
		ID:     session.ID,
		Status: "running",
	})

	sessionIDStr := util.UUIDToString(session.ID)
	workspaceIDStr := util.UUIDToString(session.WorkspaceID)

	// Broadcast session started
	s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.status_running", map[string]string{
		"agent_name": agentRow.Name,
		"status":     "running",
	})

	go func() {
		// Merge execute options
		var opt ExecuteOptions
		if len(opts) > 0 {
			opt = opts[0]
		}

		// Create tool executor
		rawExec := executor.NewExecutor(s.Queries, session.WorkspaceID, session.ID, s.Logger)
		defer rawExec.Close() // Clean up sandbox on completion

		// Apply stealth config if requested
		if opt.StealthMode {
			cfg := stealth.DefaultConfig()
			rawExec.Stealth = &cfg
			if len(opt.ProxyURLs) > 0 {
				rawExec.ProxyPool = stealth.NewProxyPool(opt.ProxyURLs)
			}
			s.Logger.Info("stealth mode enabled",
				"session_id", sessionIDStr,
				"proxies", len(opt.ProxyURLs))
		}

		// Load MCP connectors for this agent and create MCP pool
		var mcpPool *mcpclient.Pool
		var mcpToolDefs []agent.McpToolDef
		mcpConfigs := loadMcpConfigs(agentRow)

		// Also load DB-stored connectors (with OAuth credential injection)
		dbConfigs := s.loadMcpConnectorsFromDB(ctx, agentRow.ID, session.WorkspaceID)
		mcpConfigs = append(mcpConfigs, dbConfigs...)

		// Resolve execution mode and auto-inject default MCPs ("real AI agent"
		// experience: every browser/hybrid agent gets Playwright MCP for free
		// so it can drive the web like a real user. Routine agents skip it.)
		execMode := resolveExecutionMode(agentRow, opt.ExecutionMode)
		mcpConfigs = injectDefaultMcpConfigs(mcpConfigs, execMode)
		if len(mcpConfigs) > 0 {
			pool, err := mcpclient.NewPool(ctx, mcpConfigs, s.Logger)
			if err != nil {
				s.Logger.Warn("MCP pool creation failed, continuing without MCP tools",
					"error", err, "session_id", sessionIDStr)
			} else {
				mcpPool = pool
				defer mcpPool.Close()
				mcpToolDefs = mcpPool.Tools()

				// Wire MCP execution into the executor
				rawExec.McpExecute = func(ctx context.Context, toolName string, args map[string]any) (string, error) {
					return mcpPool.Execute(ctx, toolName, args)
				}

				s.Logger.Info("MCP pool connected",
					"session_id", sessionIDStr,
					"servers", len(mcpConfigs),
					"tools", len(mcpToolDefs))
			}
		}

		exec := &executorAdapter{exec: rawExec}

		// Extract model from agent config
		model := extractModelFromAgent(agentRow)

		// Build system prompt
		systemPrompt := agentRow.SystemPrompt.String
		if systemPrompt == "" {
			systemPrompt = buildDefaultAgentPrompt(agentRow, execMode)
		} else {
			// Append an execution-mode contract so even custom prompts benefit
			// from the browser / routine guidance.
			systemPrompt = systemPrompt + "\n\n" + executionModeContract(execMode)
		}

		// Build tool set from agent config — nil means use all tools
		var toolNames []string
		var customTools []string
		permissions := map[string]string{}
		if agentRow.Tools != nil {
			var toolConfigs []struct {
				Name             string `json:"name"`
				Type             string `json:"type"`
				PermissionPolicy string `json:"permission_policy"`
			}
			if json.Unmarshal(agentRow.Tools, &toolConfigs) == nil {
				for _, t := range toolConfigs {
					toolNames = append(toolNames, t.Name)
					if t.Type == "custom" {
						customTools = append(customTools, t.Name)
					}
					if t.PermissionPolicy != "" {
						permissions[t.Name] = t.PermissionPolicy
					}
				}
			}
		}

		// Create agentic backend with tool executor
		backend := agent.NewAgenticCloudClaude(apiKey, s.Logger, exec, toolNames)

		// Execute the agentic loop
		agentSession, err := backend.Execute(ctx, prompt, agent.ExecOptions{
			Model:           model,
			SystemPrompt:    systemPrompt,
			MaxTurns:        25,
			Timeout:         10 * time.Minute,
			CustomTools:     customTools,
			ToolPermissions: permissions,
			McpTools:        mcpToolDefs,
		})

		if err != nil {
			s.Logger.Error("managed session execution failed", "error", err, "session_id", sessionIDStr)
			s.failSession(ctx, session.ID, workspaceIDStr, sessionIDStr, err.Error())
			return
		}

		// Register active session for custom tool results / confirmations
		s.mu.Lock()
		s.activeSessions[sessionIDStr] = agentSession
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.activeSessions, sessionIDStr)
			s.mu.Unlock()
		}()

		// Drain messages, stream to clients, save events
		s.drainAndStream(ctx, agentSession, session, workspaceIDStr, sessionIDStr)
	}()

	return nil
}

// drainAndStream reads all messages from the agent session, persists them
// through the Session Store (append-only log with event indices), broadcasts
// them via SSE + WebSocket, tracks costs, and handles the final result.
func (s *ManagedSessionService) drainAndStream(
	ctx context.Context,
	agentSession *agent.Session,
	dbSession db.ManagedSession,
	workspaceIDStr, sessionIDStr string,
) {
	// Initialize the Session Store counter for this session
	s.Store.InitCounter(ctx, sessionIDStr)

	for msg := range agentSession.Messages {
		// Map agent message type to session event type
		evt := session.Event{
			SessionID: sessionIDStr,
			Type:      mapMessageType(msg.Type),
			Data:      mapMessageData(msg),
		}

		// Append to Session Store (durable, indexed, broadcast)
		idx, err := s.Store.AppendEvent(ctx, sessionIDStr, evt)
		if err != nil {
			s.Logger.Error("failed to append session event",
				"session_id", sessionIDStr, "error", err)
		}

		// Also stream via SSE (legacy path for backward compat)
		stream.Global.Broadcast(sessionIDStr, stream.Event{
			Type:    string(msg.Type),
			Content: formatMessageContent(msg),
		})

		// Broadcast via WebSocket with event index
		s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.message", map[string]any{
			"type":        string(msg.Type),
			"event_index": idx,
			"content":     msg.Content,
			"tool":        msg.Tool,
			"call_id":     msg.CallID,
			"status":      msg.Status,
		})
	}

	// Wait for final result
	result := <-agentSession.Result

	// Record cost via CostTracker
	for model, usage := range result.Usage {
		s.CostTracker.Record(ctx, session.CostRecord{
			SessionID:    sessionIDStr,
			WorkspaceID:  workspaceIDStr,
			Provider:     "anthropic",
			Model:        model,
			Operation:    "inference",
			TokensInput:  usage.InputTokens,
			TokensOutput: usage.OutputTokens,
			TokensCached: usage.CacheReadTokens,
		})
	}

	// Close session via Session Store
	finalStatus := "idle"
	if result.Status == "failed" || result.Status == "aborted" {
		finalStatus = "terminated"
	}

	s.Store.Close(ctx, sessionIDStr, finalStatus, map[string]any{
		"status":      result.Status,
		"error":       result.Error,
		"duration_ms": result.DurationMs,
	})

	// Update usage tokens (legacy path)
	for _, usage := range result.Usage {
		s.Queries.UpdateManagedSessionUsage(ctx, db.UpdateManagedSessionUsageParams{
			ID:                       dbSession.ID,
			UsageInputTokens:         usage.InputTokens,
			UsageOutputTokens:        usage.OutputTokens,
			UsageCacheCreationTokens: usage.CacheWriteTokens,
			UsageCacheReadTokens:     usage.CacheReadTokens,
		})
	}

	// Stream done event
	stream.Global.Broadcast(sessionIDStr, stream.Event{
		Type:    "done",
		Content: result.Output,
	})

	s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.status_idle", map[string]any{
		"status":      finalStatus,
		"duration_ms": result.DurationMs,
		"output":      truncateForBroadcast(result.Output),
	})

	// Publish bus event
	s.Bus.Publish(events.Event{
		Type:        "managed_session:completed",
		WorkspaceID: workspaceIDStr,
		ActorType:   "agent",
		Payload: map[string]any{
			"session_id":  sessionIDStr,
			"status":      finalStatus,
			"duration_ms": result.DurationMs,
		},
	})

	s.Logger.Info("managed session completed",
		"session_id", sessionIDStr,
		"status", finalStatus,
		"duration_ms", result.DurationMs,
	)
}

// mapMessageType converts agent.MessageType to session.EventType.
func mapMessageType(mt agent.MessageType) session.EventType {
	switch mt {
	case agent.MessageText:
		return session.EventAssistantMessage
	case agent.MessageToolUse:
		return session.EventToolCall
	case agent.MessageToolResult:
		return session.EventToolResult
	case agent.MessageThinking:
		return session.EventThinking
	default:
		return session.EventSystemEvent
	}
}

// mapMessageData converts agent.Message data to session.EventData.
func mapMessageData(msg agent.Message) session.EventData {
	switch msg.Type {
	case agent.MessageText:
		return session.EventData{
			Role:    "assistant",
			Content: msg.Content,
		}
	case agent.MessageToolUse:
		return session.EventData{
			ToolName: msg.Tool,
			CallID:   msg.CallID,
			Input:    msg.Input,
		}
	case agent.MessageToolResult:
		return session.EventData{
			ToolName: msg.Tool,
			CallID:   msg.CallID,
			Output:   msg.Output,
			IsError:  msg.Status == "error",
		}
	case agent.MessageThinking:
		return session.EventData{
			Thinking: msg.Content,
		}
	default:
		return session.EventData{
			EventName: string(msg.Type),
			Details:   msg.Content,
		}
	}
}

// failSession marks a session as failed and broadcasts the error.
func (s *ManagedSessionService) failSession(ctx context.Context, sessionID pgtype.UUID, workspaceIDStr, sessionIDStr, errMsg string) {
	s.Queries.UpdateManagedSessionStatus(ctx, db.UpdateManagedSessionStatusParams{
		ID:     sessionID,
		Status: "terminated",
	})

	stopReason, _ := json.Marshal(map[string]any{
		"status": "terminated",
		"error":  errMsg,
	})
	s.Queries.SetManagedSessionStopReason(ctx, sessionID, stopReason)

	stream.Global.Broadcast(sessionIDStr, stream.Event{
		Type:    "error",
		Content: errMsg,
	})

	s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.status_terminated", map[string]any{
		"error": errMsg,
	})
}

// broadcastEvent sends a WebSocket event to all workspace clients.
func (s *ManagedSessionService) broadcastEvent(workspaceIDStr, sessionIDStr, eventType string, data any) {
	msg, _ := json.Marshal(map[string]any{
		"type":       eventType,
		"session_id": sessionIDStr,
		"data":       data,
	})
	s.Hub.BroadcastToWorkspace(workspaceIDStr, msg)
}

// SendCustomToolResult sends a custom tool result to the waiting agentic loop.
func (s *ManagedSessionService) SendCustomToolResult(sessionID string, result agent.CustomToolResult) error {
	s.mu.Lock()
	sess, ok := s.activeSessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("no active session %s", sessionID)
	}
	select {
	case sess.CustomToolResults <- result:
		return nil
	default:
		return fmt.Errorf("custom tool result channel full for session %s", sessionID)
	}
}

// SendToolConfirmation sends a tool confirmation to the waiting agentic loop.
func (s *ManagedSessionService) SendToolConfirmation(sessionID string, conf agent.ToolConfirmation) error {
	s.mu.Lock()
	sess, ok := s.activeSessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("no active session %s", sessionID)
	}
	select {
	case sess.ToolConfirmations <- conf:
		return nil
	default:
		return fmt.Errorf("tool confirmation channel full for session %s", sessionID)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractModelFromAgent(a db.ManagedAgent) string {
	if a.Model == nil {
		return ""
	}
	var m struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(a.Model, &m) == nil {
		return m.ID
	}
	return ""
}

func buildDefaultAgentPrompt(a db.ManagedAgent, execMode string) string {
	desc := a.Description.String
	if desc == "" {
		desc = "a managed AI agent"
	}
	base := fmt.Sprintf(`You are %s, %s.

You are running as a managed agent on the Aurion platform.`, a.Name, desc)
	return base + "\n\n" + executionModeContract(execMode)
}

// executionModeContract returns the behavioral contract added to every system
// prompt so the model knows whether to drive the browser like a real user or
// operate headlessly through direct APIs.
func executionModeContract(mode string) string {
	switch mode {
	case ExecModeRoutine:
		return `## Execution mode: ROUTINE

This task is repetitive / programmatic. Do NOT open a browser. Use the most
efficient path:

- Direct HTTP / API calls (http_request, web_fetch) for data
- Connected MCP servers (email, GitHub, Slack, database, etc.) for side effects
- bash for local work; memory_read / memory_write for state between runs

Available tools:
- Managing issues (create_issue, list_issues, update_issue, add_comment)
- File I/O (read_file, write_file, edit, list_directory)
- Shell (bash)
- HTTP (http_request, web_fetch, web_search)
- Persistent memory (memory_read, memory_write)
- MCP servers already attached to this agent
- Sub-agent delegation (delegate_to_agent)

Be direct, batch operations, and return a structured summary at the end.`

	case ExecModeHybrid:
		return `## Execution mode: HYBRID

Prefer fast / API paths when possible; escalate to the browser (Playwright MCP)
only when the target has no public API, requires a login session, or needs to
be driven visually.

Decision rule:
1. Is there an API or MCP tool for this? Use it.
2. Otherwise call the Playwright MCP tools (browser_navigate, browser_click,
   browser_type, browser_snapshot, browser_evaluate, ...) — you are driving a
   real Chromium browser exactly like a human user would.

Available tools:
- All routine tools (HTTP, files, bash, memory, issues, MCP servers)
- Playwright MCP: browser_navigate, browser_snapshot, browser_click,
  browser_type, browser_press_key, browser_wait_for, browser_evaluate,
  browser_take_screenshot, browser_tab_*, browser_network_requests
- Sub-agent delegation (delegate_to_agent)

When driving the browser, always take a snapshot first to get element refs,
then act on them. Never guess selectors.`

	default: // ExecModeBrowser
		return `## Execution mode: BROWSER (real AI agent driving Chromium)

You operate the web like a real person using a real browser. You have a
Playwright MCP session with full Chromium behind the scenes. Whenever you
need to interact with any website — Gmail, LinkedIn, a CRM, a dashboard,
a form — you USE THE BROWSER TOOLS, not curl.

Core browser tools (from the Playwright MCP server):
- browser_navigate(url): go to a URL
- browser_snapshot(): return the accessibility tree with element refs — ALWAYS
  call this before clicking or typing so you have a ref to target
- browser_click(element, ref): click an element
- browser_type(element, ref, text, submit?): fill an input
- browser_press_key(key): keyboard input
- browser_wait_for({text|textGone|time}): wait for state
- browser_evaluate(function): run JS in the page
- browser_take_screenshot(): visual check when needed
- browser_tab_list / browser_tab_new / browser_tab_select / browser_tab_close
- browser_network_requests(): inspect XHR / fetch traffic
- browser_file_upload(paths): upload files
- browser_handle_dialog({accept, promptText}): accept/dismiss dialogs

Standard loop:
1. browser_navigate → browser_snapshot
2. Read the snapshot, pick the ref of the element you want
3. browser_click / browser_type with the exact ref
4. Re-snapshot to confirm state changed
5. Extract data with browser_evaluate or browser_snapshot text

You also have:
- HTTP (http_request, web_fetch, web_search) for quick API calls
- File / memory / bash / issue / MCP / delegate_to_agent tools

Guidelines:
- Never invent CSS selectors; always use refs from the latest snapshot
- Prefer structured data (snapshot / evaluate) over screenshots
- Respect rate limits and login sessions — treat the browser as the user's
- If a task is clearly repetitive (scraping a list, sending 100 emails, polling
  a feed), switch to direct HTTP / MCP tools mid-task to save time`
	}
}

// injectDefaultMcpConfigs adds the zero-config MCP servers that correspond to
// the requested execution mode. Today that means attaching Playwright MCP
// (headless Chromium, no API keys) to every browser / hybrid agent unless the
// user already pinned a playwright entry in their agent config.
func injectDefaultMcpConfigs(existing []mcpclient.Config, execMode string) []mcpclient.Config {
	if execMode == ExecModeRoutine {
		return existing
	}
	hasPlaywright := false
	for _, c := range existing {
		lc := strings.ToLower(c.Name)
		if strings.Contains(lc, "playwright") {
			hasPlaywright = true
			break
		}
	}
	if hasPlaywright {
		return existing
	}
	// Spawn Playwright MCP over stdio with a persistent profile dir so the
	// session can reuse cookies between tool calls.
	return append(existing, mcpclient.Config{
		Name:      "playwright",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@playwright/mcp@latest", "--headless", "--isolated"},
	})
}

func formatMessageContent(msg agent.Message) string {
	switch msg.Type {
	case agent.MessageToolUse:
		data, _ := json.Marshal(map[string]any{
			"tool":    msg.Tool,
			"call_id": msg.CallID,
			"input":   msg.Input,
		})
		return string(data)
	case agent.MessageToolResult:
		data, _ := json.Marshal(map[string]any{
			"tool":    msg.Tool,
			"call_id": msg.CallID,
			"output":  msg.Output,
		})
		return string(data)
	default:
		return msg.Content
	}
}

func truncateForBroadcast(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "..."
	}
	return s
}

// loadMcpConfigs extracts MCP server configurations from an agent's
// mcp_servers JSONB column. Returns nil if no MCP servers are configured.
func loadMcpConfigs(a db.ManagedAgent) []mcpclient.Config {
	if a.McpServers == nil {
		return nil
	}
	var specs []struct {
		Name      string            `json:"name"`
		Transport string            `json:"transport"`
		Command   string            `json:"command"`
		Args      []string          `json:"args"`
		URL       string            `json:"url"`
		Env       map[string]string `json:"env"`
	}
	if err := json.Unmarshal(a.McpServers, &specs); err != nil {
		return nil
	}
	configs := make([]mcpclient.Config, 0, len(specs))
	for _, s := range specs {
		if s.Name == "" {
			continue
		}
		configs = append(configs, mcpclient.Config{
			Name:      s.Name,
			Transport: s.Transport,
			Command:   s.Command,
			Args:      s.Args,
			URL:       s.URL,
			Env:       s.Env,
		})
	}
	return configs
}

// loadMcpConnectorsFromDB loads enabled MCP connectors from the agent_mcp_connector
// table, decrypts OAuth credentials from the vault, and injects access tokens
// as environment variables into the MCP server configs.
func (s *ManagedSessionService) loadMcpConnectorsFromDB(ctx context.Context, agentID, workspaceID pgtype.UUID) []mcpclient.Config {
	connectors, err := s.Queries.ListEnabledAgentMcpConnectors(ctx, agentID, workspaceID)
	if err != nil {
		s.Logger.Warn("failed to load MCP connectors from DB", "error", err)
		return nil
	}

	var configs []mcpclient.Config
	for _, c := range connectors {
		cfg := mcpclient.Config{
			Name:      c.Name,
			Transport: c.Transport,
			Command:   c.Command,
			URL:       c.ServerUrl,
		}

		// Parse args
		if c.Args != nil {
			var args []string
			if json.Unmarshal(c.Args, &args) == nil {
				cfg.Args = args
			}
		}

		// Parse env config
		env := make(map[string]string)
		if c.EnvConfig != nil {
			json.Unmarshal(c.EnvConfig, &env)
		}

		// If connector has a vault credential, decrypt and inject tokens
		if c.VaultCredentialID.Valid {
			cred, err := s.Queries.GetVaultCredential(ctx, c.VaultCredentialID)
			if err == nil && cred.EncryptedPayload != nil {
				decrypted, err := crypto.Decrypt(string(cred.EncryptedPayload))
				if err == nil {
					var credData map[string]any
					if json.Unmarshal([]byte(decrypted), &credData) == nil {
						// Extract access token
						if accessToken, ok := credData["access_token"].(string); ok && accessToken != "" {
							// Check if token needs refresh
							if refreshToken, ok := credData["refresh_token"].(string); ok && refreshToken != "" {
								if s.isTokenExpired(credData) {
									newToken, _, refreshErr := oauth.RefreshGoogleToken(refreshToken)
									if refreshErr == nil && newToken != "" {
										accessToken = newToken
										s.Logger.Info("refreshed OAuth token for MCP connector",
											"connector", c.Name)
									} else {
										s.Logger.Warn("failed to refresh OAuth token",
											"connector", c.Name, "error", refreshErr)
									}
								}
							}
							// Inject token into MCP server env
							env["GOOGLE_ACCESS_TOKEN"] = accessToken
							env["OAUTH_ACCESS_TOKEN"] = accessToken
						}
						// Also inject email if available
						if email, ok := credData["email"].(string); ok {
							env["GOOGLE_USER_EMAIL"] = email
						}
					}
				} else {
					s.Logger.Warn("failed to decrypt vault credential",
						"connector", c.Name, "error", err)
				}
			}
		}

		cfg.Env = env
		configs = append(configs, cfg)
	}

	return configs
}

// isTokenExpired checks if an OAuth token is expired based on obtained_at + expires_in.
func (s *ManagedSessionService) isTokenExpired(credData map[string]any) bool {
	obtainedAt, ok := credData["obtained_at"].(string)
	if !ok {
		return true // If we can't tell, assume expired
	}
	expiresIn, ok := credData["expires_in"].(float64)
	if !ok || expiresIn <= 0 {
		return true
	}
	t, err := time.Parse(time.RFC3339, obtainedAt)
	if err != nil {
		return true
	}
	// Token expired if obtained_at + expires_in - 5min buffer < now
	return time.Now().After(t.Add(time.Duration(expiresIn-300) * time.Second))
}

// executorAdapter wraps executor.Executor to satisfy the agent.ToolExecutor
// interface without creating a circular dependency (executor imports agent).
type executorAdapter struct {
	exec *executor.Executor
}

func (a *executorAdapter) Execute(ctx context.Context, toolName, callID string, input map[string]any) agent.ToolExecResult {
	r := a.exec.Execute(ctx, toolName, callID, input)
	return agent.ToolExecResult{
		CallID:  r.CallID,
		Output:  r.Output,
		IsError: r.IsError,
	}
}
