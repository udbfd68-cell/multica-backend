package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/executor"
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
}

// NewManagedSessionService creates a new session execution service.
func NewManagedSessionService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *ManagedSessionService {
	return &ManagedSessionService{
		Queries: q,
		Hub:     hub,
		Bus:     bus,
		Logger:  slog.Default(),
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
	s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.started", map[string]string{
		"agent_name": agentRow.Name,
		"status":     "running",
	})

	go func() {
		// Create tool executor
		rawExec := executor.NewExecutor(s.Queries, session.WorkspaceID, session.ID, s.Logger)
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
		if agentRow.Tools != nil {
			var customTools []struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(agentRow.Tools, &customTools) == nil {
				for _, t := range customTools {
					toolNames = append(toolNames, t.Name)
				}
			}
		}

		// Create agentic backend with tool executor
		backend := agent.NewAgenticCloudClaude(apiKey, s.Logger, exec, toolNames)

		// Execute the agentic loop
		agentSession, err := backend.Execute(ctx, prompt, agent.ExecOptions{
			Model:        model,
			SystemPrompt: systemPrompt,
			MaxTurns:     25,
			Timeout:      10 * time.Minute,
		})

		if err != nil {
			s.Logger.Error("managed session execution failed", "error", err, "session_id", sessionIDStr)
			s.failSession(ctx, session.ID, workspaceIDStr, sessionIDStr, err.Error())
			return
		}

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

	// Update session status
	finalStatus := "completed"
	if result.Status == "failed" || result.Status == "aborted" {
		finalStatus = "failed"
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

	s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.completed", map[string]any{
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
		Status: "failed",
	})

	stopReason, _ := json.Marshal(map[string]any{
		"status": "failed",
		"error":  errMsg,
	})
	s.Queries.SetManagedSessionStopReason(ctx, sessionID, stopReason)

	stream.Global.Broadcast(sessionIDStr, stream.Event{
		Type:    "error",
		Content: errMsg,
	})

	s.broadcastEvent(workspaceIDStr, sessionIDStr, "session.failed", map[string]any{
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

You are running as a managed agent on the Multica platform. You have access to tools for:
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
