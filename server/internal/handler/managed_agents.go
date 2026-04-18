package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/stream"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// v1 Managed Agents API — Claude Managed Agents compatible
// ---------------------------------------------------------------------------

type ManagedAgentResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    *string         `json:"description"`
	Model          json.RawMessage `json:"model"`
	SystemPrompt   *string         `json:"system_prompt"`
	Tools          json.RawMessage `json:"tools"`
	McpServers     json.RawMessage `json:"mcp_servers"`
	Skills         json.RawMessage `json:"skills"`
	CallableAgents json.RawMessage `json:"callable_agents"`
	Metadata       json.RawMessage `json:"metadata"`
	Version        int32           `json:"version"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	ArchivedAt     *string         `json:"archived_at,omitempty"`
}

func managedAgentToResponse(a db.ManagedAgent) ManagedAgentResponse {
	resp := ManagedAgentResponse{
		ID:             uuidToString(a.ID),
		Name:           a.Name,
		Model:          a.Model,
		Tools:          a.Tools,
		McpServers:     a.McpServers,
		Skills:         a.Skills,
		CallableAgents: a.CallableAgents,
		Metadata:       a.Metadata,
		Version:        a.Version,
		CreatedAt:      timestampToString(a.CreatedAt),
		UpdatedAt:      timestampToString(a.UpdatedAt),
	}
	if a.Description.Valid {
		resp.Description = &a.Description.String
	}
	if a.SystemPrompt.Valid {
		resp.SystemPrompt = &a.SystemPrompt.String
	}
	if a.ArchivedAt.Valid {
		s := timestampToString(a.ArchivedAt)
		resp.ArchivedAt = &s
	}
	return resp
}

// --- CRUD ---

type CreateManagedAgentRequest struct {
	Name           string          `json:"name"`
	Description    *string         `json:"description"`
	Model          json.RawMessage `json:"model"`
	SystemPrompt   *string         `json:"system_prompt"`
	Tools          json.RawMessage `json:"tools"`
	McpServers     json.RawMessage `json:"mcp_servers"`
	Skills         json.RawMessage `json:"skills"`
	CallableAgents json.RawMessage `json:"callable_agents"`
	Metadata       json.RawMessage `json:"metadata"`
}

func (h *Handler) CreateManagedAgent(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateManagedAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	params := db.CreateManagedAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
	}
	if req.Description != nil {
		params.Description.String = *req.Description
		params.Description.Valid = true
	}
	if req.Model != nil {
		params.Model = req.Model
	} else {
		params.Model = []byte(`{"id":"claude-sonnet-4-20250514","speed":"standard"}`)
	}
	if req.SystemPrompt != nil {
		params.SystemPrompt.String = *req.SystemPrompt
		params.SystemPrompt.Valid = true
	}
	if req.Tools != nil {
		params.Tools = req.Tools
	} else {
		params.Tools = []byte(`[]`)
	}
	if req.McpServers != nil {
		params.McpServers = req.McpServers
	} else {
		params.McpServers = []byte(`[]`)
	}
	if req.Skills != nil {
		params.Skills = req.Skills
	} else {
		params.Skills = []byte(`[]`)
	}
	if req.CallableAgents != nil {
		params.CallableAgents = req.CallableAgents
	} else {
		params.CallableAgents = []byte(`[]`)
	}
	if req.Metadata != nil {
		params.Metadata = req.Metadata
	} else {
		params.Metadata = []byte(`{}`)
	}

	agent, err := h.Queries.CreateManagedAgent(r.Context(), params)
	if err != nil {
		slog.Error("failed to create managed agent", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create agent")
		return
	}

	// Save version snapshot
	snapshot, _ := json.Marshal(managedAgentToResponse(agent))
	h.Queries.CreateManagedAgentVersion(r.Context(), db.CreateManagedAgentVersionParams{
		AgentID:  agent.ID,
		Version:  agent.Version,
		Snapshot: snapshot,
	})

	writeJSON(w, http.StatusCreated, managedAgentToResponse(agent))
}

func (h *Handler) ListManagedAgents(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	agents, err := h.Queries.ListManagedAgents(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	resp := make([]ManagedAgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = managedAgentToResponse(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetManagedAgent(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentID := chi.URLParam(r, "agentId")

	agent, err := h.Queries.GetManagedAgentInWorkspace(r.Context(), db.GetManagedAgentInWorkspaceParams{
		ID:          parseUUID(agentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	writeJSON(w, http.StatusOK, managedAgentToResponse(agent))
}

type UpdateManagedAgentRequest struct {
	Name           *string         `json:"name"`
	Description    *string         `json:"description"`
	Model          json.RawMessage `json:"model"`
	SystemPrompt   *string         `json:"system_prompt"`
	Tools          json.RawMessage `json:"tools"`
	McpServers     json.RawMessage `json:"mcp_servers"`
	Skills         json.RawMessage `json:"skills"`
	CallableAgents json.RawMessage `json:"callable_agents"`
	Metadata       json.RawMessage `json:"metadata"`
}

func (h *Handler) UpdateManagedAgent(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentID := chi.URLParam(r, "agentId")

	// Verify ownership
	existing, err := h.Queries.GetManagedAgentInWorkspace(r.Context(), db.GetManagedAgentInWorkspaceParams{
		ID:          parseUUID(agentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if existing.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}

	var req UpdateManagedAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateManagedAgentParams{ID: parseUUID(agentID)}
	if req.Name != nil {
		params.Name = *req.Name
	}
	if req.Description != nil {
		params.Description.String = *req.Description
		params.Description.Valid = true
	}
	if req.Model != nil {
		params.Model = req.Model
	}
	if req.SystemPrompt != nil {
		params.SystemPrompt.String = *req.SystemPrompt
		params.SystemPrompt.Valid = true
	}
	if req.Tools != nil {
		params.Tools = req.Tools
	}
	if req.McpServers != nil {
		params.McpServers = req.McpServers
	}
	if req.Skills != nil {
		params.Skills = req.Skills
	}
	if req.CallableAgents != nil {
		params.CallableAgents = req.CallableAgents
	}
	if req.Metadata != nil {
		params.Metadata = req.Metadata
	}

	agent, err := h.Queries.UpdateManagedAgent(r.Context(), params)
	if err != nil {
		slog.Error("failed to update managed agent", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update agent")
		return
	}

	// Save version snapshot
	snapshot, _ := json.Marshal(managedAgentToResponse(agent))
	h.Queries.CreateManagedAgentVersion(r.Context(), db.CreateManagedAgentVersionParams{
		AgentID:  agent.ID,
		Version:  agent.Version,
		Snapshot: snapshot,
	})

	writeJSON(w, http.StatusOK, managedAgentToResponse(agent))
}

func (h *Handler) ArchiveManagedAgent(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Queries.ArchiveManagedAgent(r.Context(), parseUUID(agentID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive agent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListManagedAgentVersions(w http.ResponseWriter, r *http.Request) {
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

	versions, err := h.Queries.ListManagedAgentVersions(r.Context(), parseUUID(agentID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}

	type VersionResponse struct {
		ID        string          `json:"id"`
		Version   int32           `json:"version"`
		Snapshot  json.RawMessage `json:"snapshot"`
		CreatedAt string          `json:"created_at"`
	}

	resp := make([]VersionResponse, len(versions))
	for i, v := range versions {
		resp[i] = VersionResponse{
			ID:        uuidToString(v.ID),
			Version:   v.Version,
			Snapshot:  v.Snapshot,
			CreatedAt: timestampToString(v.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// ---------------------------------------------------------------------------
// v1 Environments
// ---------------------------------------------------------------------------

type EnvironmentResponse struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Config     json.RawMessage `json:"config"`
	CreatedAt  string          `json:"created_at"`
	ArchivedAt *string         `json:"archived_at,omitempty"`
}

func environmentToResponse(e db.Environment) EnvironmentResponse {
	resp := EnvironmentResponse{
		ID:        uuidToString(e.ID),
		Name:      e.Name,
		Config:    e.Config,
		CreatedAt: timestampToString(e.CreatedAt),
	}
	if e.ArchivedAt.Valid {
		s := timestampToString(e.ArchivedAt)
		resp.ArchivedAt = &s
	}
	return resp
}

type CreateEnvironmentRequest struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

func (h *Handler) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	config := req.Config
	if config == nil {
		config = []byte(`{"type":"cloud","packages":{},"networking":{"type":"unrestricted"}}`)
	}

	env, err := h.Queries.CreateEnvironment(r.Context(), db.CreateEnvironmentParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Config:      config,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create environment")
		return
	}
	writeJSON(w, http.StatusCreated, environmentToResponse(env))
}

func (h *Handler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	envs, err := h.Queries.ListEnvironments(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list environments")
		return
	}

	resp := make([]EnvironmentResponse, len(envs))
	for i, e := range envs {
		resp[i] = environmentToResponse(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetEnvironment(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	envID := chi.URLParam(r, "envId")

	env, err := h.Queries.GetEnvironmentInWorkspace(r.Context(), db.GetEnvironmentInWorkspaceParams{
		ID:          parseUUID(envID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "environment not found")
		return
	}
	writeJSON(w, http.StatusOK, environmentToResponse(env))
}

func (h *Handler) ArchiveEnvironment(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Queries.ArchiveEnvironment(r.Context(), parseUUID(envID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive environment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// v1 Managed Sessions
// ---------------------------------------------------------------------------

type ManagedSessionResponse struct {
	ID           string          `json:"id"`
	AgentID      string          `json:"agent_id"`
	AgentVersion int32           `json:"agent_version"`
	Status       string          `json:"status"`
	VaultIds     []string        `json:"vault_ids"`
	Resources    json.RawMessage `json:"resources"`
	Usage        SessionUsage    `json:"usage"`
	Title        *string         `json:"title,omitempty"`
	StopReason   json.RawMessage `json:"stop_reason,omitempty"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

type SessionUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
}

func managedSessionToResponse(s db.ManagedSession) ManagedSessionResponse {
	vaultIDs := make([]string, 0, len(s.VaultIds))
	for _, v := range s.VaultIds {
		if v.Valid {
			vaultIDs = append(vaultIDs, uuidToString(v))
		}
	}

	resp := ManagedSessionResponse{
		ID:           uuidToString(s.ID),
		AgentID:      uuidToString(s.AgentID),
		AgentVersion: s.AgentVersion,
		Status:       s.Status,
		VaultIds:     vaultIDs,
		Resources:    s.Resources,
		Usage: SessionUsage{
			InputTokens:         s.UsageInputTokens,
			OutputTokens:        s.UsageOutputTokens,
			CacheCreationTokens: s.UsageCacheCreationTokens,
			CacheReadTokens:     s.UsageCacheReadTokens,
		},
		CreatedAt: timestampToString(s.CreatedAt),
		UpdatedAt: timestampToString(s.UpdatedAt),
	}
	if s.Title.Valid {
		resp.Title = &s.Title.String
	}
	if len(s.StopReason) > 0 {
		resp.StopReason = s.StopReason
	}
	return resp
}

type CreateManagedSessionRequest struct {
	AgentID       string   `json:"agent_id"`
	EnvironmentID *string  `json:"environment_id,omitempty"`
	VaultIds      []string `json:"vault_ids,omitempty"`
	Title         *string  `json:"title,omitempty"`
	Prompt        string   `json:"prompt"`
}

func (h *Handler) CreateManagedSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateManagedSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	agent, err := h.Queries.GetManagedAgentInWorkspace(r.Context(), db.GetManagedAgentInWorkspaceParams{
		ID:          parseUUID(req.AgentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}

	params := db.CreateManagedSessionParams{
		WorkspaceID:  parseUUID(workspaceID),
		AgentID:      agent.ID,
		AgentVersion: agent.Version,
	}

	if req.EnvironmentID != nil {
		params.EnvironmentID = parseUUID(*req.EnvironmentID)
	}
	if req.Title != nil {
		params.Title.String = *req.Title
		params.Title.Valid = true
	}

	vaultUUIDs := make([]pgtype.UUID, 0, len(req.VaultIds))
	for _, vid := range req.VaultIds {
		vaultUUIDs = append(vaultUUIDs, parseUUID(vid))
	}
	params.VaultIds = vaultUUIDs

	session, err := h.Queries.CreateManagedSession(r.Context(), params)
	if err != nil {
		slog.Error("failed to create managed session", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Launch agentic execution asynchronously
	if err := h.ManagedSessionService.ExecuteSession(r.Context(), session, agent, req.Prompt); err != nil {
		slog.Error("failed to start session execution", "error", err, "session_id", uuidToString(session.ID))
		// Session is created but execution failed to start — update status
		h.Queries.UpdateManagedSessionStatus(r.Context(), db.UpdateManagedSessionStatusParams{
			ID:     session.ID,
			Status: "failed",
		})
		session.Status = "failed"
	}

	writeJSON(w, http.StatusCreated, managedSessionToResponse(session))
}

func (h *Handler) ListManagedSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	limit := int32(50)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := parseInt32(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := parseInt32(o); err == nil && v >= 0 {
			offset = v
		}
	}

	sessions, err := h.Queries.ListManagedSessions(r.Context(), db.ListManagedSessionsParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	resp := make([]ManagedSessionResponse, len(sessions))
	for i, s := range sessions {
		resp[i] = managedSessionToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetManagedSession(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusOK, managedSessionToResponse(session))
}

func (h *Handler) ArchiveManagedSession(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Queries.ArchiveManagedSession(r.Context(), parseUUID(sessionID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// v1 Session Events (SSE stream)
// ---------------------------------------------------------------------------

func (h *Handler) ListSessionEvents(w http.ResponseWriter, r *http.Request) {
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

	events, err := h.Queries.ListSessionEvents(r.Context(), db.ListSessionEventsParams{
		SessionID: parseUUID(sessionID),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list events")
		return
	}

	type EventResponse struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
		CreatedAt string          `json:"created_at"`
	}

	resp := make([]EventResponse, len(events))
	for i, e := range events {
		resp[i] = EventResponse{
			ID:        uuidToString(e.ID),
			Type:      e.Type,
			Payload:   e.Payload,
			CreatedAt: timestampToString(e.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// StreamSessionEvents opens an SSE connection for real-time session events.
func (h *Handler) StreamSessionEvents(w http.ResponseWriter, r *http.Request) {
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

	ch := stream.Global.Subscribe(sessionID)
	defer stream.Global.Unsubscribe(sessionID, ch)

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
			if evt.Type == "done" || evt.Type == "error" {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ---------------------------------------------------------------------------
// v1 Memory Stores
// ---------------------------------------------------------------------------

type MemoryStoreResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func memoryStoreToResponse(s db.MemoryStore) MemoryStoreResponse {
	resp := MemoryStoreResponse{
		ID:        uuidToString(s.ID),
		Name:      s.Name,
		CreatedAt: timestampToString(s.CreatedAt),
	}
	if s.Description.Valid {
		resp.Description = &s.Description.String
	}
	return resp
}

func (h *Handler) CreateMemoryStore(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	params := db.CreateMemoryStoreParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
	}
	if req.Description != nil {
		params.Description.String = *req.Description
		params.Description.Valid = true
	}

	store, err := h.Queries.CreateMemoryStore(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create memory store")
		return
	}
	writeJSON(w, http.StatusCreated, memoryStoreToResponse(store))
}

func (h *Handler) ListMemoryStores(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	stores, err := h.Queries.ListMemoryStores(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list memory stores")
		return
	}

	resp := make([]MemoryStoreResponse, len(stores))
	for i, s := range stores {
		resp[i] = memoryStoreToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetMemoryStore(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, memoryStoreToResponse(store))
}

// ---------------------------------------------------------------------------
// v1 Vaults
// ---------------------------------------------------------------------------

type VaultResponse struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"display_name"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func vaultToResponse(v db.Vault) VaultResponse {
	return VaultResponse{
		ID:          uuidToString(v.ID),
		DisplayName: v.DisplayName,
		Metadata:    v.Metadata,
		CreatedAt:   timestampToString(v.CreatedAt),
		UpdatedAt:   timestampToString(v.UpdatedAt),
	}
}

func (h *Handler) CreateVault(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req struct {
		DisplayName string          `json:"display_name"`
		Metadata    json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}

	metadata := req.Metadata
	if metadata == nil {
		metadata = []byte(`{}`)
	}

	vault, err := h.Queries.CreateVault(r.Context(), db.CreateVaultParams{
		WorkspaceID: parseUUID(workspaceID),
		DisplayName: req.DisplayName,
		Metadata:    metadata,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create vault")
		return
	}
	writeJSON(w, http.StatusCreated, vaultToResponse(vault))
}

func (h *Handler) ListVaults(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	vaults, err := h.Queries.ListVaults(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list vaults")
		return
	}

	resp := make([]VaultResponse, len(vaults))
	for i, v := range vaults {
		resp[i] = vaultToResponse(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetVault(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, vaultToResponse(vault))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseInt32(s string) (int32, error) {
	var v int32
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
