// Package executor provides server-side tool execution for the cloud-claude
// backend. When Claude returns tool_use blocks, this package handles executing
// them and returning tool_result messages, enabling a full agentic loop
// without requiring a local daemon.
package executor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	CallID  string
	Output  string
	IsError bool
}

// Executor runs tools server-side on behalf of a managed agent session.
type Executor struct {
	Queries     *db.Queries
	WorkspaceID pgtype.UUID
	SessionID   pgtype.UUID
	Logger      *slog.Logger
	WorkDir     string
	BashTimeout time.Duration
}

// NewExecutor creates a tool executor bound to a workspace and session.
func NewExecutor(q *db.Queries, workspaceID, sessionID pgtype.UUID, logger *slog.Logger) *Executor {
	return &Executor{
		Queries:     q,
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Logger:      logger,
		WorkDir:     "/tmp",
		BashTimeout: 30 * time.Second,
	}
}

// Execute runs a tool and returns the result.
func (e *Executor) Execute(ctx context.Context, toolName, callID string, input map[string]any) ToolResult {
	e.Logger.Info("executing tool", "tool", toolName, "call_id", callID)

	var output string
	var isError bool

	switch toolName {
	case "bash":
		output, isError = e.execBash(ctx, input)
	case "create_issue":
		output, isError = e.execCreateIssue(ctx, input)
	case "list_issues":
		output, isError = e.execListIssues(ctx, input)
	case "update_issue":
		output, isError = e.execUpdateIssue(ctx, input)
	case "add_comment":
		output, isError = e.execAddComment(ctx, input)
	case "search_code", "grep":
		output, isError = e.execSearchCode(ctx, input)
	case "read_file", "read":
		output, isError = e.execReadFile(ctx, input)
	case "write_file", "write":
		output, isError = e.execWriteFile(ctx, input)
	case "edit":
		output, isError = e.execEditFile(ctx, input)
	case "list_directory", "glob":
		output, isError = e.execListDirectory(ctx, input)
	case "send_email":
		output, isError = e.execSendEmail(ctx, input)
	case "http_request", "web_fetch":
		output, isError = e.execHTTPRequest(ctx, input)
	case "web_search":
		output, isError = e.execWebSearch(ctx, input)
	case "memory_read":
		output, isError = e.execMemoryRead(ctx, input)
	case "memory_write":
		output, isError = e.execMemoryWrite(ctx, input)
	case "delegate_to_agent":
		output, isError = e.execDelegateToAgent(ctx, input)
	default:
		output = fmt.Sprintf("Unknown tool: %s", toolName)
		isError = true
	}

	return ToolResult{
		CallID:  callID,
		Output:  output,
		IsError: isError,
	}
}

// ---------------------------------------------------------------------------
// Tool: bash
// ---------------------------------------------------------------------------

func (e *Executor) execBash(ctx context.Context, input map[string]any) (string, bool) {
	command, _ := input["command"].(string)
	if command == "" {
		return "command is required", true
	}

	if containsDangerousCommand(command) {
		return "command blocked for security: destructive operations not allowed in cloud sandbox", true
	}

	timeout := e.BashTimeout
	if t, ok := input["timeout"].(float64); ok && t > 0 && t <= 120 {
		timeout = time.Duration(t) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	cmd.Dir = e.WorkDir
	cmd.Env = []string{
		"HOME=/tmp",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"TERM=xterm",
		"LANG=en_US.UTF-8",
	}

	out, err := cmd.CombinedOutput()
	result := string(out)

	if len(result) > 50000 {
		result = result[:50000] + "\n... (output truncated at 50KB)"
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("command timed out after %v:\n%s", timeout, result), true
		}
		return fmt.Sprintf("exit code %s:\n%s", err.Error(), result), false
	}

	return result, false
}

func containsDangerousCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	dangerous := []string{
		"rm -rf /", "mkfs", "dd if=", ":(){:|:&};:",
		"chmod -R 777 /", "shutdown", "reboot", "halt",
		"init 0", "init 6", "> /dev/sda",
		"curl | sh", "wget | sh", "curl | bash", "wget | bash",
	}
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Tool: create_issue
// ---------------------------------------------------------------------------

func (e *Executor) execCreateIssue(ctx context.Context, input map[string]any) (string, bool) {
	title, _ := input["title"].(string)
	if title == "" {
		return "title is required", true
	}
	description, _ := input["description"].(string)
	priority, _ := input["priority"].(string)
	if priority == "" {
		priority = "medium"
	}
	status, _ := input["status"].(string)
	if status == "" {
		status = "todo"
	}

	ws, err := e.Queries.GetWorkspace(ctx, e.WorkspaceID)
	if err != nil {
		return "failed to get workspace: " + err.Error(), true
	}

	counter, err := e.Queries.IncrementIssueCounter(ctx, e.WorkspaceID)
	if err != nil {
		return "failed to increment issue counter: " + err.Error(), true
	}

	identifier := fmt.Sprintf("%s-%d", ws.IssuePrefix, counter)

	issue, err := e.Queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: e.WorkspaceID,
		Title:       title,
		Description: pgtype.Text{String: description, Valid: description != ""},
		Priority:    priority,
		Status:      status,
		CreatorType: "agent",
		Number:      counter,
	})
	if err != nil {
		return "failed to create issue: " + err.Error(), true
	}

	return fmt.Sprintf("Created issue %s: %s (id: %s)", identifier, title, util.UUIDToString(issue.ID)), false
}

// ---------------------------------------------------------------------------
// Tool: list_issues
// ---------------------------------------------------------------------------

func (e *Executor) execListIssues(ctx context.Context, input map[string]any) (string, bool) {
	limit := int32(20)
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int32(l)
	}
	if limit > 100 {
		limit = 100
	}

	statusFilter, _ := input["status"].(string)

	params := db.ListIssuesParams{
		WorkspaceID: e.WorkspaceID,
		Limit:       limit,
	}
	if statusFilter != "" {
		params.Status = pgtype.Text{String: statusFilter, Valid: true}
	}

	issues, err := e.Queries.ListIssues(ctx, params)
	if err != nil {
		return "failed to list issues: " + err.Error(), true
	}

	if len(issues) == 0 {
		return "No issues found.", false
	}

	// Get workspace prefix for display
	ws, _ := e.Queries.GetWorkspace(ctx, e.WorkspaceID)
	prefix := ws.IssuePrefix
	if prefix == "" {
		prefix = "ISS"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d issues:\n\n", len(issues)))
	for _, iss := range issues {
		sb.WriteString(fmt.Sprintf("- %s-%d [%s] %s (priority: %s)\n",
			prefix, iss.Number, iss.Status, iss.Title, iss.Priority))
	}
	return sb.String(), false
}

// ---------------------------------------------------------------------------
// Tool: update_issue
// ---------------------------------------------------------------------------

func (e *Executor) execUpdateIssue(ctx context.Context, input map[string]any) (string, bool) {
	issueID, _ := input["issue_id"].(string)
	if issueID == "" {
		return "issue_id is required", true
	}

	// Try to parse as PREFIX-NUMBER or just UUID
	var issue db.Issue
	var err error

	// Try as "PREFIX-123" format
	if parts := strings.SplitN(issueID, "-", 2); len(parts) == 2 {
		if num, parseErr := strconv.Atoi(parts[1]); parseErr == nil {
			issue, err = e.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
				WorkspaceID: e.WorkspaceID,
				Number:      int32(num),
			})
		}
	}

	if err != nil || issue.ID == (pgtype.UUID{}) {
		// Fallback: try as UUID
		issue, err = e.Queries.GetIssue(ctx, util.ParseUUID(issueID))
		if err != nil {
			return "issue not found: " + issueID, true
		}
	}

	params := db.UpdateIssueParams{
		ID:    issue.ID,
		Title: pgtype.Text{String: issue.Title, Valid: true},
	}

	if t, ok := input["title"].(string); ok && t != "" {
		params.Title = pgtype.Text{String: t, Valid: true}
	}
	if s, ok := input["status"].(string); ok && s != "" {
		params.Status = pgtype.Text{String: s, Valid: true}
	}
	if p, ok := input["priority"].(string); ok && p != "" {
		params.Priority = pgtype.Text{String: p, Valid: true}
	}
	if d, ok := input["description"].(string); ok {
		params.Description = pgtype.Text{String: d, Valid: true}
	}

	_, err = e.Queries.UpdateIssue(ctx, params)
	if err != nil {
		return "failed to update issue: " + err.Error(), true
	}

	return fmt.Sprintf("Updated issue %s successfully.", issueID), false
}

// ---------------------------------------------------------------------------
// Tool: add_comment
// ---------------------------------------------------------------------------

func (e *Executor) execAddComment(ctx context.Context, input map[string]any) (string, bool) {
	issueID, _ := input["issue_id"].(string)
	content, _ := input["content"].(string)
	if issueID == "" || content == "" {
		return "issue_id and content are required", true
	}

	var issue db.Issue
	var err error

	// Try as "PREFIX-123" format
	if parts := strings.SplitN(issueID, "-", 2); len(parts) == 2 {
		if num, parseErr := strconv.Atoi(parts[1]); parseErr == nil {
			issue, err = e.Queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
				WorkspaceID: e.WorkspaceID,
				Number:      int32(num),
			})
		}
	}

	if err != nil || issue.ID == (pgtype.UUID{}) {
		issue, err = e.Queries.GetIssue(ctx, util.ParseUUID(issueID))
		if err != nil {
			return "issue not found: " + issueID, true
		}
	}

	comment, err := e.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: e.WorkspaceID,
		AuthorType:  "agent",
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		return "failed to create comment: " + err.Error(), true
	}

	return fmt.Sprintf("Comment added to %s (id: %s)", issueID, util.UUIDToString(comment.ID)), false
}

// ---------------------------------------------------------------------------
// Tool: search_code
// ---------------------------------------------------------------------------

func (e *Executor) execSearchCode(ctx context.Context, input map[string]any) (string, bool) {
	query, _ := input["query"].(string)
	if query == "" {
		return "query is required", true
	}
	pathFilter, _ := input["path"].(string)

	args := []string{"-rn", "--color=never", "--max-count=50"}
	if pathFilter != "" {
		args = append(args, "--include="+pathFilter)
	}
	args = append(args, query, ".")

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "grep", args...)
	cmd.Dir = e.WorkDir

	out, err := cmd.CombinedOutput()
	result := string(out)

	if len(result) > 30000 {
		result = result[:30000] + "\n... (results truncated)"
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found.", false
		}
		return result, false
	}

	return result, false
}

// ---------------------------------------------------------------------------
// Tool: read_file
// ---------------------------------------------------------------------------

func (e *Executor) execReadFile(_ context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	if path == "" {
		return "path is required", true
	}
	if strings.Contains(path, "..") {
		return "path traversal not allowed", true
	}

	fullPath := path
	if !strings.HasPrefix(path, "/") {
		fullPath = e.WorkDir + "/" + path
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "failed to read file: " + err.Error(), true
	}

	result := string(data)
	if len(result) > 100000 {
		result = result[:100000] + "\n... (file truncated at 100KB)"
	}

	return result, false
}

// ---------------------------------------------------------------------------
// Tool: write_file
// ---------------------------------------------------------------------------

func (e *Executor) execWriteFile(_ context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	if path == "" {
		return "path is required", true
	}
	if strings.Contains(path, "..") {
		return "path traversal not allowed", true
	}

	fullPath := path
	if !strings.HasPrefix(path, "/") {
		fullPath = e.WorkDir + "/" + path
	}

	// Create parent directories
	if idx := strings.LastIndex(fullPath, "/"); idx > 0 {
		os.MkdirAll(fullPath[:idx], 0o755)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "failed to write file: " + err.Error(), true
	}

	return fmt.Sprintf("File written: %s (%d bytes)", path, len(content)), false
}

// ---------------------------------------------------------------------------
// Tool: edit (Anthropic-compatible file edit with old_string/new_string)
// ---------------------------------------------------------------------------

func (e *Executor) execEditFile(_ context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	oldStr, _ := input["old_string"].(string)
	newStr, _ := input["new_string"].(string)
	if path == "" {
		return "path is required", true
	}
	if strings.Contains(path, "..") {
		return "path traversal not allowed", true
	}

	fullPath := path
	if !strings.HasPrefix(path, "/") {
		fullPath = e.WorkDir + "/" + path
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "failed to read file: " + err.Error(), true
	}

	content := string(data)
	if oldStr == "" {
		// Create new file with new_string content
		if err := os.WriteFile(fullPath, []byte(newStr), 0o644); err != nil {
			return "failed to write file: " + err.Error(), true
		}
		return fmt.Sprintf("File created: %s (%d bytes)", path, len(newStr)), false
	}

	count := strings.Count(content, oldStr)
	if count == 0 {
		return "old_string not found in file", true
	}
	if count > 1 {
		return fmt.Sprintf("old_string found %d times, must be unique", count), true
	}

	content = strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "failed to write file: " + err.Error(), true
	}

	return fmt.Sprintf("File edited: %s", path), false
}

// ---------------------------------------------------------------------------
// Tool: web_search (basic web search via DuckDuckGo HTML)
// ---------------------------------------------------------------------------

func (e *Executor) execWebSearch(ctx context.Context, input map[string]any) (string, bool) {
	query, _ := input["query"].(string)
	if query == "" {
		return "query is required", true
	}

	// Use DuckDuckGo lite as a simple search backend
	searchURL := "https://lite.duckduckgo.com/lite/?q=" + strings.ReplaceAll(query, " ", "+")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "failed to create request: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "Multica-Agent/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "search request failed: " + err.Error(), true
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return "failed to read response: " + err.Error(), true
	}

	return fmt.Sprintf("Search results for '%s':\n%s", query, string(body)), false
}

// ---------------------------------------------------------------------------
// Tool: list_directory
// ---------------------------------------------------------------------------

func (e *Executor) execListDirectory(_ context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}
	if strings.Contains(path, "..") {
		return "path traversal not allowed", true
	}

	fullPath := path
	if !strings.HasPrefix(path, "/") {
		fullPath = e.WorkDir + "/" + path
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return "failed to list directory: " + err.Error(), true
	}

	var sb strings.Builder
	for _, entry := range entries {
		info, _ := entry.Info()
		if info != nil {
			sb.WriteString(fmt.Sprintf("%s %8d %s %s\n",
				info.Mode(), info.Size(), info.ModTime().Format("Jan 02 15:04"), entry.Name()))
		}
	}
	return sb.String(), false
}

// ---------------------------------------------------------------------------
// Tool: send_email
// ---------------------------------------------------------------------------

func (e *Executor) execSendEmail(ctx context.Context, input map[string]any) (string, bool) {
	to, _ := input["to"].(string)
	subject, _ := input["subject"].(string)
	body, _ := input["body"].(string)

	if to == "" || subject == "" || body == "" {
		return "to, subject, and body are required", true
	}

	if !strings.Contains(to, "@") || !strings.Contains(to, ".") {
		return "invalid email address", true
	}

	e.Logger.Info("agent email request", "to", to, "subject", subject, "session_id", util.UUIDToString(e.SessionID))

	payload, _ := json.Marshal(map[string]string{"to": to, "subject": subject, "body": body})
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.email_sent",
		Payload:   payload,
	})

	return fmt.Sprintf("Email queued to %s with subject: %s", to, subject), false
}

// ---------------------------------------------------------------------------
// Tool: http_request
// ---------------------------------------------------------------------------

func (e *Executor) execHTTPRequest(ctx context.Context, input map[string]any) (string, bool) {
	urlStr, _ := input["url"].(string)
	method, _ := input["method"].(string)
	if urlStr == "" {
		return "url is required", true
	}
	if method == "" {
		method = "GET"
	}
	method = strings.ToUpper(method)

	if method != "GET" && method != "POST" {
		return "only GET and POST methods are allowed", true
	}

	lower := strings.ToLower(urlStr)
	if strings.Contains(lower, "localhost") || strings.Contains(lower, "127.0.0.1") ||
		strings.Contains(lower, "169.254") || strings.Contains(lower, "10.") ||
		strings.Contains(lower, "192.168") || strings.HasPrefix(lower, "http://172.") {
		return "access to internal network addresses is blocked", true
	}

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	args := []string{"-s", "-S", "--max-time", "10", "-X", method}
	if headers, ok := input["headers"].(map[string]any); ok {
		for k, v := range headers {
			args = append(args, "-H", fmt.Sprintf("%s: %s", k, v))
		}
	}
	if method == "POST" {
		if body, ok := input["body"].(string); ok {
			args = append(args, "-d", body)
		}
	}
	args = append(args, urlStr)

	cmd := exec.CommandContext(execCtx, "curl", args...)
	out, err := cmd.CombinedOutput()
	result := string(out)

	if len(result) > 50000 {
		result = result[:50000] + "\n... (response truncated)"
	}

	if err != nil {
		return "HTTP request failed: " + err.Error() + "\n" + result, true
	}

	return result, false
}

// ---------------------------------------------------------------------------
// Tool: memory_read
// ---------------------------------------------------------------------------

func (e *Executor) execMemoryRead(ctx context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	storeID, _ := input["store_id"].(string)
	if path == "" || storeID == "" {
		return "path and store_id are required", true
	}

	mem, err := e.Queries.GetMemoryByPath(ctx, db.GetMemoryByPathParams{
		StoreID: util.ParseUUID(storeID),
		Path:    path,
	})
	if err != nil {
		return "memory not found at path: " + path, true
	}

	return mem.Content, false
}

// ---------------------------------------------------------------------------
// Tool: memory_write
// ---------------------------------------------------------------------------

func (e *Executor) execMemoryWrite(ctx context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	storeID, _ := input["store_id"].(string)
	if path == "" || content == "" || storeID == "" {
		return "path, content, and store_id are required", true
	}

	storeUUID := util.ParseUUID(storeID)

	existing, err := e.Queries.GetMemoryByPath(ctx, db.GetMemoryByPathParams{
		StoreID: storeUUID,
		Path:    path,
	})

	if err == nil {
		_, err = e.Queries.UpdateMemory(ctx, db.UpdateMemoryParams{
			ID:               existing.ID,
			Content:          content,
			ContentSha256:    computeSha256(content),
			ContentSizeBytes: int32(len(content)),
		})
		if err != nil {
			return "failed to update memory: " + err.Error(), true
		}

		e.Queries.CreateMemoryVersion(ctx, db.CreateMemoryVersionParams{
			MemoryID:  existing.ID,
			StoreID:   storeUUID,
			Operation: "modified",
			Content:   pgtype.Text{String: content, Valid: true},
			Path:      path,
			SessionID: e.SessionID,
		})

		return fmt.Sprintf("Memory updated at %s (%d bytes)", path, len(content)), false
	}

	mem, err := e.Queries.CreateMemory(ctx, db.CreateMemoryParams{
		StoreID:          storeUUID,
		Path:             path,
		Content:          content,
		ContentSha256:    computeSha256(content),
		ContentSizeBytes: int32(len(content)),
	})
	if err != nil {
		return "failed to create memory: " + err.Error(), true
	}

	e.Queries.CreateMemoryVersion(ctx, db.CreateMemoryVersionParams{
		MemoryID:  mem.ID,
		StoreID:   storeUUID,
		Operation: "created",
		Content:   pgtype.Text{String: content, Valid: true},
		Path:      path,
		SessionID: e.SessionID,
	})

	return fmt.Sprintf("Memory created at %s (%d bytes)", path, len(content)), false
}

// ---------------------------------------------------------------------------
// Tool: delegate_to_agent
// ---------------------------------------------------------------------------

func (e *Executor) execDelegateToAgent(ctx context.Context, input map[string]any) (string, bool) {
	agentID, _ := input["agent_id"].(string)
	prompt, _ := input["prompt"].(string)
	if agentID == "" || prompt == "" {
		return "agent_id and prompt are required", true
	}

	targetAgent, err := e.Queries.GetManagedAgentInWorkspace(ctx, db.GetManagedAgentInWorkspaceParams{
		ID:          util.ParseUUID(agentID),
		WorkspaceID: e.WorkspaceID,
	})
	if err != nil {
		return "target agent not found: " + agentID, true
	}
	if targetAgent.ArchivedAt.Valid {
		return "target agent is archived", true
	}

	thread, err := e.Queries.CreateSessionThread(ctx, db.CreateSessionThreadParams{
		SessionID: e.SessionID,
		AgentID:   targetAgent.ID,
		AgentName: targetAgent.Name,
		Status:    "running",
	})
	if err != nil {
		return "failed to create sub-agent thread: " + err.Error(), true
	}

	e.Logger.Info("delegating to sub-agent",
		"parent_session", util.UUIDToString(e.SessionID),
		"target_agent", targetAgent.Name,
		"thread_id", util.UUIDToString(thread.ID),
	)

	payload, _ := json.Marshal(map[string]string{
		"agent_id":   agentID,
		"agent_name": targetAgent.Name,
		"prompt":     prompt,
		"thread_id":  util.UUIDToString(thread.ID),
	})
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		ThreadID:  thread.ID,
		Type:      "agent.delegation",
		Payload:   payload,
	})

	subResult, err := e.executeSubAgent(ctx, targetAgent, prompt, thread.ID)
	if err != nil {
		e.Queries.UpdateSessionThreadStatus(ctx, thread.ID, "failed")
		return fmt.Sprintf("Sub-agent %s failed: %s", targetAgent.Name, err.Error()), true
	}

	e.Queries.UpdateSessionThreadStatus(ctx, thread.ID, "completed")

	return fmt.Sprintf("[Sub-agent %s response]:\n%s", targetAgent.Name, subResult), false
}

func (e *Executor) executeSubAgent(ctx context.Context, agentRow db.ManagedAgent, prompt string, threadID pgtype.UUID) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not configured for sub-agent execution")
	}

	subCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	systemPrompt := agentRow.SystemPrompt.String
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are %s. %s\nComplete the delegated task thoroughly and return a clear result.", agentRow.Name, agentRow.Description.String)
	}

	backend := agent.NewCloudClaude(apiKey, e.Logger)

	model := ""
	if agentRow.Model != nil {
		var m struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(agentRow.Model, &m) == nil {
			model = m.ID
		}
	}

	session, err := backend.Execute(subCtx, prompt, agent.ExecOptions{
		Model:        model,
		SystemPrompt: systemPrompt,
		Timeout:      3 * time.Minute,
	})
	if err != nil {
		return "", err
	}

	var output strings.Builder
	for msg := range session.Messages {
		if msg.Type == agent.MessageText {
			output.WriteString(msg.Content)
		}
		payload, _ := json.Marshal(map[string]any{
			"type":    string(msg.Type),
			"content": msg.Content,
		})
		e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
			SessionID: e.SessionID,
			ThreadID:  threadID,
			Type:      "sub_agent." + string(msg.Type),
			Payload:   payload,
		})
	}

	result := <-session.Result
	if result.Status == "failed" {
		return "", fmt.Errorf("%s", result.Error)
	}

	return output.String(), nil
}

func computeSha256(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
