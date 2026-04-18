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
	"strings"
	"time"
)

// ToolExecutor is called by the agentic loop to execute tools server-side.
// The executor package implements this interface.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName, callID string, input map[string]any) ToolExecResult
}

// ToolExecResult is returned by a ToolExecutor.
type ToolExecResult struct {
	CallID  string
	Output  string
	IsError bool
}

// agenticCloudBackend wraps cloudClaudeBackend with a multi-turn agentic loop.
// When Claude returns tool_use blocks, this backend executes them via the
// ToolExecutor and sends tool_result back, continuing until end_turn or max turns.
type agenticCloudBackend struct {
	apiKey       string
	baseURL      string
	logger       *slog.Logger
	toolExecutor ToolExecutor
	tools        []anthropicTool
	maxTurns     int
}

// NewAgenticCloudClaude creates a Backend with full agentic loop support.
// If enabledTools is nil or empty, all tools are enabled.
func NewAgenticCloudClaude(apiKey string, logger *slog.Logger, executor ToolExecutor, enabledTools []string) Backend {
	if logger == nil {
		logger = slog.Default()
	}

	// Build tool set
	allTools := AllTools()
	tools := allTools
	if len(enabledTools) > 0 {
		enabled := make(map[string]bool, len(enabledTools))
		for _, n := range enabledTools {
			enabled[n] = true
		}
		tools = nil
		for _, t := range allTools {
			if enabled[t.Name] {
				tools = append(tools, t)
			}
		}
	}

	return &agenticCloudBackend{
		apiKey:       apiKey,
		baseURL:      "https://api.anthropic.com",
		logger:       logger,
		toolExecutor: executor,
		tools:        tools,
		maxTurns:     25,
	}
}

func (b *agenticCloudBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not configured")
	}

	model := opts.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	maxTurns := b.maxTurns
	if opts.MaxTurns > 0 {
		maxTurns = opts.MaxTurns
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultCloudSystemPrompt()
	}

	tools := b.tools
	if len(tools) == 0 {
		tools = AllTools()
	}

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		b.agenticLoop(runCtx, model, systemPrompt, prompt, tools, maxTurns, msgCh, resCh)
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// agenticLoop runs the multi-turn conversation loop. Each iteration:
// 1. Call Anthropic API with current messages
// 2. Parse streaming response, collecting text + tool_use blocks
// 3. If stop_reason == "tool_use", execute all tools, append results, loop
// 4. If stop_reason == "end_turn" or max turns reached, finish
func (b *agenticCloudBackend) agenticLoop(
	ctx context.Context,
	model, systemPrompt, prompt string,
	tools []anthropicTool,
	maxTurns int,
	msgCh chan<- Message,
	resCh chan<- Result,
) {
	startTime := time.Now()

	// Build initial messages
	messages := []anthropicMessage{
		{Role: "user", Content: prompt},
	}

	var totalInputTokens, totalOutputTokens, totalCacheRead, totalCacheWrite int64
	finalStatus := "completed"
	var finalError string
	var finalOutput strings.Builder

	for turn := 0; turn < maxTurns; turn++ {
		select {
		case <-ctx.Done():
			finalStatus = "aborted"
			finalError = ctx.Err().Error()
			goto done
		default:
		}

		b.logger.Info("agentic loop: calling API",
			"turn", turn+1,
			"max_turns", maxTurns,
			"messages_count", len(messages),
		)

		safeSend(ctx, msgCh, Message{
			Type:    MessageStatus,
			Content: fmt.Sprintf("Turn %d/%d", turn+1, maxTurns),
			Status:  "thinking",
		})

		// Call Anthropic API
		turnResult, err := b.callAnthropicAPI(ctx, model, systemPrompt, messages, tools)
		if err != nil {
			finalStatus = "failed"
			finalError = err.Error()
			safeSend(ctx, msgCh, Message{Type: MessageError, Content: finalError})
			goto done
		}

		// Accumulate usage
		totalInputTokens += turnResult.inputTokens
		totalOutputTokens += turnResult.outputTokens
		totalCacheRead += turnResult.cacheReadTokens
		totalCacheWrite += turnResult.cacheWriteTokens

		// Stream text content to message channel
		for _, block := range turnResult.contentBlocks {
			switch block.Type {
			case "text":
				finalOutput.WriteString(block.Text)
				safeSend(ctx, msgCh, Message{
					Type:    MessageText,
					Content: block.Text,
				})
			case "thinking":
				safeSend(ctx, msgCh, Message{
					Type:    MessageThinking,
					Content: block.Text,
				})
			}
		}

		// Check if we need to execute tools
		toolUseBlocks := turnResult.toolUseBlocks()
		if len(toolUseBlocks) == 0 || turnResult.stopReason != "tool_use" {
			// No tool calls or end_turn — we're done
			if turnResult.stopReason == "max_tokens" {
				safeSend(ctx, msgCh, Message{
					Type:    MessageStatus,
					Content: "max tokens reached",
					Status:  "max_tokens",
				})
			}
			break
		}

		// Build the assistant message with ALL content blocks (text + tool_use)
		assistantContent := make([]contentBlock, 0, len(turnResult.contentBlocks))
		for _, cb := range turnResult.contentBlocks {
			assistantContent = append(assistantContent, cb)
		}
		messages = append(messages, anthropicMessage{
			Role:    "assistant",
			Content: marshalContentBlocks(assistantContent),
		})

		// Execute each tool and build tool_result blocks
		var toolResults []contentBlock
		for _, tu := range toolUseBlocks {
			// Notify about tool use
			safeSend(ctx, msgCh, Message{
				Type:   MessageToolUse,
				Tool:   tu.Name,
				CallID: tu.ID,
				Input:  parseToolInput(tu.Input),
			})

			// Execute the tool
			input := parseToolInput(tu.Input)
			result := b.toolExecutor.Execute(ctx, tu.Name, tu.ID, input)

			// Notify about tool result
			safeSend(ctx, msgCh, Message{
				Type:   MessageToolResult,
				Tool:   tu.Name,
				CallID: tu.ID,
				Output: truncateOutput(result.Output, 30000),
			})

			toolResults = append(toolResults, contentBlock{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   truncateOutput(result.Output, 30000),
			})
		}

		// Add tool results as user message
		messages = append(messages, anthropicMessage{
			Role:    "user",
			Content: marshalContentBlocks(toolResults),
		})
	}

done:
	usage := map[string]TokenUsage{
		model: {
			InputTokens:      totalInputTokens,
			OutputTokens:     totalOutputTokens,
			CacheReadTokens:  totalCacheRead,
			CacheWriteTokens: totalCacheWrite,
		},
	}

	resCh <- Result{
		Status:     finalStatus,
		Output:     finalOutput.String(),
		Error:      finalError,
		DurationMs: time.Since(startTime).Milliseconds(),
		Usage:      usage,
	}

	b.logger.Info("agentic loop: done",
		"status", finalStatus,
		"duration_ms", time.Since(startTime).Milliseconds(),
		"input_tokens", totalInputTokens,
		"output_tokens", totalOutputTokens,
	)
}

// turnResult holds the parsed result of a single API call.
type turnResult struct {
	contentBlocks   []contentBlock
	stopReason      string
	inputTokens     int64
	outputTokens    int64
	cacheReadTokens int64
	cacheWriteTokens int64
}

func (r *turnResult) toolUseBlocks() []contentBlock {
	var out []contentBlock
	for _, cb := range r.contentBlocks {
		if cb.Type == "tool_use" {
			out = append(out, cb)
		}
	}
	return out
}

// callAnthropicAPI makes a single call to the Anthropic Messages API and
// returns the parsed content blocks and metadata.
func (b *agenticCloudBackend) callAnthropicAPI(
	ctx context.Context,
	model, systemPrompt string,
	messages []anthropicMessage,
	tools []anthropicTool,
) (*turnResult, error) {
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
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", b.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(errBody))
	}

	return b.parseStreamingResponse(ctx, resp.Body)
}

// parseStreamingResponse reads an SSE stream from the Anthropic API and
// returns all content blocks + metadata. Unlike streamResponse, this
// collects everything for the agentic loop instead of streaming to channels.
func (b *agenticCloudBackend) parseStreamingResponse(ctx context.Context, body io.Reader) (*turnResult, error) {
	result := &turnResult{}

	var currentBlockType string
	var currentToolName string
	var currentToolID string
	var currentToolInput strings.Builder
	var currentText strings.Builder

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()
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
			continue
		}

		switch eventType.Type {
		case "message_start":
			var evt sseMessageStart
			if json.Unmarshal([]byte(data), &evt) == nil {
				result.inputTokens += evt.Message.Usage.InputTokens
				result.outputTokens += evt.Message.Usage.OutputTokens
				result.cacheReadTokens += evt.Message.Usage.CacheReadTokens
				result.cacheWriteTokens += evt.Message.Usage.CacheCreateTokens
			}

		case "content_block_start":
			var evt sseContentBlockStart
			if json.Unmarshal([]byte(data), &evt) == nil {
				currentBlockType = evt.ContentBlock.Type
				switch evt.ContentBlock.Type {
				case "tool_use":
					currentToolName = evt.ContentBlock.Name
					currentToolID = evt.ContentBlock.ID
					currentToolInput.Reset()
				case "text":
					currentText.Reset()
					if evt.ContentBlock.Text != "" {
						currentText.WriteString(evt.ContentBlock.Text)
					}
				case "thinking":
					currentText.Reset()
					if evt.ContentBlock.Text != "" {
						currentText.WriteString(evt.ContentBlock.Text)
					}
				}
			}

		case "content_block_delta":
			var evt sseContentBlockDelta
			if json.Unmarshal([]byte(data), &evt) == nil {
				switch evt.Delta.Type {
				case "text_delta":
					currentText.WriteString(evt.Delta.Text)
				case "input_json_delta":
					currentToolInput.WriteString(evt.Delta.PartialJSON)
				case "thinking_delta":
					currentText.WriteString(evt.Delta.Text)
				}
			}

		case "content_block_stop":
			switch currentBlockType {
			case "tool_use":
				var inputJSON any
				if raw := currentToolInput.String(); raw != "" {
					json.Unmarshal([]byte(raw), &inputJSON)
				}
				result.contentBlocks = append(result.contentBlocks, contentBlock{
					Type:  "tool_use",
					ID:    currentToolID,
					Name:  currentToolName,
					Input: inputJSON,
				})
			case "text":
				result.contentBlocks = append(result.contentBlocks, contentBlock{
					Type: "text",
					Text: currentText.String(),
				})
			case "thinking":
				result.contentBlocks = append(result.contentBlocks, contentBlock{
					Type: "thinking",
					Text: currentText.String(),
				})
			}
			currentBlockType = ""
			currentToolName = ""
			currentToolID = ""

		case "message_delta":
			var evt sseMessageDelta
			if json.Unmarshal([]byte(data), &evt) == nil {
				result.outputTokens += evt.Usage.OutputTokens
				result.stopReason = evt.Delta.StopReason
			}

		case "error":
			var errEvt struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &errEvt) == nil {
				return nil, fmt.Errorf("%s: %s", errEvt.Error.Type, errEvt.Error.Message)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read error: %v", err)
	}

	return result, nil
}

// marshalContentBlocks serializes content blocks to JSON for the API.
func marshalContentBlocks(blocks []contentBlock) json.RawMessage {
	data, _ := json.Marshal(blocks)
	return data
}

// parseToolInput extracts map[string]any from the tool input (which may be
// any type after JSON unmarshaling).
func parseToolInput(input any) map[string]any {
	if m, ok := input.(map[string]any); ok {
		return m
	}
	// Try re-marshaling
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

// truncateOutput limits string length.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (output truncated)"
}

// AllTools returns the complete set of tools for the agentic cloud backend.
// This extends defaultTools() with additional execution tools.
func AllTools() []anthropicTool {
	base := defaultTools()
	extra := []anthropicTool{
		{
			Name:        "bash",
			Description: "Execute a shell command in a sandboxed environment. Use for running scripts, installing packages, or any system operation.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "The shell command to execute"},
					"timeout": map[string]any{"type": "number", "description": "Timeout in seconds (max 120, default 30)"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read the contents of a file at the given path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "File path to read"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file, creating it if it doesn't exist.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "File path to write"},
					"content": map[string]any{"type": "string", "description": "Content to write to the file"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "list_directory",
			Description: "List files and directories at the given path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Directory path to list (default: current directory)"},
				},
			},
		},
		{
			Name:        "send_email",
			Description: "Send an email notification.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":      map[string]any{"type": "string", "description": "Recipient email address"},
					"subject": map[string]any{"type": "string", "description": "Email subject"},
					"body":    map[string]any{"type": "string", "description": "Email body (supports markdown)"},
				},
				"required": []string{"to", "subject", "body"},
			},
		},
		{
			Name:        "http_request",
			Description: "Make an HTTP request to an external URL. Only GET and POST are allowed. Internal network addresses are blocked.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":     map[string]any{"type": "string", "description": "The URL to request"},
					"method":  map[string]any{"type": "string", "enum": []string{"GET", "POST"}, "description": "HTTP method (default: GET)"},
					"headers": map[string]any{"type": "object", "description": "Optional HTTP headers"},
					"body":    map[string]any{"type": "string", "description": "Request body for POST requests"},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "memory_read",
			Description: "Read a value from the agent's persistent memory store.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"store_id": map[string]any{"type": "string", "description": "Memory store ID"},
					"path":     map[string]any{"type": "string", "description": "Memory path (e.g., 'notes/project.md')"},
				},
				"required": []string{"store_id", "path"},
			},
		},
		{
			Name:        "memory_write",
			Description: "Write a value to the agent's persistent memory store.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"store_id": map[string]any{"type": "string", "description": "Memory store ID"},
					"path":     map[string]any{"type": "string", "description": "Memory path (e.g., 'notes/project.md')"},
					"content":  map[string]any{"type": "string", "description": "Content to store"},
				},
				"required": []string{"store_id", "path", "content"},
			},
		},
		{
			Name:        "delegate_to_agent",
			Description: "Delegate a task to another managed agent in the workspace. The sub-agent will run the task and return its result.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_id": map[string]any{"type": "string", "description": "ID of the target agent to delegate to"},
					"prompt":   map[string]any{"type": "string", "description": "The task description for the sub-agent"},
				},
				"required": []string{"agent_id", "prompt"},
			},
		},
	}
	return append(base, extra...)
}
