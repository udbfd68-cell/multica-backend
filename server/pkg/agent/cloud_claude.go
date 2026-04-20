package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// cloudClaudeBackend implements Backend by calling the Anthropic Messages API
// directly over HTTP with streaming. This enables server-side agent execution
// without requiring a local daemon or Claude Code CLI.
type cloudClaudeBackend struct {
	apiKey  string
	baseURL string
	logger  *slog.Logger
}

// NewCloudClaude creates a Backend that calls the Anthropic Messages API.
func NewCloudClaude(apiKey string, logger *slog.Logger) Backend {
	if logger == nil {
		logger = slog.Default()
	}
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &cloudClaudeBackend{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
	}
}

// anthropicRequest is the Anthropic Messages API request body.
type anthropicRequest struct {
	Model       string              `json:"model"`
	MaxTokens   int                 `json:"max_tokens"`
	System      string              `json:"system,omitempty"`
	Messages    []anthropicMessage  `json:"messages"`
	Stream      bool                `json:"stream"`
	Tools       []anthropicTool     `json:"tools,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []contentBlock
}

type contentBlock struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Input   any    `json:"input,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

// SSE event types from the Anthropic streaming API.
type sseContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type  string `json:"type"`
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input any    `json:"input,omitempty"`
	} `json:"content_block"`
}

type sseContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

type sseMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens       int64 `json:"input_tokens"`
			OutputTokens      int64 `json:"output_tokens"`
			CacheReadTokens   int64 `json:"cache_read_input_tokens"`
			CacheCreateTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type sseMessageDelta struct {
	Type  string `json:"type"`
	Usage struct {
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
}

func (b *cloudClaudeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not configured")
	}

	model := opts.Model
	if model == "" {
		model = os.Getenv("CLOUD_DEFAULT_MODEL")
	}
	if model == "" {
		model = "anthropic/claude-opus-4.6"
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	messages := []anthropicMessage{
		{Role: "user", Content: prompt},
	}

	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultCloudSystemPrompt()
	}

	// Build tool definitions for the agent
	tools := defaultTools()

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: 16384,
		System:    systemPrompt,
		Messages:  messages,
		Stream:    true,
		Tools:     tools,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, b.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.Contains(b.baseURL, "openrouter.ai") {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
		req.Header.Set("HTTP-Referer", os.Getenv("FRONTEND_ORIGIN"))
	} else {
		req.Header.Set("x-api-key", b.apiKey)
	}
	req.Header.Set("anthropic-version", "2023-06-01")

	b.logger.Info("cloud claude: calling API", "model", model, "base_url", b.baseURL, "prompt_len", len(prompt))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("anthropic API call: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		cancel()
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer resp.Body.Close()

		b.streamResponse(runCtx, resp.Body, model, msgCh, resCh)
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *cloudClaudeBackend) streamResponse(ctx context.Context, body io.Reader, model string, msgCh chan<- Message, resCh chan<- Result) {
	startTime := time.Now()
	var output strings.Builder
	var inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64
	finalStatus := "completed"
	var finalError string
	var currentToolName string
	var currentToolID string
	var currentToolInput strings.Builder

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			finalStatus = "aborted"
			finalError = ctx.Err().Error()
			goto done
		default:
		}

		line := scanner.Text()

		// SSE format: "event: <type>\ndata: <json>"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var eventType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &eventType); err != nil {
			b.logger.Debug("cloud claude: skip non-JSON line", "data", data)
			continue
		}

		switch eventType.Type {
		case "message_start":
			var evt sseMessageStart
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				inputTokens += evt.Message.Usage.InputTokens
				outputTokens += evt.Message.Usage.OutputTokens
				cacheReadTokens += evt.Message.Usage.CacheReadTokens
				cacheWriteTokens += evt.Message.Usage.CacheCreateTokens
			}

		case "content_block_start":
			var evt sseContentBlockStart
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				switch evt.ContentBlock.Type {
				case "tool_use":
					currentToolName = evt.ContentBlock.Name
					currentToolID = evt.ContentBlock.ID
					currentToolInput.Reset()
					safeSend(ctx, msgCh, Message{
						Type:   MessageToolUse,
						Tool:   currentToolName,
						CallID: currentToolID,
					})
				case "thinking":
					safeSend(ctx, msgCh, Message{
						Type:    MessageThinking,
						Content: evt.ContentBlock.Text,
					})
				}
			}

		case "content_block_delta":
			var evt sseContentBlockDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				switch evt.Delta.Type {
				case "text_delta":
					output.WriteString(evt.Delta.Text)
					safeSend(ctx, msgCh, Message{
						Type:    MessageText,
						Content: evt.Delta.Text,
					})
				case "input_json_delta":
					currentToolInput.WriteString(evt.Delta.PartialJSON)
				case "thinking_delta":
					safeSend(ctx, msgCh, Message{
						Type:    MessageThinking,
						Content: evt.Delta.Text,
					})
				}
			}

		case "content_block_stop":
			if currentToolName != "" {
				// Parse accumulated tool input
				var toolInput map[string]any
				_ = json.Unmarshal([]byte(currentToolInput.String()), &toolInput)
				safeSend(ctx, msgCh, Message{
					Type:   MessageToolUse,
					Tool:   currentToolName,
					CallID: currentToolID,
					Input:  toolInput,
				})
				currentToolName = ""
				currentToolID = ""
				currentToolInput.Reset()
			}

		case "message_delta":
			var evt sseMessageDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				outputTokens += evt.Usage.OutputTokens
				if evt.Delta.StopReason == "end_turn" || evt.Delta.StopReason == "stop_sequence" {
					finalStatus = "completed"
				} else if evt.Delta.StopReason == "max_tokens" {
					finalStatus = "completed"
					safeSend(ctx, msgCh, Message{
						Type:    MessageStatus,
						Content: "max tokens reached",
						Status:  "max_tokens",
					})
				}
			}

		case "error":
			var errEvt struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &errEvt); err == nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("%s: %s", errEvt.Error.Type, errEvt.Error.Message)
				safeSend(ctx, msgCh, Message{
					Type:    MessageError,
					Content: finalError,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil && finalStatus == "completed" {
		finalStatus = "failed"
		finalError = fmt.Sprintf("stream read error: %v", err)
	}

done:
	usage := map[string]TokenUsage{
		model: {
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
			CacheReadTokens:  cacheReadTokens,
			CacheWriteTokens: cacheWriteTokens,
		},
	}

	resCh <- Result{
		Status:     finalStatus,
		Output:     output.String(),
		Error:      finalError,
		DurationMs: time.Since(startTime).Milliseconds(),
		Usage:      usage,
	}

	b.logger.Info("cloud claude: done",
		"status", finalStatus,
		"duration_ms", time.Since(startTime).Milliseconds(),
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
	)
}

// safeSend sends a message to the channel, respecting context cancellation.
func safeSend(ctx context.Context, ch chan<- Message, msg Message) {
	select {
	case ch <- msg:
	case <-ctx.Done():
	}
}

func defaultCloudSystemPrompt() string {
	return `You are a highly capable autonomous AI agent running on the Aurion platform. You are a managed coding agent with expertise in software engineering, project management, web automation, and technical problem-solving.

Your capabilities:
- Execute shell commands, read/write/edit files in an isolated sandbox
- Browse web pages, extract content, follow links, and submit forms
- Download files from the internet
- Send real emails (when SMTP or Resend is configured)
- Search the web for information
- Create, manage, and track issues in the workspace
- Read and write persistent memory across sessions
- Delegate tasks to other agents in the workspace (sub-agents have full tool access)
- Plan complex multi-step tasks with dependency tracking
- Connect to 50+ external services via MCP (GitHub, Slack, databases, cloud providers, etc.)

Autonomous execution guidelines:
- When given a task, break it into steps and execute them yourself using your tools
- Browse the web to gather information before making decisions
- Create issues to track progress on complex work
- Use plan_task for multi-step workflows, then execute each step
- Use bash for system operations, file manipulation, and script execution
- Use browse_page to read web content, documentation, or research topics
- Use download_file for fetching resources from the internet
- Use fill_form to interact with web APIs and services
- Use delegate_to_agent to parallelize work across multiple agents
- Use memory_write to persist important findings across sessions
- Be direct and actionable — execute rather than explain
- If a task requires multiple tools, chain them together automatically
- Report results concisely with what was done and what was found

You are part of the Aurion workspace and operate autonomously to complete tasks.`
}

func defaultTools() []anthropicTool {
	return []anthropicTool{
		{
			Name:        "create_issue",
			Description: "Create a new issue in the workspace for tracking work items, bugs, or feature requests.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":       map[string]any{"type": "string", "description": "Issue title"},
					"description": map[string]any{"type": "string", "description": "Detailed issue description in markdown"},
					"priority":    map[string]any{"type": "string", "enum": []string{"urgent", "high", "medium", "low", "none"}, "description": "Issue priority"},
					"status":      map[string]any{"type": "string", "enum": []string{"backlog", "todo", "in_progress", "in_review", "done"}, "description": "Initial status"},
				},
				"required": []string{"title"},
			},
		},
		{
			Name:        "list_issues",
			Description: "List issues in the workspace, optionally filtered by status or assignee.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status":  map[string]any{"type": "string", "description": "Filter by status (backlog, todo, in_progress, in_review, done)"},
					"limit":   map[string]any{"type": "integer", "description": "Maximum number of issues to return (default 20)"},
				},
			},
		},
		{
			Name:        "update_issue",
			Description: "Update an existing issue's status, priority, title, or description.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id":    map[string]any{"type": "string", "description": "The issue ID or identifier (e.g., MYW-1)"},
					"status":      map[string]any{"type": "string", "enum": []string{"backlog", "todo", "in_progress", "in_review", "done"}},
					"priority":    map[string]any{"type": "string", "enum": []string{"urgent", "high", "medium", "low", "none"}},
					"title":       map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
				},
				"required": []string{"issue_id"},
			},
		},
		{
			Name:        "add_comment",
			Description: "Add a comment to an issue for discussion or status updates.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issue_id": map[string]any{"type": "string", "description": "The issue ID or identifier"},
					"content":  map[string]any{"type": "string", "description": "Comment content in markdown"},
				},
				"required": []string{"issue_id", "content"},
			},
		},
		{
			Name:        "search_code",
			Description: "Search for code patterns, functions, or concepts in the codebase.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query (supports regex)"},
					"path":  map[string]any{"type": "string", "description": "Optional path filter (e.g., 'src/**/*.ts')"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "generate_code",
			Description: "Generate code based on a specification or requirement.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"language":    map[string]any{"type": "string", "description": "Programming language (typescript, go, python, etc.)"},
					"description": map[string]any{"type": "string", "description": "What the code should do"},
					"context":     map[string]any{"type": "string", "description": "Additional context like existing code or constraints"},
				},
				"required": []string{"description"},
			},
		},
	}
}
