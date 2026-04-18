package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/crypto"
	"github.com/multica-ai/multica/server/internal/stream"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// v1 Managed Agents API — Additional handlers for Managed Agents parity
// ---------------------------------------------------------------------------

// requireUserID is defined in the original file — re-use via handler package.
// ctxWorkspaceID is also defined there.

// ---------------------------------------------------------------------------
// Agent Delete
// ---------------------------------------------------------------------------

func (h *Handler) DeleteManagedAgent(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentID := chi.URLParam(r, "agentId")

	if _, err := h.Queries.GetManagedAgentInWorkspace(r.Context(), db.GetManagedAgentInWorkspaceParams{
		ID:          parseUUID(agentID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if err := h.Queries.DeleteManagedAgent(r.Context(), parseUUID(agentID)); err != nil {
		writeError(w, http.StatusConflict, "agent has active sessions, archive instead")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Environment Update
// ---------------------------------------------------------------------------

func (h *Handler) UpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	envID := chi.URLParam(r, "envId")

	if _, err := h.Queries.GetEnvironmentInWorkspace(r.Context(), db.GetEnvironmentInWorkspaceParams{
		ID:          parseUUID(envID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "environment not found")
		return
	}

	var req struct {
		Name   *string         `json:"name"`
		Config json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateEnvironmentParams{ID: parseUUID(envID)}
	if req.Name != nil {
		params.Name = *req.Name
	}
	if req.Config != nil {
		params.Config = req.Config
	}

	env, err := h.Queries.UpdateEnvironment(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update environment")
		return
	}
	writeJSON(w, http.StatusOK, environmentToResponse(env))
}

// ---------------------------------------------------------------------------
// Environment Delete
// ---------------------------------------------------------------------------

func (h *Handler) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	envID := chi.URLParam(r, "envId")

	if _, err := h.Queries.GetEnvironmentInWorkspace(r.Context(), db.GetEnvironmentInWorkspaceParams{
		ID:          parseUUID(envID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "environment not found")
		return
	}

	if err := h.Queries.DeleteEnvironment(r.Context(), parseUUID(envID)); err != nil {
		writeError(w, http.StatusConflict, "environment has active sessions, archive instead")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Session Delete
// ---------------------------------------------------------------------------

func (h *Handler) DeleteManagedSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, err := h.Queries.GetManagedSessionInWorkspace(r.Context(), db.GetManagedSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if session.Status == "running" {
		writeError(w, http.StatusConflict, "cannot delete running session, send interrupt first")
		return
	}

	if err := h.Queries.DeleteManagedSession(r.Context(), parseUUID(sessionID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Session Send Events
// ---------------------------------------------------------------------------

func (h *Handler) SendSessionEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, err := h.Queries.GetManagedSessionInWorkspace(r.Context(), db.GetManagedSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if session.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "session is archived")
		return
	}

	var body struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var savedEvents []db.SessionEvent
	for _, raw := range body.Events {
		var evt struct {
			Type            string          `json:"type"`
			SessionThreadID *string         `json:"session_thread_id,omitempty"`
			Content         json.RawMessage `json:"content,omitempty"`
		}
		if err := json.Unmarshal(raw, &evt); err != nil {
			writeError(w, http.StatusBadRequest, "invalid event")
			return
		}

		var threadID pgtype.UUID
		if evt.SessionThreadID != nil {
			threadID = parseUUID(*evt.SessionThreadID)
		}

		saved, err := h.Queries.CreateSessionEvent(r.Context(), db.CreateSessionEventParams{
			SessionID: session.ID,
			ThreadID:  threadID,
			Type:      evt.Type,
			Payload:   raw,
		})
		if err != nil {
			slog.Error("failed to save session event", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to save event")
			return
		}
		savedEvents = append(savedEvents, saved)

		// Broadcast to SSE subscribers
		stream.Global.Broadcast(uuidToString(session.ID), stream.Event{
			Type:    saved.Type,
			Content: string(raw),
		})

		// Handle state transitions and trigger execution
		switch evt.Type {
		case "user.message":
			h.Queries.UpdateManagedSessionStatus(r.Context(), db.UpdateManagedSessionStatusParams{
				ID:     session.ID,
				Status: "running",
			})
			// Extract message content and trigger agentic execution
			var msgContent struct {
				Content string `json:"content"`
			}
			json.Unmarshal(raw, &msgContent)
			if msgContent.Content != "" {
				agent, err := h.Queries.GetManagedAgentInWorkspace(r.Context(), db.GetManagedAgentInWorkspaceParams{
					ID:          session.AgentID,
					WorkspaceID: parseUUID(workspaceID),
				})
				if err == nil {
					if execErr := h.ManagedSessionService.ExecuteSession(r.Context(), session, agent, msgContent.Content); execErr != nil {
						slog.Error("failed to execute session on user.message", "error", execErr)
					}
				}
			}
		case "user.interrupt":
			h.Queries.UpdateManagedSessionStatus(r.Context(), db.UpdateManagedSessionStatusParams{
				ID:     session.ID,
				Status: "idle",
			})
			stopReason, _ := json.Marshal(map[string]string{"type": "interrupted"})
			h.Queries.SetManagedSessionStopReason(r.Context(), session.ID, stopReason)
		case "user.tool_confirmation", "user.custom_tool_result":
			h.Queries.UpdateManagedSessionStatus(r.Context(), db.UpdateManagedSessionStatusParams{
				ID:     session.ID,
				Status: "running",
			})
		}
	}

	type EventResp struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		CreatedAt string `json:"created_at"`
	}
	resp := make([]EventResp, len(savedEvents))
	for i, e := range savedEvents {
		resp[i] = EventResp{
			ID:        uuidToString(e.ID),
			Type:      e.Type,
			CreatedAt: timestampToString(e.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "accepted", "events": resp})
}

// ---------------------------------------------------------------------------
// Resume Session — send a follow-up message to a completed/idle session
// ---------------------------------------------------------------------------

func (h *Handler) ResumeSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, err := h.Queries.GetManagedSessionInWorkspace(r.Context(), db.GetManagedSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if session.Status == "running" {
		writeError(w, http.StatusConflict, "session is already running")
		return
	}

	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Load the agent for this session
	agent, err := h.Queries.GetManagedAgentInWorkspace(r.Context(), db.GetManagedAgentInWorkspaceParams{
		ID:          session.AgentID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found for session")
		return
	}

	// Record the user message as an event
	payload, _ := json.Marshal(map[string]string{"content": body.Prompt})
	h.Queries.CreateSessionEvent(r.Context(), db.CreateSessionEventParams{
		SessionID: session.ID,
		Type:      "user.message",
		Payload:   payload,
	})

	// Re-launch execution
	if err := h.ManagedSessionService.ExecuteSession(r.Context(), session, agent, body.Prompt); err != nil {
		slog.Error("failed to resume session", "error", err, "session_id", sessionID)
		writeError(w, http.StatusInternalServerError, "failed to resume session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "running",
		"session_id": sessionID,
	})
}

// ---------------------------------------------------------------------------
// Session Threads
// ---------------------------------------------------------------------------

type SessionThreadResponse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func sessionThreadToResponse(t db.SessionThread) SessionThreadResponse {
	return SessionThreadResponse{
		ID:        uuidToString(t.ID),
		SessionID: uuidToString(t.SessionID),
		AgentID:   uuidToString(t.AgentID),
		AgentName: t.AgentName,
		Status:    t.Status,
		CreatedAt: timestampToString(t.CreatedAt),
	}
}

func (h *Handler) ListSessionThreads(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	if _, err := h.Queries.GetManagedSessionInWorkspace(r.Context(), db.GetManagedSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	threads, err := h.Queries.ListSessionThreads(r.Context(), parseUUID(sessionID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list threads")
		return
	}

	resp := make([]SessionThreadResponse, len(threads))
	for i, t := range threads {
		resp[i] = sessionThreadToResponse(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) ListThreadEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")
	threadID := chi.URLParam(r, "threadId")

	if _, err := h.Queries.GetManagedSessionInWorkspace(r.Context(), db.GetManagedSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	events, err := h.Queries.ListSessionEventsByThread(r.Context(), db.ListSessionEventsByThreadParams{
		SessionID: parseUUID(sessionID),
		ThreadID:  parseUUID(threadID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list thread events")
		return
	}

	type EventResponse struct {
		ID          string          `json:"id"`
		Type        string          `json:"type"`
		Payload     json.RawMessage `json:"payload"`
		ProcessedAt *string         `json:"processed_at,omitempty"`
		CreatedAt   string          `json:"created_at"`
	}

	resp := make([]EventResponse, len(events))
	for i, e := range events {
		er := EventResponse{
			ID:        uuidToString(e.ID),
			Type:      e.Type,
			Payload:   e.Payload,
			CreatedAt: timestampToString(e.CreatedAt),
		}
		if e.ProcessedAt.Valid {
			s := timestampToString(e.ProcessedAt)
			er.ProcessedAt = &s
		}
		resp[i] = er
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) StreamSessionThread(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")
	threadID := chi.URLParam(r, "threadId")

	if _, err := h.Queries.GetManagedSessionInWorkspace(r.Context(), db.GetManagedSessionInWorkspaceParams{
		ID:          parseUUID(sessionID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	// Use thread-specific key for pub/sub
	streamKey := sessionID + ":" + threadID
	ch := stream.Global.Subscribe(streamKey)
	defer stream.Global.Unsubscribe(streamKey, ch)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// ---------------------------------------------------------------------------
// Memory CRUD (within a store)
// ---------------------------------------------------------------------------

type MemoryResponse struct {
	ID               string `json:"id"`
	StoreID          string `json:"store_id"`
	Path             string `json:"path"`
	Content          string `json:"content"`
	ContentSha256    string `json:"content_sha256"`
	ContentSizeBytes int32  `json:"content_size_bytes"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

func memoryToResponse(m db.Memory) MemoryResponse {
	return MemoryResponse{
		ID:               uuidToString(m.ID),
		StoreID:          uuidToString(m.StoreID),
		Path:             m.Path,
		Content:          m.Content,
		ContentSha256:    m.ContentSha256,
		ContentSizeBytes: m.ContentSizeBytes,
		CreatedAt:        timestampToString(m.CreatedAt),
		UpdatedAt:        timestampToString(m.UpdatedAt),
	}
}

func computeSha256(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func (h *Handler) ListMemories(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")

	if _, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	limit := int32(100)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := parseInt32(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := parseInt32(o); err == nil && v >= 0 {
			offset = v
		}
	}

	memories, err := h.Queries.ListMemories(r.Context(), db.ListMemoriesParams{
		StoreID: parseUUID(storeID),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list memories")
		return
	}

	resp := make([]MemoryResponse, len(memories))
	for i, m := range memories {
		resp[i] = memoryToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) WriteMemory(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")

	store, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	var req struct {
		Path         string `json:"path"`
		Content      string `json:"content"`
		Precondition *struct {
			Type          string `json:"type"`
			ContentSha256 string `json:"content_sha256,omitempty"`
		} `json:"precondition,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if len(req.Content) > 100*1024 {
		writeError(w, http.StatusBadRequest, "content exceeds 100KB limit")
		return
	}

	contentHash := computeSha256(req.Content)
	contentSize := int32(len(req.Content))

	// Check if path already exists (upsert semantics)
	existing, err := h.Queries.GetMemoryByPath(r.Context(), db.GetMemoryByPathParams{
		StoreID: store.ID,
		Path:    req.Path,
	})
	if err == nil {
		// Path exists — update (unless precondition says not_exists)
		if req.Precondition != nil && req.Precondition.Type == "not_exists" {
			writeError(w, http.StatusConflict, "memory already exists at this path")
			return
		}

		updated, err := h.Queries.UpdateMemory(r.Context(), db.UpdateMemoryParams{
			ID:               existing.ID,
			Content:          req.Content,
			ContentSha256:    contentHash,
			ContentSizeBytes: contentSize,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update memory")
			return
		}

		// Record version
		h.Queries.CreateMemoryVersion(r.Context(), db.CreateMemoryVersionParams{
			MemoryID:         updated.ID,
			StoreID:          store.ID,
			Operation:        "modified",
			Content:          pgtype.Text{String: req.Content, Valid: true},
			ContentSha256:    pgtype.Text{String: contentHash, Valid: true},
			ContentSizeBytes: pgtype.Int4{Int32: contentSize, Valid: true},
			Path:             req.Path,
		})

		writeJSON(w, http.StatusOK, memoryToResponse(updated))
		return
	}

	// Path doesn't exist — create
	mem, err := h.Queries.CreateMemory(r.Context(), db.CreateMemoryParams{
		StoreID:          store.ID,
		Path:             req.Path,
		Content:          req.Content,
		ContentSha256:    contentHash,
		ContentSizeBytes: contentSize,
	})
	if err != nil {
		slog.Error("failed to create memory", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create memory")
		return
	}

	// Record version
	h.Queries.CreateMemoryVersion(r.Context(), db.CreateMemoryVersionParams{
		MemoryID:         mem.ID,
		StoreID:          store.ID,
		Operation:        "created",
		Content:          pgtype.Text{String: req.Content, Valid: true},
		ContentSha256:    pgtype.Text{String: contentHash, Valid: true},
		ContentSizeBytes: pgtype.Int4{Int32: contentSize, Valid: true},
		Path:             req.Path,
	})

	writeJSON(w, http.StatusCreated, memoryToResponse(mem))
}

func (h *Handler) ReadMemory(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")
	memID := chi.URLParam(r, "memId")

	if _, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	mem, err := h.Queries.GetMemory(r.Context(), parseUUID(memID))
	if err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}
	if uuidToString(mem.StoreID) != storeID {
		writeError(w, http.StatusNotFound, "memory not found in this store")
		return
	}

	writeJSON(w, http.StatusOK, memoryToResponse(mem))
}

func (h *Handler) UpdateMemory(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")
	memID := chi.URLParam(r, "memId")

	store, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	mem, err := h.Queries.GetMemory(r.Context(), parseUUID(memID))
	if err != nil || uuidToString(mem.StoreID) != storeID {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}

	var req struct {
		Path         *string `json:"path,omitempty"`
		Content      *string `json:"content,omitempty"`
		Precondition *struct {
			Type          string `json:"type"`
			ContentSha256 string `json:"content_sha256"`
		} `json:"precondition,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Check optimistic concurrency
	if req.Precondition != nil && req.Precondition.Type == "content_sha256" {
		if mem.ContentSha256 != req.Precondition.ContentSha256 {
			writeError(w, http.StatusConflict, "content_sha256 mismatch, re-read and retry")
			return
		}
	}

	if req.Content != nil {
		if len(*req.Content) > 100*1024 {
			writeError(w, http.StatusBadRequest, "content exceeds 100KB limit")
			return
		}
		contentHash := computeSha256(*req.Content)
		contentSize := int32(len(*req.Content))

		updated, err := h.Queries.UpdateMemory(r.Context(), db.UpdateMemoryParams{
			ID:               mem.ID,
			Content:          *req.Content,
			ContentSha256:    contentHash,
			ContentSizeBytes: contentSize,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update memory")
			return
		}

		h.Queries.CreateMemoryVersion(r.Context(), db.CreateMemoryVersionParams{
			MemoryID:         updated.ID,
			StoreID:          store.ID,
			Operation:        "modified",
			Content:          pgtype.Text{String: *req.Content, Valid: true},
			ContentSha256:    pgtype.Text{String: contentHash, Valid: true},
			ContentSizeBytes: pgtype.Int4{Int32: contentSize, Valid: true},
			Path:             updated.Path,
		})

		writeJSON(w, http.StatusOK, memoryToResponse(updated))
		return
	}

	// Path-only rename not supported via simple update (would need separate SQL)
	writeJSON(w, http.StatusOK, memoryToResponse(mem))
}

func (h *Handler) DeleteMemory(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")
	memID := chi.URLParam(r, "memId")

	store, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	mem, err := h.Queries.GetMemory(r.Context(), parseUUID(memID))
	if err != nil || uuidToString(mem.StoreID) != storeID {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}

	// Record deletion version before deleting
	h.Queries.CreateMemoryVersion(r.Context(), db.CreateMemoryVersionParams{
		MemoryID:  mem.ID,
		StoreID:   store.ID,
		Operation: "deleted",
		Path:      mem.Path,
	})

	if err := h.Queries.DeleteMemory(r.Context(), parseUUID(memID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete memory")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Memory Versions
// ---------------------------------------------------------------------------

type MemoryVersionResponse struct {
	ID               string  `json:"id"`
	MemoryID         *string `json:"memory_id"`
	StoreID          string  `json:"store_id"`
	Operation        string  `json:"operation"`
	Content          *string `json:"content,omitempty"`
	ContentSha256    *string `json:"content_sha256,omitempty"`
	ContentSizeBytes *int32  `json:"content_size_bytes,omitempty"`
	Path             string  `json:"path"`
	SessionID        *string `json:"session_id,omitempty"`
	CreatedAt        string  `json:"created_at"`
	RedactedAt       *string `json:"redacted_at,omitempty"`
}

func memoryVersionToResponse(v db.MemoryVersion) MemoryVersionResponse {
	resp := MemoryVersionResponse{
		ID:        uuidToString(v.ID),
		StoreID:   uuidToString(v.StoreID),
		Operation: v.Operation,
		Path:      v.Path,
		CreatedAt: timestampToString(v.CreatedAt),
	}
	if v.MemoryID.Valid {
		s := uuidToString(v.MemoryID)
		resp.MemoryID = &s
	}
	if v.Content.Valid {
		resp.Content = &v.Content.String
	}
	if v.ContentSha256.Valid {
		resp.ContentSha256 = &v.ContentSha256.String
	}
	if v.ContentSizeBytes.Valid {
		resp.ContentSizeBytes = &v.ContentSizeBytes.Int32
	}
	if v.SessionID.Valid {
		s := uuidToString(v.SessionID)
		resp.SessionID = &s
	}
	if v.RedactedAt.Valid {
		s := timestampToString(v.RedactedAt)
		resp.RedactedAt = &s
	}
	return resp
}

func (h *Handler) ListMemoryVersions(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")

	if _, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	limit := int32(100)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := parseInt32(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := parseInt32(o); err == nil && v >= 0 {
			offset = v
		}
	}

	versions, err := h.Queries.ListMemoryVersions(r.Context(), db.ListMemoryVersionsParams{
		StoreID: parseUUID(storeID),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}

	resp := make([]MemoryVersionResponse, len(versions))
	for i, v := range versions {
		resp[i] = memoryVersionToResponse(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetMemoryVersion(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")
	versionID := chi.URLParam(r, "versionId")

	if _, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	ver, err := h.Queries.GetMemoryVersion(r.Context(), parseUUID(versionID))
	if err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	if uuidToString(ver.StoreID) != storeID {
		writeError(w, http.StatusNotFound, "version not found in this store")
		return
	}

	writeJSON(w, http.StatusOK, memoryVersionToResponse(ver))
}

func (h *Handler) RedactMemoryVersion(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")
	versionID := chi.URLParam(r, "versionId")

	if _, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	ver, err := h.Queries.GetMemoryVersion(r.Context(), parseUUID(versionID))
	if err != nil || uuidToString(ver.StoreID) != storeID {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}

	if err := h.Queries.RedactMemoryVersion(r.Context(), parseUUID(versionID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redact version")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ArchiveMemoryStore(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	storeID := chi.URLParam(r, "storeId")

	if _, err := h.Queries.GetMemoryStoreInWorkspace(r.Context(), db.GetMemoryStoreInWorkspaceParams{
		ID:          parseUUID(storeID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "memory store not found")
		return
	}

	if err := h.Queries.ArchiveMemoryStore(r.Context(), parseUUID(storeID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive memory store")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Vault Archive & Delete
// ---------------------------------------------------------------------------

func (h *Handler) ArchiveVault(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	vaultID := chi.URLParam(r, "vaultId")

	if _, err := h.Queries.GetVaultInWorkspace(r.Context(), db.GetVaultInWorkspaceParams{
		ID:          parseUUID(vaultID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "vault not found")
		return
	}

	if err := h.Queries.ArchiveVault(r.Context(), parseUUID(vaultID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive vault")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteVault(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	vaultID := chi.URLParam(r, "vaultId")

	if _, err := h.Queries.GetVaultInWorkspace(r.Context(), db.GetVaultInWorkspaceParams{
		ID:          parseUUID(vaultID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "vault not found")
		return
	}

	if err := h.Queries.DeleteVault(r.Context(), parseUUID(vaultID)); err != nil {
		writeError(w, http.StatusConflict, "cannot delete vault with active credentials")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Vault Credentials
// ---------------------------------------------------------------------------

type VaultCredentialResponse struct {
	ID           string  `json:"id"`
	VaultID      string  `json:"vault_id"`
	McpServerUrl string  `json:"mcp_server_url"`
	AuthType     string  `json:"auth_type"`
	ExpiresAt    *string `json:"expires_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
	ArchivedAt   *string `json:"archived_at,omitempty"`
}

func credentialRowToResponse(c db.ListVaultCredentialsRow) VaultCredentialResponse {
	resp := VaultCredentialResponse{
		ID:           uuidToString(c.ID),
		VaultID:      uuidToString(c.VaultID),
		McpServerUrl: c.McpServerUrl,
		AuthType:     c.AuthType,
		CreatedAt:    timestampToString(c.CreatedAt),
	}
	if c.ExpiresAt.Valid {
		s := timestampToString(c.ExpiresAt)
		resp.ExpiresAt = &s
	}
	if c.ArchivedAt.Valid {
		s := timestampToString(c.ArchivedAt)
		resp.ArchivedAt = &s
	}
	return resp
}

func (h *Handler) ListVaultCredentials(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	vaultID := chi.URLParam(r, "vaultId")

	if _, err := h.Queries.GetVaultInWorkspace(r.Context(), db.GetVaultInWorkspaceParams{
		ID:          parseUUID(vaultID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "vault not found")
		return
	}

	creds, err := h.Queries.ListVaultCredentials(r.Context(), parseUUID(vaultID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list credentials")
		return
	}

	resp := make([]VaultCredentialResponse, len(creds))
	for i, c := range creds {
		resp[i] = credentialRowToResponse(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) AddVaultCredential(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	vaultID := chi.URLParam(r, "vaultId")

	vault, err := h.Queries.GetVaultInWorkspace(r.Context(), db.GetVaultInWorkspaceParams{
		ID:          parseUUID(vaultID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "vault not found")
		return
	}
	if vault.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "vault is archived")
		return
	}

	// Check max 20 credentials
	count, _ := h.Queries.CountActiveVaultCredentials(r.Context(), vault.ID)
	if count >= 20 {
		writeError(w, http.StatusBadRequest, "max 20 credentials per vault")
		return
	}

	var req struct {
		McpServerUrl string          `json:"mcp_server_url"`
		Auth         json.RawMessage `json:"auth"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.McpServerUrl == "" {
		writeError(w, http.StatusBadRequest, "mcp_server_url is required")
		return
	}

	// Parse auth type
	var authInfo struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(req.Auth, &authInfo); err != nil {
		writeError(w, http.StatusBadRequest, "invalid auth object")
		return
	}
	if authInfo.Type != "mcp_oauth" && authInfo.Type != "bearer" {
		writeError(w, http.StatusBadRequest, "auth.type must be 'mcp_oauth' or 'bearer'")
		return
	}

	// Encrypt the auth payload
	encrypted, err := crypto.Encrypt(string(req.Auth))
	if err != nil {
		slog.Error("failed to encrypt credential", "error", err)
		writeError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	cred, err := h.Queries.CreateVaultCredential(r.Context(), db.CreateVaultCredentialParams{
		VaultID:          vault.ID,
		McpServerUrl:     req.McpServerUrl,
		AuthType:         authInfo.Type,
		EncryptedPayload: []byte(encrypted),
	})
	if err != nil {
		// Check for unique constraint violation
		writeError(w, http.StatusConflict, "credential already exists for this mcp_server_url")
		return
	}

	writeJSON(w, http.StatusCreated, VaultCredentialResponse{
		ID:           uuidToString(cred.ID),
		VaultID:      uuidToString(cred.VaultID),
		McpServerUrl: cred.McpServerUrl,
		AuthType:     cred.AuthType,
		CreatedAt:    timestampToString(cred.CreatedAt),
	})
}

func (h *Handler) ArchiveVaultCredential(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	vaultID := chi.URLParam(r, "vaultId")
	credID := chi.URLParam(r, "credId")

	if _, err := h.Queries.GetVaultInWorkspace(r.Context(), db.GetVaultInWorkspaceParams{
		ID:          parseUUID(vaultID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "vault not found")
		return
	}

	if err := h.Queries.ArchiveVaultCredential(r.Context(), parseUUID(credID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive credential")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
