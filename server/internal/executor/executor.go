// Package executor provides server-side tool execution for the cloud-claude
// backend. When Claude returns tool_use blocks, this package handles executing
// them and returning tool_result messages, enabling a full agentic loop
// without requiring a local daemon.
package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/sandbox"
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
	Sandbox     *sandbox.Sandbox
	BashTimeout time.Duration
	// Depth tracks delegation depth to prevent infinite sub-agent recursion.
	Depth int
	// McpExecute is set by the session service when MCP servers are connected.
	// It routes MCP tool calls to the appropriate server.
	McpExecute func(ctx context.Context, toolName string, args map[string]any) (string, error)
}

// NewExecutor creates a tool executor bound to a workspace and session.
// It creates an isolated sandbox for the session.
func NewExecutor(q *db.Queries, workspaceID, sessionID pgtype.UUID, logger *slog.Logger) *Executor {
	sid := util.UUIDToString(sessionID)
	sb, err := sandbox.New(sandbox.Config{SessionID: sid})
	if err != nil {
		logger.Error("failed to create sandbox, falling back to /tmp", "error", err)
		sb, _ = sandbox.New(sandbox.Config{SessionID: sid, BaseDir: "/tmp"})
	}
	return &Executor{
		Queries:     q,
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Logger:      logger,
		Sandbox:     sb,
		BashTimeout: 30 * time.Second,
	}
}

// Close releases sandbox resources. Should be called when the session ends.
func (e *Executor) Close() {
	if e.Sandbox != nil {
		e.Sandbox.Close()
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
	case "browse_page":
		output, isError = e.execBrowsePage(ctx, input)
	case "download_file":
		output, isError = e.execDownloadFile(ctx, input)
	case "screenshot_page":
		output, isError = e.execScreenshotPage(ctx, input)
	case "plan_task":
		output, isError = e.execPlanTask(ctx, input)
	case "extract_links":
		output, isError = e.execExtractLinks(ctx, input)
	case "fill_form":
		output, isError = e.execFillForm(ctx, input)
	default:
		// Try MCP executor if available
		if e.McpExecute != nil {
			result, err := e.McpExecute(ctx, toolName, input)
			if err != nil {
				return ToolResult{CallID: callID, Output: "MCP tool error: " + err.Error(), IsError: true}
			}
			return ToolResult{CallID: callID, Output: result}
		}
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

	timeout := e.BashTimeout
	if t, ok := input["timeout"].(float64); ok && t > 0 && t <= 120 {
		timeout = time.Duration(t) * time.Second
	}

	result := e.Sandbox.Exec(ctx, command, timeout)
	return result.Output, result.IsError
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

	// Sanitize pathFilter to prevent shell injection
	// Only allow safe glob characters: alphanumeric, *, ?, ., /, -, _
	if pathFilter != "" {
		safePattern := regexp.MustCompile(`^[a-zA-Z0-9*?._/\-]+$`)
		if !safePattern.MatchString(pathFilter) {
			return "invalid path filter: only alphanumeric, *, ?, ., /, -, _ are allowed", true
		}
	}

	// Build grep command and run in sandbox
	args := "-rn --color=never --max-count=50"
	if pathFilter != "" {
		args += fmt.Sprintf(" --include=%q", pathFilter)
	}
	cmd := fmt.Sprintf("grep %s %q .", args, query)
	result := e.Sandbox.Exec(ctx, cmd, 15*time.Second)
	if result.ExitCode == 1 && result.Output == "" {
		return "No matches found.", false
	}
	return result.Output, false
}

// ---------------------------------------------------------------------------
// Tool: read_file
// ---------------------------------------------------------------------------

func (e *Executor) execReadFile(_ context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	if path == "" {
		return "path is required", true
	}

	data, err := e.Sandbox.ReadFile(path)
	if err != nil {
		return "failed to read file: " + err.Error(), true
	}

	if len(data) > 100000 {
		data = data[:100000] + "\n... (file truncated at 100KB)"
	}

	return data, false
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

	if err := e.Sandbox.WriteFile(path, content); err != nil {
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

	data, err := e.Sandbox.ReadFile(path)
	if err != nil {
		return "failed to read file: " + err.Error(), true
	}

	if oldStr == "" {
		// Create new file with new_string content
		if err := e.Sandbox.WriteFile(path, newStr); err != nil {
			return "failed to write file: " + err.Error(), true
		}
		return fmt.Sprintf("File created: %s (%d bytes)", path, len(newStr)), false
	}

	count := strings.Count(data, oldStr)
	if count == 0 {
		return "old_string not found in file", true
	}
	if count > 1 {
		return fmt.Sprintf("old_string found %d times, must be unique", count), true
	}

	content := strings.Replace(data, oldStr, newStr, 1)
	if err := e.Sandbox.WriteFile(path, content); err != nil {
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
	searchURL := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "failed to create request: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Aurion-Agent/2.0)")

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

	htmlStr := string(body)

	// Parse DuckDuckGo Lite results into structured format
	// DDG Lite uses <a> tags with class "result-link" and <td> for snippets
	linkRe := regexp.MustCompile(`(?is)<a[^>]+href\s*=\s*["']([^"']+)["'][^>]*class\s*=\s*["']result-link["'][^>]*>(.*?)</a>`)
	matches := linkRe.FindAllStringSubmatch(htmlStr, 20)

	if len(matches) == 0 {
		// Fallback: try generic link extraction
		linkRe2 := regexp.MustCompile(`(?is)<a[^>]+href\s*=\s*["'](https?://[^"']+)["'][^>]*>(.*?)</a>`)
		matches = linkRe2.FindAllStringSubmatch(htmlStr, 20)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for '%s':\n\n", query))

	if len(matches) == 0 {
		// Return text-extracted content as fallback
		text := extractTextFromHTML(htmlStr)
		if len(text) > 5000 {
			text = text[:5000]
		}
		sb.WriteString(text)
	} else {
		for i, m := range matches {
			if len(m) < 3 {
				continue
			}
			href := strings.TrimSpace(m[1])
			title := strings.TrimSpace(extractTextFromHTML(m[2]))
			if href == "" || strings.Contains(href, "duckduckgo.com") {
				continue
			}
			sb.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n\n", i+1, title, href))
		}
	}

	return sb.String(), false
}

// ---------------------------------------------------------------------------
// Tool: list_directory
// ---------------------------------------------------------------------------

func (e *Executor) execListDirectory(_ context.Context, input map[string]any) (string, bool) {
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}

	entries, err := e.Sandbox.ListDir(path)
	if err != nil {
		return "failed to list directory: " + err.Error(), true
	}

	return strings.Join(entries, "\n"), false
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

	// Try Resend API first (recommended for production)
	resendKey := os.Getenv("RESEND_API_KEY")
	if resendKey != "" {
		return e.sendViaResend(ctx, resendKey, to, subject, body)
	}

	// Try SMTP configuration
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASSWORD")
	smtpFrom := os.Getenv("SMTP_FROM")

	if smtpHost != "" && smtpUser != "" {
		if smtpPort == "" {
			smtpPort = "587"
		}
		if smtpFrom == "" {
			smtpFrom = smtpUser
		}
		return e.sendViaSMTP(ctx, smtpHost, smtpPort, smtpUser, smtpPass, smtpFrom, to, subject, body)
	}

	// Fallback: no email provider configured — return error
	payload, _ := json.Marshal(map[string]string{"to": to, "subject": subject, "body": body})
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.email_failed",
		Payload:   payload,
	})

	return fmt.Sprintf("Cannot send email: no email provider configured. Set RESEND_API_KEY or SMTP_HOST/SMTP_PORT/SMTP_USER/SMTP_PASSWORD environment variables to enable email delivery. Email to %s was NOT sent.", to), true
}

// sendViaResend sends email using the Resend API (https://resend.com).
func (e *Executor) sendViaResend(ctx context.Context, apiKey, to, subject, body string) (string, bool) {
	fromAddr := os.Getenv("RESEND_FROM")
	if fromAddr == "" {
		fromAddr = "agent@aurion.studio"
	}

	payload, _ := json.Marshal(map[string]any{
		"from":    fromAddr,
		"to":      []string{to},
		"subject": subject,
		"text":    body,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return "failed to create request: " + err.Error(), true
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "Resend API error: " + err.Error(), true
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 400 {
		return fmt.Sprintf("Resend API error (status %d): %s", resp.StatusCode, string(respBody)), true
	}

	// Log success event
	eventPayload, _ := json.Marshal(map[string]string{
		"to": to, "subject": subject, "provider": "resend", "status": "sent",
	})
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.email_sent",
		Payload:   eventPayload,
	})

	return fmt.Sprintf("Email sent successfully to %s via Resend (subject: %s)", to, subject), false
}

// sendViaSMTP sends email using SMTP with STARTTLS.
func (e *Executor) sendViaSMTP(_ context.Context, host, port, user, pass, from, to, subject, body string) (string, bool) {
	addr := net.JoinHostPort(host, port)

	// Build RFC 2822 email
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nDate: %s\r\n\r\n%s",
		from, to, mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z), body)

	auth := smtp.PlainAuth("", user, pass, host)

	// Use STARTTLS
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return "SMTP connection failed: " + err.Error(), true
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return "SMTP client error: " + err.Error(), true
	}
	defer c.Quit()

	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if err := c.StartTLS(tlsConfig); err != nil {
		// Continue without TLS if STARTTLS fails (some servers don't support it)
		e.Logger.Warn("SMTP STARTTLS failed, continuing without TLS", "error", err)
	}

	if err := c.Auth(auth); err != nil {
		return "SMTP auth failed: " + err.Error(), true
	}
	if err := c.Mail(from); err != nil {
		return "SMTP MAIL FROM failed: " + err.Error(), true
	}
	if err := c.Rcpt(to); err != nil {
		return "SMTP RCPT TO failed: " + err.Error(), true
	}

	w, err := c.Data()
	if err != nil {
		return "SMTP DATA failed: " + err.Error(), true
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return "SMTP write failed: " + err.Error(), true
	}
	if err := w.Close(); err != nil {
		return "SMTP close failed: " + err.Error(), true
	}

	// Log success event
	eventPayload, _ := json.Marshal(map[string]string{
		"to": to, "subject": subject, "provider": "smtp", "host": host, "status": "sent",
	})
	e.Queries.CreateSessionEvent(context.Background(), db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.email_sent",
		Payload:   eventPayload,
	})

	return fmt.Sprintf("Email sent successfully to %s via SMTP (subject: %s)", to, subject), false
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

	if isInternalURL(urlStr) {
		return "access to internal network addresses is blocked", true
	}

	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Use Go's http.Client instead of curl to avoid command injection
	var bodyReader io.Reader
	if method == "POST" {
		if body, ok := input["body"].(string); ok {
			bodyReader = strings.NewReader(body)
		}
	}

	req, err := http.NewRequestWithContext(execCtx, method, urlStr, bodyReader)
	if err != nil {
		return "failed to create request: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "Aurion-Agent/2.0")

	if headers, ok := input["headers"].(map[string]any); ok {
		for k, v := range headers {
			key := fmt.Sprintf("%s", k)
			val := fmt.Sprintf("%s", v)
			// Reject headers with newlines (header injection)
			if strings.ContainsAny(key, "\r\n") || strings.ContainsAny(val, "\r\n") {
				continue
			}
			req.Header.Set(key, val)
		}
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "HTTP request failed: " + err.Error(), true
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		return "failed to read response: " + err.Error(), true
	}

	result := string(respBody)
	if len(result) > 50000 {
		result = result[:50000] + "\n... (response truncated)"
	}

	return fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, resp.Status, result), false
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

	// Prevent infinite delegation chains
	const maxDelegationDepth = 3
	if e.Depth >= maxDelegationDepth {
		return fmt.Sprintf("delegation depth limit reached (%d). Cannot delegate further to prevent infinite recursion.", maxDelegationDepth), true
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

	subCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	systemPrompt := agentRow.SystemPrompt.String
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are %s. %s\nComplete the delegated task thoroughly and return a clear result.", agentRow.Name, agentRow.Description.String)
	}

	model := ""
	if agentRow.Model != nil {
		var m struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(agentRow.Model, &m) == nil {
			model = m.ID
		}
	}

	// Use agentic backend so sub-agents have access to tools (bash, read, write, web_fetch, etc.)
	subExecutor := NewExecutor(e.Queries, e.WorkspaceID, e.SessionID, e.Logger)
	subExecutor.Depth = e.Depth + 1 // propagate and increment depth for recursion limit
	defer subExecutor.Close()

	backend := agent.NewAgenticCloudClaude(apiKey, e.Logger, subExecutor, nil)

	session, err := backend.Execute(subCtx, prompt, agent.ExecOptions{
		Model:        model,
		SystemPrompt: systemPrompt,
		Timeout:      5 * time.Minute,
		MaxTurns:     10, // Sub-agents get fewer turns to prevent runaway
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

// ---------------------------------------------------------------------------
// Tool: browse_page — Navigate web pages, extract content, follow links
// ---------------------------------------------------------------------------

func (e *Executor) execBrowsePage(ctx context.Context, input map[string]any) (string, bool) {
	urlStr, _ := input["url"].(string)
	if urlStr == "" {
		return "url is required", true
	}

	// Block internal addresses
	if isInternalURL(urlStr) {
		return "access to internal network addresses is blocked", true
	}

	selector, _ := input["selector"].(string) // CSS selector to extract
	action, _ := input["action"].(string)       // "get_text", "get_links", "get_html"

	if action == "" {
		action = "get_text"
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "failed to create request: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Aurion-Agent/2.0; +https://aurion.studio)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "page request failed: " + err.Error(), true
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500*1024)) // 500KB max
	if err != nil {
		return "failed to read page: " + err.Error(), true
	}

	htmlStr := string(body)
	contentType := resp.Header.Get("Content-Type")

	// For non-HTML responses, return raw content
	if !strings.Contains(contentType, "html") {
		if len(htmlStr) > 50000 {
			htmlStr = htmlStr[:50000] + "\n... (truncated)"
		}
		return fmt.Sprintf("URL: %s\nStatus: %d\nContent-Type: %s\n\n%s", urlStr, resp.StatusCode, contentType, htmlStr), false
	}

	var result string
	switch action {
	case "get_text":
		result = extractTextFromHTML(htmlStr)
		if selector != "" {
			result = extractBySelector(htmlStr, selector)
		}
	case "get_links":
		result = extractLinksFromHTML(htmlStr, urlStr)
	case "get_html":
		result = htmlStr
	default:
		result = extractTextFromHTML(htmlStr)
	}

	if len(result) > 80000 {
		result = result[:80000] + "\n... (truncated)"
	}

	// Log browse event
	eventPayload, _ := json.Marshal(map[string]string{
		"url": urlStr, "action": action, "status": strconv.Itoa(resp.StatusCode),
	})
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.browse_page",
		Payload:   eventPayload,
	})

	return fmt.Sprintf("URL: %s\nStatus: %d\nFinal URL: %s\n\n%s", urlStr, resp.StatusCode, resp.Request.URL.String(), result), false
}

// ---------------------------------------------------------------------------
// Tool: screenshot_page — Get a visual summary of a web page
// ---------------------------------------------------------------------------

func (e *Executor) execScreenshotPage(ctx context.Context, input map[string]any) (string, bool) {
	urlStr, _ := input["url"].(string)
	if urlStr == "" {
		return "url is required", true
	}

	if isInternalURL(urlStr) {
		return "access to internal network addresses is blocked", true
	}

	// Use headless browser via command if available, fall back to text extraction
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Try using chromium/google-chrome for real screenshots
	browsers := []string{"chromium-browser", "chromium", "google-chrome", "google-chrome-stable"}
	var browserCmd string
	for _, b := range browsers {
		if _, err := exec.LookPath(b); err == nil {
			browserCmd = b
			break
		}
	}

	if browserCmd != "" {
		// Real headless screenshot
		outputPath := filepath.Join(e.Sandbox.WorkDir(), "screenshot.png")
		args := []string{
			"--headless", "--disable-gpu", "--no-sandbox",
			"--window-size=1920,1080",
			"--screenshot=" + outputPath,
			urlStr,
		}
		cmd := exec.CommandContext(reqCtx, browserCmd, args...)
		cmd.Dir = e.Sandbox.WorkDir()
		if out, err := cmd.CombinedOutput(); err != nil {
			e.Logger.Warn("headless browser screenshot failed", "error", err, "output", string(out))
			// Fall through to text extraction
		} else {
			// Read screenshot and return as base64
			data, err := os.ReadFile(outputPath)
			if err != nil {
				return "failed to read screenshot: " + err.Error(), true
			}
			os.Remove(outputPath) // Clean up

			encoded := base64.StdEncoding.EncodeToString(data)
			if len(encoded) > 200000 {
				encoded = encoded[:200000]
			}

			eventPayload, _ := json.Marshal(map[string]string{
				"url": urlStr, "method": "headless_browser", "size": strconv.Itoa(len(data)),
			})
			e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
				SessionID: e.SessionID,
				Type:      "tool.screenshot",
				Payload:   eventPayload,
			})

			return fmt.Sprintf("Screenshot captured for %s (%d bytes).\n[Base64 PNG data available in session artifacts]\nPage loaded successfully.", urlStr, len(data)), false
		}
	}

	// Fallback: fetch and extract structured text summary
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "request failed: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Aurion-Agent/2.0)")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "page fetch failed: " + err.Error(), true
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 300*1024))
	htmlStr := string(body)

	title := extractHTMLTag(htmlStr, "title")
	description := extractMetaContent(htmlStr, "description")
	text := extractTextFromHTML(htmlStr)
	links := extractLinksFromHTML(htmlStr, urlStr)

	if len(text) > 40000 {
		text = text[:40000] + "\n... (truncated)"
	}

	result := fmt.Sprintf("=== Page Summary for %s ===\nTitle: %s\nDescription: %s\nStatus: %d\n\n=== Visible Text ===\n%s\n\n=== Links Found ===\n%s",
		urlStr, title, description, resp.StatusCode, text, links)

	return result, false
}

// ---------------------------------------------------------------------------
// Tool: download_file — Download a file from a URL to the sandbox
// ---------------------------------------------------------------------------

func (e *Executor) execDownloadFile(ctx context.Context, input map[string]any) (string, bool) {
	urlStr, _ := input["url"].(string)
	if urlStr == "" {
		return "url is required", true
	}

	if isInternalURL(urlStr) {
		return "access to internal network addresses is blocked", true
	}

	filename, _ := input["filename"].(string)
	if filename == "" {
		// Extract filename from URL
		parsed, err := url.Parse(urlStr)
		if err != nil {
			return "invalid URL: " + err.Error(), true
		}
		filename = path.Base(parsed.Path)
		if filename == "" || filename == "/" || filename == "." {
			filename = "downloaded_file"
		}
	}

	// Sanitize filename
	filename = filepath.Base(filename)

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "request failed: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "Aurion-Agent/2.0")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "download failed: " + err.Error(), true
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Sprintf("download failed with status %d", resp.StatusCode), true
	}

	// Limit download to 50MB
	const maxSize = 50 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return "failed to read download: " + err.Error(), true
	}

	// Write to sandbox
	if err := e.Sandbox.WriteFile(filename, string(data)); err != nil {
		return "failed to save file: " + err.Error(), true
	}

	contentType := resp.Header.Get("Content-Type")

	eventPayload, _ := json.Marshal(map[string]string{
		"url": urlStr, "filename": filename, "size": strconv.Itoa(len(data)), "content_type": contentType,
	})
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.download_file",
		Payload:   eventPayload,
	})

	return fmt.Sprintf("Downloaded %s (%d bytes, %s) → saved as %s in sandbox", urlStr, len(data), contentType, filename), false
}

// ---------------------------------------------------------------------------
// Tool: plan_task — Structured multi-step task decomposition
// ---------------------------------------------------------------------------

func (e *Executor) execPlanTask(ctx context.Context, input map[string]any) (string, bool) {
	task, _ := input["task"].(string)
	if task == "" {
		return "task description is required", true
	}

	stepsRaw, _ := input["steps"].([]any)
	if len(stepsRaw) == 0 {
		return "steps array is required — provide an ordered list of sub-tasks", true
	}

	type planStep struct {
		ID          int    `json:"id"`
		Description string `json:"description"`
		Tool        string `json:"tool,omitempty"`
		Status      string `json:"status"`
		DependsOn   []int  `json:"depends_on,omitempty"`
	}

	steps := make([]planStep, 0, len(stepsRaw))
	for i, s := range stepsRaw {
		switch v := s.(type) {
		case string:
			steps = append(steps, planStep{ID: i + 1, Description: v, Status: "pending"})
		case map[string]any:
			desc, _ := v["description"].(string)
			tool, _ := v["tool"].(string)
			step := planStep{ID: i + 1, Description: desc, Tool: tool, Status: "pending"}
			if deps, ok := v["depends_on"].([]any); ok {
				for _, d := range deps {
					if num, ok := d.(float64); ok {
						step.DependsOn = append(step.DependsOn, int(num))
					}
				}
			}
			steps = append(steps, step)
		}
	}

	plan := map[string]any{
		"task":       task,
		"steps":      steps,
		"created_at": time.Now().Format(time.RFC3339),
		"status":     "planned",
	}

	planJSON, _ := json.MarshalIndent(plan, "", "  ")

	// Store plan in session events
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.plan_created",
		Payload:   planJSON,
	})

	// Also write plan to sandbox for reference
	e.Sandbox.WriteFile("plan.json", string(planJSON))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task Plan: %s\n", task))
	sb.WriteString(fmt.Sprintf("Steps: %d\n\n", len(steps)))
	for _, s := range steps {
		deps := ""
		if len(s.DependsOn) > 0 {
			depStrs := make([]string, len(s.DependsOn))
			for i, d := range s.DependsOn {
				depStrs[i] = strconv.Itoa(d)
			}
			deps = fmt.Sprintf(" (depends on: %s)", strings.Join(depStrs, ", "))
		}
		tool := ""
		if s.Tool != "" {
			tool = fmt.Sprintf(" [tool: %s]", s.Tool)
		}
		sb.WriteString(fmt.Sprintf("  %d. %s%s%s\n", s.ID, s.Description, tool, deps))
	}
	sb.WriteString("\nPlan saved. Execute steps sequentially using the appropriate tools.")

	return sb.String(), false
}

// ---------------------------------------------------------------------------
// Tool: extract_links — Extract and categorize all links from a webpage
// ---------------------------------------------------------------------------

func (e *Executor) execExtractLinks(ctx context.Context, input map[string]any) (string, bool) {
	urlStr, _ := input["url"].(string)
	if urlStr == "" {
		return "url is required", true
	}

	if isInternalURL(urlStr) {
		return "access to internal network addresses is blocked", true
	}

	filterType, _ := input["filter"].(string) // "internal", "external", "all"
	if filterType == "" {
		filterType = "all"
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "request failed: " + err.Error(), true
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Aurion-Agent/2.0)")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "page fetch failed: " + err.Error(), true
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 300*1024))
	htmlStr := string(body)

	parsedBase, _ := url.Parse(urlStr)

	// Extract all href links
	linkRe := regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+)["']`)
	matches := linkRe.FindAllStringSubmatch(htmlStr, -1)

	type linkInfo struct {
		URL      string `json:"url"`
		Text     string `json:"text,omitempty"`
		Type     string `json:"type"` // internal, external
	}

	var links []linkInfo
	seen := make(map[string]bool)

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		href := strings.TrimSpace(m[1])
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") {
			continue
		}

		// Resolve relative URLs
		resolved := href
		if !strings.HasPrefix(href, "http") {
			if ref, err := url.Parse(href); err == nil && parsedBase != nil {
				resolved = parsedBase.ResolveReference(ref).String()
			}
		}

		if seen[resolved] {
			continue
		}
		seen[resolved] = true

		linkType := "external"
		if parsedRef, err := url.Parse(resolved); err == nil && parsedBase != nil {
			if parsedRef.Host == parsedBase.Host {
				linkType = "internal"
			}
		}

		if filterType != "all" && filterType != linkType {
			continue
		}

		links = append(links, linkInfo{URL: resolved, Type: linkType})
	}

	if len(links) == 0 {
		return "No links found on the page.", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d links on %s:\n\n", len(links), urlStr))
	for i, l := range links {
		if i >= 200 { // Max 200 links
			sb.WriteString(fmt.Sprintf("\n... and %d more links", len(links)-200))
			break
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s\n", l.Type, l.URL))
	}

	return sb.String(), false
}

// ---------------------------------------------------------------------------
// Tool: fill_form — Submit form data to a URL via POST
// ---------------------------------------------------------------------------

func (e *Executor) execFillForm(ctx context.Context, input map[string]any) (string, bool) {
	urlStr, _ := input["url"].(string)
	if urlStr == "" {
		return "url is required", true
	}

	if isInternalURL(urlStr) {
		return "access to internal network addresses is blocked", true
	}

	fields, _ := input["fields"].(map[string]any)
	if len(fields) == 0 {
		return "fields (key-value pairs) are required", true
	}

	contentType, _ := input["content_type"].(string)
	if contentType == "" {
		contentType = "application/x-www-form-urlencoded"
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var bodyReader io.Reader
	switch contentType {
	case "application/json":
		data, _ := json.Marshal(fields)
		bodyReader = bytes.NewReader(data)
	default: // form-urlencoded
		form := url.Values{}
		for k, v := range fields {
			form.Set(k, fmt.Sprintf("%v", v))
		}
		bodyReader = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, urlStr, bodyReader)
	if err != nil {
		return "request failed: " + err.Error(), true
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Aurion-Agent/2.0)")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "form submission failed: " + err.Error(), true
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	respText := string(respBody)
	if strings.Contains(resp.Header.Get("Content-Type"), "html") {
		respText = extractTextFromHTML(respText)
	}

	if len(respText) > 30000 {
		respText = respText[:30000] + "\n... (truncated)"
	}

	eventPayload, _ := json.Marshal(map[string]string{
		"url": urlStr, "status": strconv.Itoa(resp.StatusCode),
	})
	e.Queries.CreateSessionEvent(ctx, db.CreateSessionEventParams{
		SessionID: e.SessionID,
		Type:      "tool.form_submitted",
		Payload:   eventPayload,
	})

	return fmt.Sprintf("Form submitted to %s\nStatus: %d\n\n%s", urlStr, resp.StatusCode, respText), false
}

// ===========================================================================
// HTML parsing helpers
// ===========================================================================

// extractTextFromHTML strips HTML tags and returns visible text content.
func extractTextFromHTML(html string) string {
	// Remove script and style elements
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = scriptRe.ReplaceAllString(html, "")
	html = styleRe.ReplaceAllString(html, "")

	// Remove HTML comments
	commentRe := regexp.MustCompile(`(?s)<!--.*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	// Add newlines before block elements
	blockRe := regexp.MustCompile(`(?i)<(br|p|div|h[1-6]|li|tr|blockquote|pre|hr)[^>]*>`)
	html = blockRe.ReplaceAllString(html, "\n")

	// Strip all tags
	tagRe := regexp.MustCompile(`<[^>]+>`)
	text := tagRe.ReplaceAllString(html, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// Collapse whitespace
	spaceRe := regexp.MustCompile(`[ \t]+`)
	text = spaceRe.ReplaceAllString(text, " ")
	nlRe := regexp.MustCompile(`\n{3,}`)
	text = nlRe.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// extractLinksFromHTML extracts all links with their text.
func extractLinksFromHTML(html, baseURL string) string {
	linkRe := regexp.MustCompile(`(?is)<a[^>]+href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	matches := linkRe.FindAllStringSubmatch(html, 100) // Max 100 links

	parsedBase, _ := url.Parse(baseURL)

	var sb strings.Builder
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		href := strings.TrimSpace(m[1])
		text := extractTextFromHTML(m[2])
		text = strings.TrimSpace(text)

		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
			continue
		}

		// Resolve relative URLs
		if !strings.HasPrefix(href, "http") && parsedBase != nil {
			if ref, err := url.Parse(href); err == nil {
				href = parsedBase.ResolveReference(ref).String()
			}
		}

		if seen[href] {
			continue
		}
		seen[href] = true

		if text != "" {
			sb.WriteString(fmt.Sprintf("- [%s](%s)\n", text, href))
		} else {
			sb.WriteString(fmt.Sprintf("- %s\n", href))
		}
	}

	if sb.Len() == 0 {
		return "No links found."
	}
	return sb.String()
}

// extractBySelector does a simplified CSS selector extraction.
func extractBySelector(html, selector string) string {
	// Simple implementation: extract content between matching tags
	// Supports: tag, .class, #id (basic)
	var pattern string
	if strings.HasPrefix(selector, "#") {
		id := strings.TrimPrefix(selector, "#")
		pattern = fmt.Sprintf(`(?is)<[^>]+id\s*=\s*["']%s["'][^>]*>(.*?)</`, regexp.QuoteMeta(id))
	} else if strings.HasPrefix(selector, ".") {
		class := strings.TrimPrefix(selector, ".")
		pattern = fmt.Sprintf(`(?is)<[^>]+class\s*=\s*["'][^"']*\b%s\b[^"']*["'][^>]*>(.*?)</`, regexp.QuoteMeta(class))
	} else {
		pattern = fmt.Sprintf(`(?is)<%s[^>]*>(.*?)</%s>`, regexp.QuoteMeta(selector), regexp.QuoteMeta(selector))
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return extractTextFromHTML(html)
	}

	matches := re.FindAllStringSubmatch(html, 10)
	var sb strings.Builder
	for _, m := range matches {
		if len(m) >= 2 {
			sb.WriteString(extractTextFromHTML(m[1]))
			sb.WriteString("\n")
		}
	}

	if sb.Len() == 0 {
		return "No content found matching selector: " + selector
	}
	return sb.String()
}

// extractHTMLTag extracts the content of the first matching HTML tag.
func extractHTMLTag(html, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<%s[^>]*>(.*?)</%s>`, tag, tag))
	m := re.FindStringSubmatch(html)
	if len(m) >= 2 {
		return strings.TrimSpace(extractTextFromHTML(m[1]))
	}
	return ""
}

// extractMetaContent extracts the content attribute of a meta tag by name.
func extractMetaContent(html, name string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?i)<meta[^>]+name\s*=\s*["']%s["'][^>]+content\s*=\s*["']([^"']*)["']`, regexp.QuoteMeta(name)))
	m := re.FindStringSubmatch(html)
	if len(m) >= 2 {
		return m[1]
	}
	// Try reversed attribute order
	re2 := regexp.MustCompile(fmt.Sprintf(`(?i)<meta[^>]+content\s*=\s*["']([^"']*)["'][^>]+name\s*=\s*["']%s["']`, regexp.QuoteMeta(name)))
	m2 := re2.FindStringSubmatch(html)
	if len(m2) >= 2 {
		return m2[1]
	}
	return ""
}

// isInternalURL checks if a URL points to an internal network address.
// It resolves the hostname to IP addresses and checks all of them against
// private/reserved ranges, preventing SSRF via DNS rebinding, hex IPs, etc.
func isInternalURL(urlStr string) bool {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return true // block unparseable URLs
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return true
	}

	// Quick string checks for common patterns
	lower := strings.ToLower(hostname)
	if lower == "localhost" || lower == "0.0.0.0" || strings.HasSuffix(lower, ".local") ||
		strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".railway.internal") {
		return true
	}

	// Block metadata endpoints (cloud providers)
	if lower == "metadata.google.internal" || lower == "169.254.169.254" {
		return true
	}

	// Resolve hostname to IPs and check each one
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// If we can't resolve, try parsing as IP directly
		ip := net.ParseIP(hostname)
		if ip != nil {
			return isPrivateIP(ip)
		}
		return true // can't resolve → block
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return true
		}
	}

	return false
}

// isPrivateIP checks if an IP is in a private/reserved/loopback range.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}

	privateRanges := []struct {
		network string
	}{
		{"10.0.0.0/8"},
		{"172.16.0.0/12"},
		{"192.168.0.0/16"},
		{"169.254.0.0/16"}, // link-local / cloud metadata
		{"100.64.0.0/10"},  // CGN
		{"fc00::/7"},        // IPv6 ULA
		{"fe80::/10"},       // IPv6 link-local
		{"::1/128"},         // IPv6 loopback
	}

	for _, r := range privateRanges {
		_, cidr, err := net.ParseCIDR(r.network)
		if err != nil {
			continue
		}
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

func computeSha256(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}
