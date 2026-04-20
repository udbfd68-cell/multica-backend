package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/crypto"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/executor"
	"github.com/multica-ai/multica/server/internal/mcpclient"
	"github.com/multica-ai/multica/server/internal/oauth"
	"github.com/multica-ai/multica/server/internal/realtime"
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
		activeSessions: make(map[string]*agent.Session),
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
		// Create tool executor
		rawExec := executor.NewExecutor(s.Queries, session.WorkspaceID, session.ID, s.Logger)
		defer rawExec.Close() // Clean up sandbox on completion

		// Load MCP connectors for this agent and create MCP pool
		var mcpPool *mcpclient.Pool
		var mcpToolDefs []agent.McpToolDef
		mcpConfigs := loadMcpConfigs(agentRow)

		// Also load DB-stored connectors (with OAuth credential injection)
		dbConfigs := s.loadMcpConnectorsFromDB(ctx, agentRow.ID, session.WorkspaceID)
		mcpConfigs = append(mcpConfigs, dbConfigs...)
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
			systemPrompt = buildDefaultAgentPrompt(agentRow)
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

// drainAndStream reads all messages from the agent session, broadcasts them
// via SSE + WebSocket, saves them as session events, and handles the final result.
func (s *ManagedSessionService) drainAndStream(
	ctx context.Context,
	agentSession *agent.Session,
	session db.ManagedSession,
	workspaceIDStr, sessionIDStr string,
) {
	for msg := range agentSession.Messages {
		// Stream via SSE
		stream.Global.Broadcast(sessionIDStr, stream.Event{
			Type:    string(msg.Type),
			Content: formatMessageContent(msg),
		})

		// Broadcast via WebSocket
		s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.message", map[string]any{
			"type":    string(msg.Type),
			"content": msg.Content,
			"tool":    msg.Tool,
			"call_id": msg.CallID,
			"status":  msg.Status,
		})

		// Save as session event
		payload, _ := json.Marshal(map[string]any{
			"type":    string(msg.Type),
			"content": msg.Content,
			"tool":    msg.Tool,
			"call_id": msg.CallID,
			"input":   msg.Input,
			"output":  msg.Output,
			"status":  msg.Status,
		})
		s.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
			SessionID: session.ID,
			Type:      "agent." + string(msg.Type),
			Payload:   payload,
		})
	}

	// Wait for final result
	result := <-agentSession.Result

	// Update session status — Anthropic uses idle (success) or terminated (error)
	finalStatus := "idle"
	if result.Status == "failed" || result.Status == "aborted" {
		finalStatus = "terminated"
	}

	s.Queries.UpdateManagedSessionStatus(ctx, db.UpdateManagedSessionStatusParams{
		ID:     session.ID,
		Status: finalStatus,
	})

	// Update usage tokens
	for _, usage := range result.Usage {
		s.Queries.UpdateManagedSessionUsage(ctx, db.UpdateManagedSessionUsageParams{
			ID:                       session.ID,
			UsageInputTokens:         usage.InputTokens,
			UsageOutputTokens:        usage.OutputTokens,
			UsageCacheCreationTokens: usage.CacheWriteTokens,
			UsageCacheReadTokens:     usage.CacheReadTokens,
		})
	}

	// Set stop reason
	stopReason, _ := json.Marshal(map[string]any{
		"status":      result.Status,
		"error":       result.Error,
		"duration_ms": result.DurationMs,
	})
	s.Queries.SetManagedSessionStopReason(ctx, session.ID, stopReason)

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

func buildDefaultAgentPrompt(a db.ManagedAgent) string {
	desc := a.Description.String
	if desc == "" {
		desc = "a managed AI agent"
	}
	return fmt.Sprintf(`You are %s, %s.

You are running as a managed agent on the Aurion platform. You have access to tools for:
- Managing issues (create, list, update, comment)
- File operations (read, write, list)
- Shell command execution
- HTTP requests
- Memory (persistent notes)
- Delegating tasks to other agents

Guidelines:
- Be direct and actionable
- Use tools to accomplish tasks rather than just explaining
- Track your work through the issue system
- Write clear, structured output`, a.Name, desc)
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
