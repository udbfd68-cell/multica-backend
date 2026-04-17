package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/mention"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/stream"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type TaskService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
	Bus     *events.Bus
}

func NewTaskService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *TaskService {
	return &TaskService{Queries: q, Hub: hub, Bus: bus}
}

// EnqueueTaskForIssue creates a queued task for an agent-assigned issue.
// No context snapshot is stored — the agent fetches all data it needs at
// runtime via the multica CLI.
func (s *TaskService) EnqueueTaskForIssue(ctx context.Context, issue db.Issue, triggerCommentID ...pgtype.UUID) (db.AgentTaskQueue, error) {
	if !issue.AssigneeID.Valid {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", "issue has no assignee")
		return db.AgentTaskQueue{}, fmt.Errorf("issue has no assignee")
	}

	agent, err := s.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("task enqueue skipped: agent is archived", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agent.ID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", "agent has no runtime")
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime")
	}

	// Check runtime is online before enqueuing — prevents tasks rotting in queue
	runtime, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		slog.Error("task enqueue failed: runtime not found", "issue_id", util.UUIDToString(issue.ID), "runtime_id", util.UUIDToString(agent.RuntimeID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load runtime: %w", err)
	}
	if runtime.Status == "offline" {
		slog.Warn("task enqueue failed: runtime is offline", "issue_id", util.UUIDToString(issue.ID), "runtime_id", util.UUIDToString(agent.RuntimeID))
		return db.AgentTaskQueue{}, fmt.Errorf("runtime %q is offline — start the daemon first", runtime.Name)
	}

	var commentID pgtype.UUID
	if len(triggerCommentID) > 0 {
		commentID = triggerCommentID[0]
	}

	task, err := s.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		AgentID:          issue.AssigneeID,
		RuntimeID:        agent.RuntimeID,
		IssueID:          issue.ID,
		Priority:         priorityToInt(issue.Priority),
		TriggerCommentID: commentID,
	})
	if err != nil {
		slog.Error("task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create task: %w", err)
	}

	slog.Info("task enqueued", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(issue.AssigneeID))
	return task, nil
}

// EnqueueTaskForMention creates a queued task for a mentioned agent on an issue.
// Unlike EnqueueTaskForIssue, this takes an explicit agent ID rather than
// deriving it from the issue assignee.
func (s *TaskService) EnqueueTaskForMention(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentTaskQueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("mention task enqueue failed: agent not found", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("mention task enqueue skipped: agent is archived", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		slog.Error("mention task enqueue failed: agent has no runtime", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime")
	}

	// Check runtime is online before enqueuing
	runtime, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("load runtime: %w", err)
	}
	if runtime.Status == "offline" {
		slog.Warn("mention task enqueue failed: runtime offline", "agent_id", util.UUIDToString(agentID), "runtime_id", util.UUIDToString(agent.RuntimeID))
		return db.AgentTaskQueue{}, fmt.Errorf("runtime %q is offline — start the daemon first", runtime.Name)
	}

	task, err := s.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		AgentID:          agentID,
		RuntimeID:        agent.RuntimeID,
		IssueID:          issue.ID,
		Priority:         priorityToInt(issue.Priority),
		TriggerCommentID: triggerCommentID,
	})
	if err != nil {
		slog.Error("mention task enqueue failed", "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create task: %w", err)
	}

	slog.Info("mention task enqueued", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(issue.ID), "agent_id", util.UUIDToString(agentID))
	return task, nil
}

// EnqueueChatTask creates a queued task for a chat session.
// Unlike issue tasks, chat tasks have no issue_id.
// If the agent uses cloud runtime mode, the task is executed directly
// via the Anthropic API without requiring a daemon.
func (s *TaskService) EnqueueChatTask(ctx context.Context, chatSession db.ChatSession) (db.AgentTaskQueue, error) {
	agentRow, err := s.Queries.GetAgent(ctx, chatSession.AgentID)
	if err != nil {
		slog.Error("chat task enqueue failed", "chat_session_id", util.UUIDToString(chatSession.ID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agentRow.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}

	// Cloud execution: when we have an API key, execute directly on the server
	// without requiring a daemon — either for cloud-mode agents or as fallback
	// when the daemon runtime is offline.
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" && (agentRow.RuntimeMode == "cloud" || !agentRow.RuntimeID.Valid) {
		return s.executeCloudChatTask(ctx, chatSession, agentRow, apiKey)
	}

	// Demo mode: when no API key is set but agent is cloud-mode,
	// respond with a synthetic message so the full pipeline is exercised.
	if !agentRow.RuntimeID.Valid && agentRow.RuntimeMode == "cloud" {
		return s.executeDemoChatTask(ctx, chatSession, agentRow)
	}

	if !agentRow.RuntimeID.Valid {
		return db.AgentTaskQueue{}, fmt.Errorf("agent has no runtime — set ANTHROPIC_API_KEY for cloud execution")
	}

	// Check runtime is online before enqueuing
	runtime, err := s.Queries.GetAgentRuntime(ctx, agentRow.RuntimeID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("load runtime: %w", err)
	}

	// If runtime is offline but we have an API key, fall back to cloud execution
	if runtime.Status == "offline" && apiKey != "" {
		slog.Info("runtime offline, falling back to cloud execution",
			"agent_id", util.UUIDToString(agentRow.ID),
			"runtime_id", util.UUIDToString(agentRow.RuntimeID))
		return s.executeCloudChatTask(ctx, chatSession, agentRow, apiKey)
	}

	if runtime.Status == "offline" {
		return db.AgentTaskQueue{}, fmt.Errorf("runtime %q is offline — start the daemon first", runtime.Name)
	}

	task, err := s.Queries.CreateChatTask(ctx, db.CreateChatTaskParams{
		AgentID:       chatSession.AgentID,
		RuntimeID:     agentRow.RuntimeID,
		Priority:      2, // medium priority for chat
		ChatSessionID: chatSession.ID,
	})
	if err != nil {
		slog.Error("chat task enqueue failed", "chat_session_id", util.UUIDToString(chatSession.ID), "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create chat task: %w", err)
	}

	slog.Info("chat task enqueued", "task_id", util.UUIDToString(task.ID), "chat_session_id", util.UUIDToString(chatSession.ID), "agent_id", util.UUIDToString(chatSession.AgentID))
	return task, nil
}

// executeCloudChatTask runs a chat task directly on the server using the Anthropic API.
// This is the "managed agent" path — no daemon or CLI required.
func (s *TaskService) executeCloudChatTask(ctx context.Context, chatSession db.ChatSession, agentRow db.Agent, apiKey string) (db.AgentTaskQueue, error) {
	if apiKey == "" {
		return db.AgentTaskQueue{}, fmt.Errorf("cloud execution requires ANTHROPIC_API_KEY")
	}

	// Create the task in the DB so we can track it
	task, err := s.Queries.CreateChatTask(ctx, db.CreateChatTaskParams{
		AgentID:       chatSession.AgentID,
		RuntimeID:     agentRow.RuntimeID,
		Priority:      2,
		ChatSessionID: chatSession.ID,
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create cloud chat task: %w", err)
	}

	taskID := task.ID
	agentID := agentRow.ID

	// Execute asynchronously — return the task immediately, process in background
	go func() {
		execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// Mark task as running (direct transition from queued, bypassing dispatch)
		s.forceTaskRunning(execCtx, taskID)
		s.updateAgentStatus(execCtx, agentID, "working")

		// Build prompt from chat history
		prompt, systemPrompt := s.buildChatPrompt(execCtx, chatSession, agentRow)

		// Determine model from runtime_config
		model := extractModel(agentRow.RuntimeConfig)

		backend := agent.NewCloudClaude(apiKey, slog.Default())
		session, err := backend.Execute(execCtx, prompt, agent.ExecOptions{
			Model:        model,
			SystemPrompt: systemPrompt,
			Timeout:      10 * time.Minute,
		})
		if err != nil {
			slog.Error("cloud task execution failed", "task_id", util.UUIDToString(taskID), "error", err)
			s.FailTask(execCtx, taskID, err.Error())
			return
		}

		// Drain messages (broadcast streaming events)
		workspaceID := util.UUIDToString(chatSession.WorkspaceID)
		sessionIDStr := util.UUIDToString(chatSession.ID)
		var messages []agent.Message
		seq := 0
		for msg := range session.Messages {
			messages = append(messages, msg)
			// Broadcast to SSE stream subscribers
			stream.Global.Broadcast(sessionIDStr, stream.Event{
				Type:    string(msg.Type),
				Content: msg.Content,
			})
			// Also broadcast via event bus for WebSocket clients
			s.Bus.Publish(events.Event{
				Type:        protocol.EventTaskMessage,
				WorkspaceID: workspaceID,
				ActorType:   "system",
				ActorID:     "",
				Payload: protocol.TaskMessagePayload{
					TaskID:        util.UUIDToString(taskID),
					ChatSessionID: sessionIDStr,
					Seq:           seq,
					Type:          string(msg.Type),
					Content:       msg.Content,
				},
			})
			seq++
		}

		// Wait for result
		result := <-session.Result

		// Save messages to DB for task transcript
		for seq, msg := range messages {
			if msg.Content == "" && msg.Tool == "" {
				continue
			}
			contentJSON, _ := json.Marshal(map[string]any{
				"type":    string(msg.Type),
				"content": msg.Content,
			})
			var inputJSON []byte
			if msg.Input != nil {
				inputJSON, _ = json.Marshal(msg.Input)
			}
			s.Queries.CreateTaskMessage(execCtx, db.CreateTaskMessageParams{
				TaskID:  taskID,
				Seq:     int32(seq),
				Type:    string(msg.Type),
				Tool:    pgtype.Text{String: msg.Tool, Valid: msg.Tool != ""},
				Content: pgtype.Text{String: string(contentJSON), Valid: true},
				Input:   inputJSON,
				Output:  pgtype.Text{String: msg.Output, Valid: msg.Output != ""},
			})
		}

		// Report usage
		for modelName, usage := range result.Usage {
			s.Queries.UpsertTaskUsage(execCtx, db.UpsertTaskUsageParams{
				TaskID:           taskID,
				Provider:         "anthropic",
				Model:            modelName,
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				CacheReadTokens:  usage.CacheReadTokens,
				CacheWriteTokens: usage.CacheWriteTokens,
			})
		}

		// Complete the task
		resultJSON, _ := json.Marshal(protocol.TaskCompletedPayload{
			Output: result.Output,
		})
		s.CompleteTask(execCtx, taskID, resultJSON, "", "")

		// Signal SSE stream completion
		stream.Global.Broadcast(sessionIDStr, stream.Event{
			Type:    "done",
			Content: result.Output,
		})
	}()

	slog.Info("cloud chat task started", "task_id", util.UUIDToString(task.ID), "chat_session_id", util.UUIDToString(chatSession.ID))
	return task, nil
}

// buildChatPrompt constructs the prompt for a cloud chat task from chat history.
func (s *TaskService) buildChatPrompt(ctx context.Context, chatSession db.ChatSession, agentRow db.Agent) (string, string) {
	// Build system prompt from agent instructions
	systemPrompt := agentRow.Instructions
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are %s, a managed AI agent on the Multica platform. %s\n\nYou are helpful, direct, and take action when possible. Use markdown for code and structured output.", agentRow.Name, agentRow.Description)
	}

	// Get recent chat messages for context
	messages, err := s.Queries.ListChatMessages(ctx, chatSession.ID)
	if err != nil || len(messages) == 0 {
		return "Hello", systemPrompt
	}

	// Build conversation as a single prompt (the cloud backend sends it as one user message)
	var prompt strings.Builder
	for i, m := range messages {
		if i > 0 {
			prompt.WriteString("\n\n")
		}
		switch m.Role {
		case "user":
			prompt.WriteString("[User]: ")
			prompt.WriteString(m.Content)
		case "assistant":
			prompt.WriteString("[Assistant]: ")
			prompt.WriteString(m.Content)
		}
	}

	return prompt.String(), systemPrompt
}

// extractModel pulls the model name from agent runtime_config JSON.
func extractModel(config []byte) string {
	if config == nil {
		return ""
	}
	var rc map[string]any
	if err := json.Unmarshal(config, &rc); err != nil {
		return ""
	}
	if m, ok := rc["model"].(string); ok {
		return m
	}
	return ""
}

// executeDemoChatTask runs a synthetic chat response when no API key is configured.
// This exercises the full pipeline (task creation, WebSocket broadcast, message storage)
// and shows users the platform is functional.
func (s *TaskService) executeDemoChatTask(ctx context.Context, chatSession db.ChatSession, agentRow db.Agent) (db.AgentTaskQueue, error) {
	task, err := s.Queries.CreateChatTask(ctx, db.CreateChatTaskParams{
		AgentID:       chatSession.AgentID,
		RuntimeID:     agentRow.RuntimeID,
		Priority:      2,
		ChatSessionID: chatSession.ID,
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create demo chat task: %w", err)
	}

	taskID := task.ID
	agentID := agentRow.ID

	go func() {
		execCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		s.forceTaskRunning(execCtx, taskID)
		s.updateAgentStatus(execCtx, agentID, "working")

		// Get the user's last message for a contextual response
		messages, _ := s.Queries.ListChatMessages(execCtx, chatSession.ID)
		lastMsg := "Hello"
		if len(messages) > 0 {
			lastMsg = messages[len(messages)-1].Content
		}

		// Generate a demo response
		demoResponse := generateDemoResponse(agentRow.Name, lastMsg)

		workspaceID := util.UUIDToString(chatSession.WorkspaceID)
		sessionIDStr := util.UUIDToString(chatSession.ID)

		// Simulate streaming: send the response in chunks
		chunks := splitIntoChunks(demoResponse, 20)
		for seq, chunk := range chunks {
			stream.Global.Broadcast(sessionIDStr, stream.Event{
				Type:    "text",
				Content: chunk,
			})
			s.Bus.Publish(events.Event{
				Type:        protocol.EventTaskMessage,
				WorkspaceID: workspaceID,
				ActorType:   "system",
				ActorID:     "",
				Payload: protocol.TaskMessagePayload{
					TaskID:         util.UUIDToString(taskID),
					ChatSessionID:  sessionIDStr,
					Seq:            seq,
					Type:           "text",
					Content:        chunk,
				},
			})
			time.Sleep(30 * time.Millisecond)
		}

		// Save the full message
		contentJSON, _ := json.Marshal(map[string]any{
			"type":    "text",
			"content": demoResponse,
		})
		s.Queries.CreateTaskMessage(execCtx, db.CreateTaskMessageParams{
			TaskID:  taskID,
			Seq:     0,
			Type:    "text",
			Content: pgtype.Text{String: string(contentJSON), Valid: true},
		})

		resultJSON, _ := json.Marshal(protocol.TaskCompletedPayload{
			Output: demoResponse,
		})
		s.CompleteTask(execCtx, taskID, resultJSON, "", "")

		stream.Global.Broadcast(sessionIDStr, stream.Event{
			Type:    "done",
			Content: demoResponse,
		})
	}()

	slog.Info("demo chat task started", "task_id", util.UUIDToString(task.ID), "chat_session_id", util.UUIDToString(chatSession.ID))
	return task, nil
}

func generateDemoResponse(agentName, userMessage string) string {
	lowerMsg := strings.ToLower(userMessage)

	if strings.Contains(lowerMsg, "hello") || strings.Contains(lowerMsg, "hi") || strings.Contains(lowerMsg, "introduce") || strings.Contains(lowerMsg, "who are you") || strings.Contains(lowerMsg, "bonjour") {
		return fmt.Sprintf(`# 👋 Hello! I'm %s

I'm a **managed AI agent** running on the Multica platform. Here's what I can help with:

- 🔧 **Code Analysis** — Review, debug, and suggest improvements
- 📋 **Issue Management** — Create, update, and track issues
- 🏗️ **Architecture** — Design systems and plan implementations
- 💬 **Technical Guidance** — Answer questions and provide best practices

## Current Status
I'm running in **demo mode** — the full AI engine (Claude Sonnet) will be activated once the administrator configures the API key. In demo mode I can still show you around the platform!

Try asking me:
- "What issues are open?"
- "Help me plan a feature"
- "Show me the workspace status"`, agentName)
	}

	if strings.Contains(lowerMsg, "issue") || strings.Contains(lowerMsg, "task") || strings.Contains(lowerMsg, "board") {
		return `# 📋 Workspace Issues

Here's a summary of the current workspace activity:

| Status | Count | Description |
|--------|-------|-------------|
| **Backlog** | 2 | Dark mode, performance optimization |
| **Todo** | 3 | File uploads, agent skills, rate limiting |
| **In Progress** | 3 | CI/CD pipeline, task engine, error handling |
| **Done** | 3 | Authentication, WebSocket, issue board, settings |

The workspace is actively being developed. Key areas in progress:
1. **Agent task execution engine** — Cloud execution backend (this is me! 🤖)
2. **CI/CD pipeline** — Automated testing and deployment
3. **Error handling** — Structured responses and monitoring

*In full mode, I can create, update, and manage these issues directly.*`
	}

	if strings.Contains(lowerMsg, "status") || strings.Contains(lowerMsg, "health") {
		return fmt.Sprintf(`# 🟢 Platform Status

| Component | Status |
|-----------|--------|
| **Backend API** | ✅ Online |
| **Database** | ✅ Connected |
| **WebSocket** | ✅ Active |
| **Agent (%s)** | ⚡ Demo Mode |
| **AI Engine** | ⏳ Awaiting API Key |

The platform infrastructure is fully operational. Once the AI engine is activated, I'll be able to:
- Execute tasks autonomously
- Analyze code and suggest improvements
- Manage issues based on conversations
- Stream responses in real-time`, agentName)
	}

	if strings.Contains(lowerMsg, "plan") || strings.Contains(lowerMsg, "feature") || strings.Contains(lowerMsg, "implement") {
		return `# 🏗️ Feature Planning

I can help you plan features! Here's my typical workflow:

1. **Understand** — I'll ask clarifying questions about the requirement
2. **Design** — Propose architecture and implementation approach
3. **Break Down** — Create sub-issues for each implementation step
4. **Track** — Monitor progress and update issue statuses

### Example: Planning a new feature
` + "```" + `
You: "I need a notification system"
Me: I'll analyze the codebase and create:
  - MYW-15: Design notification data model
  - MYW-16: Implement WebSocket notification channel
  - MYW-17: Build notification UI components
  - MYW-18: Add notification preferences
` + "```" + `

*In full mode, I'll actually create these issues and start working on them!*`
	}

	// Default response for any other message
	return fmt.Sprintf(`Thanks for your message! I'm **%s**, running in demo mode on the Multica platform.

I received: *"%s"*

In **full mode** (once the AI engine is activated), I would:
1. Analyze your request using Claude Sonnet
2. Take action — create issues, write code, or provide detailed analysis
3. Stream my response in real-time as I work

The platform is fully functional — the agent execution pipeline, WebSocket streaming, issue management, and chat UI are all working. The AI engine just needs an API key to start generating intelligent responses.

**Try these commands:**
- "Hello" — Meet me and see my capabilities
- "Show issues" — View workspace activity
- "Platform status" — Check system health
- "Plan a feature" — See how I approach tasks`, agentName, userMessage)
}

// splitIntoChunks breaks text into chunks of roughly n words each.
func splitIntoChunks(text string, wordsPerChunk int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	var chunks []string
	for i := 0; i < len(words); i += wordsPerChunk {
		end := i + wordsPerChunk
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.Join(words[i:end], " ")
		if i+wordsPerChunk < len(words) {
			chunk += " "
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// forceTaskRunning transitions a task directly from queued to running.
// Used by cloud/demo execution which bypasses the normal dispatch flow.
func (s *TaskService) forceTaskRunning(ctx context.Context, taskID pgtype.UUID) {
	if err := s.Queries.ForceTaskRunning(ctx, taskID); err != nil {
		slog.Warn("forceTaskRunning failed", "task_id", util.UUIDToString(taskID), "error", err)
	}
}

// CancelTasksForIssue cancels all active tasks for an issue.
func (s *TaskService) CancelTasksForIssue(ctx context.Context, issueID pgtype.UUID) error {
	return s.Queries.CancelAgentTasksByIssue(ctx, issueID)
}

// CancelTask cancels a single task by ID. It broadcasts a task:cancelled event
// so frontends can update immediately.
func (s *TaskService) CancelTask(ctx context.Context, taskID pgtype.UUID) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.CancelAgentTask(ctx, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := s.Queries.GetAgentTask(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("cancel task: %w", err)
		}
		return &existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cancel task: %w", err)
	}

	slog.Info("task cancelled", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID))

	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, task.AgentID)

	// Broadcast cancellation as a task:failed event so frontends clear the live card
	s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)

	return &task, nil
}

// ClaimTask atomically claims the next queued task for an agent,
// respecting max_concurrent_tasks.
func (s *TaskService) ClaimTask(ctx context.Context, agentID pgtype.UUID) (*db.AgentTaskQueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}

	running, err := s.Queries.CountRunningTasks(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("count running tasks: %w", err)
	}
	if running >= int64(agent.MaxConcurrentTasks) {
		slog.Debug("task claim: no capacity", "agent_id", util.UUIDToString(agentID), "running", running, "max", agent.MaxConcurrentTasks)
		return nil, nil // No capacity
	}

	task, err := s.Queries.ClaimAgentTask(ctx, agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Debug("task claim: no tasks available", "agent_id", util.UUIDToString(agentID))
			return nil, nil // No tasks available
		}
		return nil, fmt.Errorf("claim task: %w", err)
	}

	slog.Info("task claimed", "task_id", util.UUIDToString(task.ID), "agent_id", util.UUIDToString(agentID))

	// Update agent status to working
	s.updateAgentStatus(ctx, agentID, "working")

	// Broadcast task:dispatch
	s.broadcastTaskDispatch(ctx, task)

	return &task, nil
}

// ClaimTaskForRuntime claims the next runnable task for a runtime while
// still respecting each agent's max_concurrent_tasks limit.
func (s *TaskService) ClaimTaskForRuntime(ctx context.Context, runtimeID pgtype.UUID) (*db.AgentTaskQueue, error) {
	tasks, err := s.Queries.ListPendingTasksByRuntime(ctx, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("list pending tasks: %w", err)
	}

	triedAgents := map[string]struct{}{}
	for _, candidate := range tasks {
		agentKey := util.UUIDToString(candidate.AgentID)
		if _, seen := triedAgents[agentKey]; seen {
			continue
		}
		triedAgents[agentKey] = struct{}{}

		task, err := s.ClaimTask(ctx, candidate.AgentID)
		if err != nil {
			return nil, err
		}
		if task != nil && task.RuntimeID == runtimeID {
			return task, nil
		}
	}

	return nil, nil
}

// StartTask transitions a dispatched task to running.
// Issue status is NOT changed here — the agent manages it via the CLI.
func (s *TaskService) StartTask(ctx context.Context, taskID pgtype.UUID) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.StartAgentTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("start task: %w", err)
	}

	slog.Info("task started", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID))
	return &task, nil
}

// CompleteTask marks a task as completed.
// Issue status is NOT changed here — the agent manages it via the CLI.
func (s *TaskService) CompleteTask(ctx context.Context, taskID pgtype.UUID, result []byte, sessionID, workDir string) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
		ID:        taskID,
		Result:    result,
		SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""},
		WorkDir:   pgtype.Text{String: workDir, Valid: workDir != ""},
	})
	if err != nil {
		// Log the current task state to help debug why the update matched no rows.
		if existing, lookupErr := s.Queries.GetAgentTask(ctx, taskID); lookupErr == nil {
			slog.Warn("complete task failed: task not in running state",
				"task_id", util.UUIDToString(taskID),
				"current_status", existing.Status,
				"issue_id", util.UUIDToString(existing.IssueID),
				"agent_id", util.UUIDToString(existing.AgentID),
			)
		} else {
			slog.Warn("complete task failed: task not found",
				"task_id", util.UUIDToString(taskID),
				"lookup_error", lookupErr,
			)
		}
		return nil, fmt.Errorf("complete task: %w", err)
	}

	slog.Info("task completed", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID))

	// Post agent output as a comment, but only for assignment-triggered issue tasks
	// where the agent did NOT already post a comment during execution.
	// Comment-triggered tasks: the agent replies via CLI with --parent, so
	// posting here would create a duplicate.
	// Chat tasks: no comment posting needed.
	if task.IssueID.Valid && !task.TriggerCommentID.Valid {
		agentCommented, _ := s.Queries.HasAgentCommentedSince(ctx, db.HasAgentCommentedSinceParams{
			IssueID:  task.IssueID,
			AuthorID: task.AgentID,
			Since:    task.StartedAt,
		})
		if !agentCommented {
			var payload protocol.TaskCompletedPayload
			if err := json.Unmarshal(result, &payload); err == nil {
				if payload.Output != "" {
					s.createAgentComment(ctx, task.IssueID, task.AgentID, redact.Text(payload.Output), "comment", task.TriggerCommentID)
				}
			}
		}
	}

	// For chat tasks, save assistant reply, update session, and broadcast chat:done.
	if task.ChatSessionID.Valid {
		var payload protocol.TaskCompletedPayload
		if err := json.Unmarshal(result, &payload); err == nil && payload.Output != "" {
			if _, err := s.Queries.CreateChatMessage(ctx, db.CreateChatMessageParams{
				ChatSessionID: task.ChatSessionID,
				Role:          "assistant",
				Content:       redact.Text(payload.Output),
				TaskID:        task.ID,
			}); err != nil {
				slog.Error("failed to save assistant chat message", "task_id", util.UUIDToString(task.ID), "error", err)
			} else {
				// Event-driven unread: stamp unread_since on the first unread
				// assistant message. No-op if the session already has unread.
				// If the user is actively viewing the session, the frontend's
				// auto-mark-read effect will clear this within a tick.
				if err := s.Queries.SetUnreadSinceIfNull(ctx, task.ChatSessionID); err != nil {
					slog.Warn("failed to set unread_since", "chat_session_id", util.UUIDToString(task.ChatSessionID), "error", err)
				}
			}
		}
		s.Queries.UpdateChatSessionSession(ctx, db.UpdateChatSessionSessionParams{
			ID:        task.ChatSessionID,
			SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""},
			WorkDir:   pgtype.Text{String: workDir, Valid: workDir != ""},
		})
		s.broadcastChatDone(ctx, task)
	}

	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, task.AgentID)

	// Broadcast
	s.broadcastTaskEvent(ctx, protocol.EventTaskCompleted, task)

	return &task, nil
}

// FailTask marks a task as failed.
// Issue status is NOT changed here — the agent manages it via the CLI.
func (s *TaskService) FailTask(ctx context.Context, taskID pgtype.UUID, errMsg string) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.FailAgentTask(ctx, db.FailAgentTaskParams{
		ID:    taskID,
		Error: pgtype.Text{String: errMsg, Valid: true},
	})
	if err != nil {
		if existing, lookupErr := s.Queries.GetAgentTask(ctx, taskID); lookupErr == nil {
			slog.Warn("fail task failed: task not in dispatched/running state",
				"task_id", util.UUIDToString(taskID),
				"current_status", existing.Status,
				"issue_id", util.UUIDToString(existing.IssueID),
				"agent_id", util.UUIDToString(existing.AgentID),
			)
		} else {
			slog.Warn("fail task failed: task not found",
				"task_id", util.UUIDToString(taskID),
				"lookup_error", lookupErr,
			)
		}
		return nil, fmt.Errorf("fail task: %w", err)
	}

	slog.Warn("task failed", "task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(task.IssueID), "error", errMsg)

	if errMsg != "" && task.IssueID.Valid {
		s.createAgentComment(ctx, task.IssueID, task.AgentID, redact.Text(errMsg), "system", task.TriggerCommentID)
	}
	// Reconcile agent status
	s.ReconcileAgentStatus(ctx, task.AgentID)

	// Broadcast
	s.broadcastTaskEvent(ctx, protocol.EventTaskFailed, task)

	return &task, nil
}

// ReportProgress broadcasts a progress update via the event bus.
func (s *TaskService) ReportProgress(ctx context.Context, taskID string, workspaceID string, summary string, step, total int) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskProgress,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: protocol.TaskProgressPayload{
			TaskID:  taskID,
			Summary: summary,
			Step:    step,
			Total:   total,
		},
	})
}

// ReconcileAgentStatus checks running task count and sets agent status accordingly.
func (s *TaskService) ReconcileAgentStatus(ctx context.Context, agentID pgtype.UUID) {
	running, err := s.Queries.CountRunningTasks(ctx, agentID)
	if err != nil {
		return
	}
	newStatus := "idle"
	if running > 0 {
		newStatus = "working"
	}
	slog.Debug("agent status reconciled", "agent_id", util.UUIDToString(agentID), "status", newStatus, "running_tasks", running)
	s.updateAgentStatus(ctx, agentID, newStatus)
}

func (s *TaskService) updateAgentStatus(ctx context.Context, agentID pgtype.UUID, status string) {
	agent, err := s.Queries.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     agentID,
		Status: status,
	})
	if err != nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: util.UUIDToString(agent.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload:     map[string]any{"agent": agentToMap(agent)},
	})
}

// LoadAgentSkills loads an agent's skills with their files for task execution.
func (s *TaskService) LoadAgentSkills(ctx context.Context, agentID pgtype.UUID) []AgentSkillData {
	skills, err := s.Queries.ListAgentSkills(ctx, agentID)
	if err != nil || len(skills) == 0 {
		return nil
	}

	result := make([]AgentSkillData, 0, len(skills))
	for _, sk := range skills {
		data := AgentSkillData{Name: sk.Name, Content: sk.Content}
		files, _ := s.Queries.ListSkillFiles(ctx, sk.ID)
		for _, f := range files {
			data.Files = append(data.Files, AgentSkillFileData{Path: f.Path, Content: f.Content})
		}
		result = append(result, data)
	}
	return result
}

// AgentSkillData represents a skill for task execution responses.
type AgentSkillData struct {
	Name    string               `json:"name"`
	Content string               `json:"content"`
	Files   []AgentSkillFileData `json:"files,omitempty"`
}

// AgentSkillFileData represents a supporting file within a skill.
type AgentSkillFileData struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func priorityToInt(p string) int32 {
	switch p {
	case "urgent":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func (s *TaskService) broadcastTaskDispatch(ctx context.Context, task db.AgentTaskQueue) {
	var payload map[string]any
	if task.Context != nil {
		json.Unmarshal(task.Context, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["task_id"] = util.UUIDToString(task.ID)
	payload["runtime_id"] = util.UUIDToString(task.RuntimeID)
	payload["issue_id"] = util.UUIDToString(task.IssueID)
	payload["agent_id"] = util.UUIDToString(task.AgentID)

	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskDispatch,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     payload,
	})
}

func (s *TaskService) broadcastTaskEvent(ctx context.Context, eventType string, task db.AgentTaskQueue) {
	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return
	}
	payload := map[string]any{
		"task_id":  util.UUIDToString(task.ID),
		"agent_id": util.UUIDToString(task.AgentID),
		"issue_id": util.UUIDToString(task.IssueID),
		"status":   task.Status,
	}
	if task.ChatSessionID.Valid {
		payload["chat_session_id"] = util.UUIDToString(task.ChatSessionID)
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     payload,
	})
}

// ResolveTaskWorkspaceID determines the workspace ID for a task.
// For issue tasks, it comes from the issue. For chat tasks, from the chat session.
// For autopilot tasks, from the autopilot via its run.
// Returns "" when none of the links resolve — callers treat that as "not found".
func (s *TaskService) ResolveTaskWorkspaceID(ctx context.Context, task db.AgentTaskQueue) string {
	if task.IssueID.Valid {
		if issue, err := s.Queries.GetIssue(ctx, task.IssueID); err == nil {
			return util.UUIDToString(issue.WorkspaceID)
		}
	}
	if task.ChatSessionID.Valid {
		if cs, err := s.Queries.GetChatSession(ctx, task.ChatSessionID); err == nil {
			return util.UUIDToString(cs.WorkspaceID)
		}
	}
	if task.AutopilotRunID.Valid {
		if run, err := s.Queries.GetAutopilotRun(ctx, task.AutopilotRunID); err == nil {
			if ap, err := s.Queries.GetAutopilot(ctx, run.AutopilotID); err == nil {
				return util.UUIDToString(ap.WorkspaceID)
			}
		}
	}
	return ""
}

func (s *TaskService) broadcastChatDone(ctx context.Context, task db.AgentTaskQueue) {
	workspaceID := s.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventChatDone,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: protocol.ChatDonePayload{
			ChatSessionID: util.UUIDToString(task.ChatSessionID),
			TaskID:        util.UUIDToString(task.ID),
		},
	})
}

func (s *TaskService) broadcastIssueUpdated(issue db.Issue) {
	prefix := s.getIssuePrefix(issue.WorkspaceID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "system",
		ActorID:     "",
		Payload:     map[string]any{"issue": issueToMap(issue, prefix)},
	})
}

func (s *TaskService) getIssuePrefix(workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}

func (s *TaskService) createAgentComment(ctx context.Context, issueID, agentID pgtype.UUID, content, commentType string, parentID pgtype.UUID) {
	if content == "" {
		return
	}
	// Look up issue to get workspace ID for mention expansion and broadcasting.
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	// Resolve thread root: if parentID points to a reply (has its own parent),
	// use that parent instead so the comment lands in the top-level thread.
	if parentID.Valid {
		if parent, err := s.Queries.GetComment(ctx, parentID); err == nil && parent.ParentID.Valid {
			parentID = parent.ParentID
		}
	}
	// Expand bare issue identifiers (e.g. MUL-117) into mention links.
	content = mention.ExpandIssueIdentifiers(ctx, s.Queries, issue.WorkspaceID, content)
	comment, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issueID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    agentID,
		Content:     content,
		Type:        commentType,
		ParentID:    parentID,
	})
	if err != nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(agentID),
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          util.UUIDToString(comment.ID),
				"issue_id":    util.UUIDToString(comment.IssueID),
				"author_type": comment.AuthorType,
				"author_id":   util.UUIDToString(comment.AuthorID),
				"content":     comment.Content,
				"type":        comment.Type,
				"parent_id":   util.UUIDToPtr(comment.ParentID),
				"created_at":  comment.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
			},
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		},
	})
}

func issueToMap(issue db.Issue, issuePrefix string) map[string]any {
	return map[string]any{
		"id":              util.UUIDToString(issue.ID),
		"workspace_id":    util.UUIDToString(issue.WorkspaceID),
		"number":          issue.Number,
		"identifier":      issuePrefix + "-" + strconv.Itoa(int(issue.Number)),
		"title":           issue.Title,
		"description":     util.TextToPtr(issue.Description),
		"status":          issue.Status,
		"priority":        issue.Priority,
		"assignee_type":   util.TextToPtr(issue.AssigneeType),
		"assignee_id":     util.UUIDToPtr(issue.AssigneeID),
		"creator_type":    issue.CreatorType,
		"creator_id":      util.UUIDToString(issue.CreatorID),
		"parent_issue_id": util.UUIDToPtr(issue.ParentIssueID),
		"position":        issue.Position,
		"due_date":        util.TimestampToPtr(issue.DueDate),
		"created_at":      util.TimestampToString(issue.CreatedAt),
		"updated_at":      util.TimestampToString(issue.UpdatedAt),
	}
}

// agentToMap builds a simple map for broadcasting agent status updates.
func agentToMap(a db.Agent) map[string]any {
	var rc any
	if a.RuntimeConfig != nil {
		json.Unmarshal(a.RuntimeConfig, &rc)
	}
	return map[string]any{
		"id":                   util.UUIDToString(a.ID),
		"workspace_id":         util.UUIDToString(a.WorkspaceID),
		"runtime_id":           util.UUIDToString(a.RuntimeID),
		"name":                 a.Name,
		"description":          a.Description,
		"avatar_url":           util.TextToPtr(a.AvatarUrl),
		"runtime_mode":         a.RuntimeMode,
		"runtime_config":       rc,
		"visibility":           a.Visibility,
		"status":               a.Status,
		"max_concurrent_tasks": a.MaxConcurrentTasks,
		"owner_id":             util.UUIDToPtr(a.OwnerID),
		"skills":               []any{},
		"created_at":           util.TimestampToString(a.CreatedAt),
		"updated_at":           util.TimestampToString(a.UpdatedAt),
		"archived_at":          util.TimestampToPtr(a.ArchivedAt),
		"archived_by":          util.UUIDToPtr(a.ArchivedBy),
	}
}
