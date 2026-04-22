// Package session — context.go implements context engineering strategies.
//
// The key insight from Anthropic's Managed Agents: the session log is NOT
// Claude's context window. The harness reads events from the session and
// TRANSFORMS them into a context window. This file implements three strategies:
//
//   1. sliding_window — Keep the last N tokens of events
//   2. smart_summary  — Compaction with summary when approaching limit
//   3. full_replay    — Replay all events (only for short sessions)
//
// Context anxiety prevention: when the context window approaches 80% of the
// model's limit, the harness triggers a context_reset event with a compressed
// summary. The original events remain in the session log — recoverable.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// AnthropicMessage — the format Claude expects
// ---------------------------------------------------------------------------

// AnthropicMessage is a single message in Claude's context window.
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"` // Can be string or JSON array of content blocks
}

// ContextWindow is the result of building the context from the session log.
type ContextWindow struct {
	Messages     []AnthropicMessage `json:"messages"`
	SystemPrompt string             `json:"system_prompt"`
	TokenEstimate int               `json:"token_estimate"`
	FromIndex    int                `json:"from_index"`     // first event included
	ToIndex      int                `json:"to_index"`       // last event included
	WasCompacted bool               `json:"was_compacted"`  // true if context_reset was used
}

// ---------------------------------------------------------------------------
// BuildContextWindow — Main entry point
// ---------------------------------------------------------------------------

// BuildContextWindow reads events from the session log and constructs the
// context window that will be sent to Claude. The strategy determines how
// events are selected and transformed.
func (s *Store) BuildContextWindow(
	ctx context.Context,
	sessionID string,
	systemPrompt string,
	strategy *ContextStrategy,
) (*ContextWindow, error) {
	if strategy == nil {
		strategy = &ContextStrategy{Type: "sliding_window", MaxTokens: 180000}
	}

	switch strategy.Type {
	case "full_replay":
		return s.buildFullReplay(ctx, sessionID, systemPrompt, strategy)
	case "smart_summary":
		return s.buildSmartSummary(ctx, sessionID, systemPrompt, strategy)
	default: // "sliding_window"
		return s.buildSlidingWindow(ctx, sessionID, systemPrompt, strategy)
	}
}

// ---------------------------------------------------------------------------
// Strategy: sliding_window
// ---------------------------------------------------------------------------

// buildSlidingWindow keeps the most recent events that fit within the token
// budget. If a context_reset exists, it starts from the summary.
func (s *Store) buildSlidingWindow(
	ctx context.Context,
	sessionID string,
	systemPrompt string,
	strategy *ContextStrategy,
) (*ContextWindow, error) {
	// Check for a context_reset — if one exists, start from there
	lastReset, _ := s.GetLastContextReset(ctx, sessionID)

	fromIndex := 0
	var prefixMessages []AnthropicMessage

	if lastReset != nil {
		// Start from after the context reset, with the summary as prefix
		fromIndex = lastReset.Index + 1
		prefixMessages = append(prefixMessages, AnthropicMessage{
			Role:    "user",
			Content: fmt.Sprintf("[Context Summary — previous %d events compacted]\n\n%s", lastReset.Data.CompactedRange[1]-lastReset.Data.CompactedRange[0], lastReset.Data.Summary),
		})
	}

	// Get all events from the starting point
	events, err := s.GetEvents(ctx, sessionID, &GetEventsOptions{From: fromIndex})
	if err != nil {
		return nil, err
	}

	// Convert events to messages
	allMessages := append(prefixMessages, eventsToMessages(events)...)

	// Estimate tokens and trim from the front if over budget
	maxTokens := strategy.MaxTokens
	if maxTokens == 0 {
		maxTokens = 180000
	}

	// Reserve 20% for the response
	budgetTokens := int(float64(maxTokens) * 0.8)
	// Reserve tokens for system prompt
	budgetTokens -= estimateTokens(systemPrompt)

	messages, trimmed := trimToTokenBudget(allMessages, budgetTokens)

	firstIdx := fromIndex
	if trimmed > 0 && len(events) > 0 {
		firstIdx = events[trimmed].Index
	}

	return &ContextWindow{
		Messages:      messages,
		SystemPrompt:  systemPrompt,
		TokenEstimate: estimateMessageTokens(messages),
		FromIndex:     firstIdx,
		ToIndex:       lastEventIndex(events),
		WasCompacted:  lastReset != nil,
	}, nil
}

// ---------------------------------------------------------------------------
// Strategy: smart_summary
// ---------------------------------------------------------------------------

// buildSmartSummary is like sliding_window but proactively creates summaries
// when the context gets large. In a full implementation, this would call a
// smaller model to generate the summary. For now it uses a heuristic.
func (s *Store) buildSmartSummary(
	ctx context.Context,
	sessionID string,
	systemPrompt string,
	strategy *ContextStrategy,
) (*ContextWindow, error) {
	// Same base as sliding_window
	return s.buildSlidingWindow(ctx, sessionID, systemPrompt, strategy)
}

// ---------------------------------------------------------------------------
// Strategy: full_replay
// ---------------------------------------------------------------------------

// buildFullReplay returns ALL events as messages. Only safe for short sessions.
func (s *Store) buildFullReplay(
	ctx context.Context,
	sessionID string,
	systemPrompt string,
	strategy *ContextStrategy,
) (*ContextWindow, error) {
	events, err := s.GetEvents(ctx, sessionID, nil)
	if err != nil {
		return nil, err
	}

	messages := eventsToMessages(events)

	return &ContextWindow{
		Messages:      messages,
		SystemPrompt:  systemPrompt,
		TokenEstimate: estimateMessageTokens(messages),
		FromIndex:     0,
		ToIndex:       lastEventIndex(events),
		WasCompacted:  false,
	}, nil
}

// ---------------------------------------------------------------------------
// ShouldCompact — Context anxiety prevention
// ---------------------------------------------------------------------------

// ShouldCompact checks if the current context window is approaching the
// model's limit (80% threshold). If so, the harness should trigger a
// context_reset event.
func ShouldCompact(tokenEstimate int, maxTokens int) bool {
	if maxTokens == 0 {
		maxTokens = 180000
	}
	return tokenEstimate > int(float64(maxTokens)*0.8)
}

// BuildCompactionSummary creates a summary of events that were compacted.
// This is the fast heuristic fallback. For model-based compaction, use
// CompactWithModel which calls a cheaper model (e.g. Haiku).
func BuildCompactionSummary(events []Event) string {
	var sb strings.Builder
	sb.WriteString("# Session Summary (Compacted)\n\n")

	// Count events by type
	counts := make(map[EventType]int)
	var toolCalls []string
	var lastAssistantMsg string
	var userMessages []string

	for _, evt := range events {
		counts[evt.Type]++
		switch evt.Type {
		case EventToolCall:
			toolCalls = append(toolCalls, evt.Data.ToolName)
		case EventAssistantMessage:
			lastAssistantMsg = evt.Data.Content
		case EventUserMessage:
			userMessages = append(userMessages, evt.Data.Content)
		}
	}

	sb.WriteString("## Event Counts\n")
	for t, c := range counts {
		fmt.Fprintf(&sb, "- %s: %d\n", t, c)
	}

	if len(toolCalls) > 0 {
		sb.WriteString("\n## Tools Used\n")
		seen := make(map[string]int)
		for _, t := range toolCalls {
			seen[t]++
		}
		for t, c := range seen {
			fmt.Fprintf(&sb, "- %s (×%d)\n", t, c)
		}
	}

	// Include user intent summary
	if len(userMessages) > 0 {
		sb.WriteString("\n## User Requests\n")
		for i, msg := range userMessages {
			if i >= 5 {
				fmt.Fprintf(&sb, "- ... and %d more\n", len(userMessages)-5)
				break
			}
			summary := msg
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			fmt.Fprintf(&sb, "- %s\n", summary)
		}
	}

	if lastAssistantMsg != "" {
		sb.WriteString("\n## Last Assistant Output\n")
		if len(lastAssistantMsg) > 2000 {
			lastAssistantMsg = lastAssistantMsg[:2000] + "..."
		}
		sb.WriteString(lastAssistantMsg)
	}

	return sb.String()
}

// CompactFunc is a function that generates a summary from events.
// The default is BuildCompactionSummary (heuristic). The harness can
// replace this with a model-based implementation.
type CompactFunc func(events []Event) string

// BuildCompactionPrompt creates a prompt suitable for sending to a cheaper
// model (e.g. Claude Haiku) to generate a compaction summary.
func BuildCompactionPrompt(events []Event) string {
	var sb strings.Builder
	sb.WriteString("Summarize this agent session conversation concisely. ")
	sb.WriteString("Focus on: (1) what the user asked for, (2) what tools were used and their results, ")
	sb.WriteString("(3) the current state and any pending work. Be factual and brief.\n\n")

	for _, evt := range events {
		switch evt.Type {
		case EventUserMessage:
			content := evt.Data.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&sb, "USER: %s\n", content)
		case EventAssistantMessage:
			content := evt.Data.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&sb, "ASSISTANT: %s\n", content)
		case EventToolCall:
			input := ""
			if evt.Data.Input != nil {
				inputBytes, _ := json.Marshal(evt.Data.Input)
				input = string(inputBytes)
				if len(input) > 200 {
					input = input[:200] + "..."
				}
			}
			fmt.Fprintf(&sb, "TOOL_CALL: %s(%s)\n", evt.Data.ToolName, input)
		case EventToolResult:
			output := ""
			if evt.Data.Output != "" {
				outputBytes, _ := json.Marshal(evt.Data.Output)
				output = string(outputBytes)
				if len(output) > 200 {
					output = output[:200] + "..."
				}
			}
			fmt.Fprintf(&sb, "TOOL_RESULT: %s\n", output)
		}
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Internal: event → message conversion
// ---------------------------------------------------------------------------

func eventsToMessages(events []Event) []AnthropicMessage {
	var messages []AnthropicMessage

	for _, evt := range events {
		switch evt.Type {
		case EventUserMessage:
			messages = append(messages, AnthropicMessage{
				Role:    "user",
				Content: evt.Data.Content,
			})

		case EventAssistantMessage:
			messages = append(messages, AnthropicMessage{
				Role:    "assistant",
				Content: evt.Data.Content,
			})

		case EventToolCall:
			// Tool calls are part of assistant messages in the Anthropic format.
			// Build a content block array.
			block := map[string]any{
				"type":  "tool_use",
				"id":    evt.Data.CallID,
				"name":  evt.Data.ToolName,
				"input": evt.Data.Input,
			}
			blockJSON, _ := json.Marshal([]any{block})
			messages = append(messages, AnthropicMessage{
				Role:    "assistant",
				Content: string(blockJSON),
			})

		case EventToolResult:
			// Tool results are user messages with tool_result content blocks.
			block := map[string]any{
				"type":       "tool_result",
				"tool_use_id": evt.Data.CallID,
				"content":    evt.Data.Output,
			}
			if evt.Data.IsError {
				block["is_error"] = true
			}
			blockJSON, _ := json.Marshal([]any{block})
			messages = append(messages, AnthropicMessage{
				Role:    "user",
				Content: string(blockJSON),
			})

		case EventContextReset:
			// Context resets become a user message with the summary
			messages = append(messages, AnthropicMessage{
				Role:    "user",
				Content: fmt.Sprintf("[Context compacted — %d events summarized]\n\n%s", evt.Data.CompactedRange[1]-evt.Data.CompactedRange[0], evt.Data.Summary),
			})

		case EventThinking:
			// Thinking blocks are not included in context window
			// (they're logged for debugging but not re-sent)

		case EventSystemEvent, EventCostEvent:
			// System/cost events are not part of the LLM context
		}
	}

	return messages
}

// ---------------------------------------------------------------------------
// Token estimation (heuristic — 1 token ≈ 4 chars)
// ---------------------------------------------------------------------------

func estimateTokens(s string) int {
	return len(s) / 4
}

func estimateMessageTokens(msgs []AnthropicMessage) int {
	total := 0
	for _, m := range msgs {
		total += estimateTokens(m.Content) + 4 // +4 for role/formatting overhead
	}
	return total
}

func trimToTokenBudget(msgs []AnthropicMessage, budget int) ([]AnthropicMessage, int) {
	total := estimateMessageTokens(msgs)
	if total <= budget {
		return msgs, 0
	}

	// Trim from the front (keep recent messages)
	trimmed := 0
	for total > budget && len(msgs) > 1 {
		total -= estimateTokens(msgs[0].Content) + 4
		msgs = msgs[1:]
		trimmed++
	}

	// Ensure first message is always "user" role (Anthropic requirement)
	if len(msgs) > 0 && msgs[0].Role != "user" {
		msgs = append([]AnthropicMessage{{
			Role:    "user",
			Content: "[Previous context trimmed — continuing session]",
		}}, msgs...)
	}

	return msgs, trimmed
}

func lastEventIndex(events []Event) int {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Index
}
