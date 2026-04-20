// Package session implements the Session Store from Anthropic's Managed Agents
// architecture. The session is an append-only event log that lives OUTSIDE
// Claude's context window. The harness reads/writes events through this
// interface, and any harness instance can resume any session via Wake().
//
// Key design principles:
//   - Append-only: events are immutable once written
//   - Positional: getEvents(from, to) returns a slice by event_index
//   - Durable: every event is persisted to Postgres before ACK
//   - Streamable: events are broadcast via SSE + WebSocket in real-time
//   - Cost-tracked: every event carries optional cost metadata
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/stream"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Types — Event types matching Anthropic Managed Agents spec
// ---------------------------------------------------------------------------

// EventType enumerates all session event types.
type EventType string

const (
	EventUserMessage      EventType = "user_message"
	EventAssistantMessage EventType = "assistant_message"
	EventToolCall         EventType = "tool_call"
	EventToolResult       EventType = "tool_result"
	EventContextReset     EventType = "context_reset"
	EventSystemEvent      EventType = "system_event"
	EventCostEvent        EventType = "cost_event"
	EventThinking         EventType = "thinking"
)

// Event is a single entry in the append-only session log.
type Event struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Type      EventType `json:"type"`
	Index     int       `json:"index"`
	Timestamp string    `json:"timestamp"`
	Data      EventData `json:"data"`
	Metadata  *EventMeta `json:"metadata,omitempty"`
}

// EventData holds the content of an event.
type EventData struct {
	// For user_message / assistant_message
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`

	// For tool_call
	ToolName string         `json:"tool_name,omitempty"`
	CallID   string         `json:"call_id,omitempty"`
	Input    map[string]any `json:"input,omitempty"`

	// For tool_result
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"is_error,omitempty"`

	// For context_reset
	Summary        string `json:"summary,omitempty"`
	CompactedRange [2]int `json:"compacted_range,omitempty"` // [fromIndex, toIndex)

	// For system_event
	EventName string `json:"event_name,omitempty"`
	Details   string `json:"details,omitempty"`

	// For thinking
	Thinking string `json:"thinking,omitempty"`

	// Raw — for extensibility
	Raw map[string]any `json:"raw,omitempty"`
}

// EventMeta holds optional cost/performance metadata per event.
type EventMeta struct {
	TokensInput  int64   `json:"tokens_input,omitempty"`
	TokensOutput int64   `json:"tokens_output,omitempty"`
	TokensCached int64   `json:"tokens_cached,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Provider     string  `json:"provider,omitempty"`
	Model        string  `json:"model,omitempty"`
	DurationMs   int64   `json:"duration_ms,omitempty"`
}

// SessionConfig is passed when creating a new session.
type SessionConfig struct {
	WorkspaceID     string
	AgentID         string
	AgentVersion    int
	EnvironmentID   string
	Title           string
	ContextStrategy *ContextStrategy
}

// ContextStrategy controls how the harness builds the context window.
type ContextStrategy struct {
	Type          string `json:"type"`           // "sliding_window" | "smart_summary" | "full_replay"
	MaxTokens     int    `json:"max_tokens"`
	SummaryModel  string `json:"summary_model,omitempty"` // cheaper model for compaction
}

// SessionInfo is a lightweight view of a session's state.
type SessionInfo struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	AgentID         string          `json:"agent_id"`
	Status          string          `json:"status"`
	LastEventIndex  int             `json:"last_event_index"`
	WakeCount       int             `json:"wake_count"`
	TotalCostUSD    float64         `json:"total_cost_usd"`
	ContextStrategy *ContextStrategy `json:"context_strategy"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

// GetEventsOptions filters events in a getEvents call.
type GetEventsOptions struct {
	From  int        // index inclusive (default 0)
	To    int        // index exclusive (default MaxInt = all)
	Types []EventType // filter by type (nil = all)
}

// ---------------------------------------------------------------------------
// Store — The core Session Store interface
// ---------------------------------------------------------------------------

// Store is the Session Store from Anthropic's Managed Agents architecture.
// It provides an append-only, durable, positionally-addressable event log
// that lives outside Claude's context window.
type Store struct {
	queries *db.Queries
	hub     *realtime.Hub
	logger  *slog.Logger

	// In-memory event index counters per session (for fast atomic increment)
	mu       sync.Mutex
	counters map[string]int // sessionID → next event_index
}

// NewStore creates a Session Store.
func NewStore(q *db.Queries, hub *realtime.Hub, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		queries:  q,
		hub:      hub,
		logger:   logger,
		counters: make(map[string]int),
	}
}

// Queries returns the underlying database queries for direct access when needed.
func (s *Store) Queries() *db.Queries {
	return s.queries
}

// ---------------------------------------------------------------------------
// Create — Start a new session
// ---------------------------------------------------------------------------

// Create starts a new session and returns its ID.
func (s *Store) Create(ctx context.Context, cfg SessionConfig) (string, error) {
	strategy := &ContextStrategy{Type: "sliding_window", MaxTokens: 180000}
	if cfg.ContextStrategy != nil {
		strategy = cfg.ContextStrategy
	}
	strategyJSON, _ := json.Marshal(strategy)

	row, err := s.queries.CreateManagedSession(ctx, db.CreateManagedSessionParams{
		WorkspaceID:  util.ParseUUID(cfg.WorkspaceID),
		AgentID:      util.ParseUUID(cfg.AgentID),
		AgentVersion: int32(cfg.AgentVersion),
		EnvironmentID: pgtype.UUID{Valid: cfg.EnvironmentID != ""},
		Title:        pgtype.Text{String: cfg.Title, Valid: cfg.Title != ""},
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	sessionID := util.UUIDToString(row.ID)

	// Set context strategy (separate update since CreateManagedSession doesn't include it)
	_ = s.queries.SetManagedSessionContextStrategy(ctx, row.ID, strategyJSON)

	// Initialize counter
	s.mu.Lock()
	s.counters[sessionID] = 0
	s.mu.Unlock()

	s.logger.Info("session created", "session_id", sessionID, "agent_id", cfg.AgentID)
	return sessionID, nil
}

// ---------------------------------------------------------------------------
// Get — Retrieve session info
// ---------------------------------------------------------------------------

// Get returns info about a session.
func (s *Store) Get(ctx context.Context, sessionID string) (*SessionInfo, error) {
	row, err := s.queries.GetManagedSession(ctx, util.ParseUUID(sessionID))
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return sessionInfoFromRow(row), nil
}

// ---------------------------------------------------------------------------
// AppendEvent — Atomically append an event to the session log
// ---------------------------------------------------------------------------

// AppendEvent appends an event to the session log. The event_index is
// assigned atomically and monotonically. Returns the assigned index.
func (s *Store) AppendEvent(ctx context.Context, sessionID string, evt Event) (int, error) {
	// Get next index atomically
	idx := s.nextIndex(sessionID)

	// Build payload
	payload, _ := json.Marshal(evt.Data)
	var metadataJSON []byte
	if evt.Metadata != nil {
		metadataJSON, _ = json.Marshal(evt.Metadata)
	}

	// Persist to Postgres (durable before ACK)
	sid := util.ParseUUID(sessionID)
	_, err := s.queries.CreateSessionEventWithIndex(ctx, db.CreateSessionEventWithIndexParams{
		SessionID:  sid,
		Type:       string(evt.Type),
		Payload:    payload,
		EventIndex: int32(idx),
		Metadata:   metadataJSON,
	})
	if err != nil {
		return -1, fmt.Errorf("append event: %w", err)
	}

	// Update session's last_event_index
	_ = s.queries.UpdateManagedSessionEventIndex(ctx, sid, int32(idx))

	// Stream in real-time via SSE + WebSocket
	s.broadcastEvent(sessionID, evt, idx)

	return idx, nil
}

// ---------------------------------------------------------------------------
// GetEvents — Positional slice of the event log
// ---------------------------------------------------------------------------

// GetEvents returns a slice of events by position. This is the core
// interface that makes the session an external context object.
//
//   getEvents(sessionId)                    → all events
//   getEvents(sessionId, {from: 10})        → events from index 10 onwards
//   getEvents(sessionId, {from: 5, to: 15}) → events [5, 15)
//   getEvents(sessionId, {types: ["tool_call", "tool_result"]}) → filtered
func (s *Store) GetEvents(ctx context.Context, sessionID string, opts *GetEventsOptions) ([]Event, error) {
	sid := util.ParseUUID(sessionID)

	from := 0
	to := 1<<31 - 1 // MaxInt
	if opts != nil {
		if opts.From > 0 {
			from = opts.From
		}
		if opts.To > 0 {
			to = opts.To
		}
	}

	var rows []db.SessionEvent
	var err error

	if opts != nil && len(opts.Types) > 0 {
		typeStrs := make([]string, len(opts.Types))
		for i, t := range opts.Types {
			typeStrs[i] = string(t)
		}
		rows, err = s.queries.GetSessionEventsByType(ctx, db.GetSessionEventsByTypeParams{
			SessionID:  sid,
			EventIndex: int32(from),
			EventIndex_2: int32(to),
			Column4:    typeStrs,
		})
	} else {
		rows, err = s.queries.GetSessionEventsSlice(ctx, db.GetSessionEventsSliceParams{
			SessionID:  sid,
			EventIndex: int32(from),
			EventIndex_2: int32(to),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}

	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventFromRow(row))
	}
	return events, nil
}

// ---------------------------------------------------------------------------
// Wake — Crash recovery
// ---------------------------------------------------------------------------

// Wake recovers a session after a crash. It reads the event log to determine
// the last state, increments the wake counter, and returns the session info.
// A new harness instance calls this to resume where the previous one left off.
func (s *Store) Wake(ctx context.Context, sessionID string) (*SessionInfo, error) {
	sid := util.ParseUUID(sessionID)

	// Get the max event index from the DB (source of truth)
	maxIdx, err := s.queries.GetSessionMaxEventIndex(ctx, sid)
	if err != nil {
		return nil, fmt.Errorf("wake: get max index: %w", err)
	}

	// Initialize the in-memory counter from the DB
	s.mu.Lock()
	s.counters[sessionID] = int(maxIdx) + 1
	s.mu.Unlock()

	// Update session status to running + increment wake count
	row, err := s.queries.WakeManagedSession(ctx, sid)
	if err != nil {
		return nil, fmt.Errorf("wake session: %w", err)
	}

	s.logger.Info("session woken",
		"session_id", sessionID,
		"wake_count", row.WakeCount,
		"last_event_index", maxIdx,
	)

	return sessionInfoFromRow(row), nil
}

// ---------------------------------------------------------------------------
// Close — Terminate a session
// ---------------------------------------------------------------------------

// Close terminates a session with an outcome.
func (s *Store) Close(ctx context.Context, sessionID string, status string, stopReason map[string]any) error {
	sid := util.ParseUUID(sessionID)

	// Append a system event recording the close
	s.AppendEvent(ctx, sessionID, Event{
		Type: EventSystemEvent,
		Data: EventData{
			EventName: "session_closed",
			Details:   status,
		},
	})

	// Update status
	_, _ = s.queries.UpdateManagedSessionStatus(ctx, db.UpdateManagedSessionStatusParams{
		ID:     sid,
		Status: status,
	})

	// Set stop reason
	if stopReason != nil {
		sr, _ := json.Marshal(stopReason)
		_ = s.queries.SetManagedSessionStopReason(ctx, sid, sr)
	}

	// Clean up counter
	s.mu.Lock()
	delete(s.counters, sessionID)
	s.mu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// GetLastContextReset — Find the most recent compaction point
// ---------------------------------------------------------------------------

// GetLastContextReset returns the most recent context_reset event, or nil
// if no compaction has occurred. The harness uses this to know where to
// start building the context window after a wake.
func (s *Store) GetLastContextReset(ctx context.Context, sessionID string) (*Event, error) {
	row, err := s.queries.GetLastContextReset(ctx, util.ParseUUID(sessionID))
	if err != nil {
		return nil, nil // No reset found is not an error
	}
	evt := eventFromRow(row)
	return &evt, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// nextIndex atomically returns the next event_index for a session.
func (s *Store) nextIndex(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.counters[sessionID]
	s.counters[sessionID] = idx + 1
	return idx
}

// InitCounter loads the current event index from DB (call on startup or wake).
func (s *Store) InitCounter(ctx context.Context, sessionID string) error {
	maxIdx, err := s.queries.GetSessionMaxEventIndex(ctx, util.ParseUUID(sessionID))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.counters[sessionID] = int(maxIdx) + 1
	s.mu.Unlock()
	return nil
}

func (s *Store) broadcastEvent(sessionID string, evt Event, idx int) {
	// SSE stream
	stream.Global.Broadcast(sessionID, stream.Event{
		Type:    string(evt.Type),
		Content: formatEventContent(evt),
	})

	// WebSocket — broadcast session event to workspace clients
	// Clients filter by session_id on their end
	msg, _ := json.Marshal(map[string]any{
		"type":        "session.event",
		"session_id":  sessionID,
		"event_type":  string(evt.Type),
		"event_index": idx,
		"data":        evt.Data,
		"metadata":    evt.Metadata,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	if s.hub != nil {
		s.hub.Broadcast(msg)
	}
}

func formatEventContent(evt Event) string {
	switch evt.Type {
	case EventUserMessage:
		return evt.Data.Content
	case EventAssistantMessage:
		return evt.Data.Content
	case EventToolCall:
		data, _ := json.Marshal(map[string]any{
			"tool":    evt.Data.ToolName,
			"call_id": evt.Data.CallID,
			"input":   evt.Data.Input,
		})
		return string(data)
	case EventToolResult:
		data, _ := json.Marshal(map[string]any{
			"tool":    evt.Data.ToolName,
			"call_id": evt.Data.CallID,
			"output":  evt.Data.Output,
		})
		return string(data)
	case EventContextReset:
		return evt.Data.Summary
	case EventThinking:
		return evt.Data.Thinking
	default:
		return evt.Data.Details
	}
}

func eventFromRow(row db.SessionEvent) Event {
	var data EventData
	if row.Payload != nil {
		json.Unmarshal(row.Payload, &data)
	}
	var meta *EventMeta
	if row.Metadata != nil {
		meta = &EventMeta{}
		json.Unmarshal(row.Metadata, meta)
	}

	idx := 0
	if row.EventIndex.Valid {
		idx = int(row.EventIndex.Int32)
	}

	return Event{
		ID:        util.UUIDToString(row.ID),
		SessionID: util.UUIDToString(row.SessionID),
		Type:      EventType(row.Type),
		Index:     idx,
		Timestamp: row.CreatedAt.Time.Format(time.RFC3339),
		Data:      data,
		Metadata:  meta,
	}
}

func sessionInfoFromRow(row db.ManagedSession) *SessionInfo {
	var strategy *ContextStrategy
	if row.ContextStrategy != nil {
		strategy = &ContextStrategy{}
		json.Unmarshal(row.ContextStrategy, strategy)
	}

	costUSD := 0.0
	// total_cost_usd is a NUMERIC which may be returned as string
	// Handle via the pgtype Numeric if available
	return &SessionInfo{
		ID:              util.UUIDToString(row.ID),
		WorkspaceID:     util.UUIDToString(row.WorkspaceID),
		AgentID:         util.UUIDToString(row.AgentID),
		Status:          row.Status,
		LastEventIndex:  int(row.LastEventIndex),
		WakeCount:       int(row.WakeCount),
		TotalCostUSD:    costUSD,
		ContextStrategy: strategy,
		CreatedAt:       row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
