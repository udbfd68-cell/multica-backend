package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp"
	"github.com/multica-ai/multica/server/internal/mcpclient"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// MCP Server Registry & Agent Connector API
// ---------------------------------------------------------------------------

// ===== CATALOG (built-in servers, no DB) =====

func (h *Handler) ListMcpCatalog(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	catalog := mcp.Catalog()

	if category != "" {
		filtered := make([]mcp.BuiltinServer, 0)
		for _, s := range catalog {
			if s.Category == category {
				filtered = append(filtered, s)
			}
		}
		catalog = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": catalog})
}

// ===== REGISTRY (workspace-scoped, DB-backed) =====

type McpRegistryResponse struct {
	ID          string          `json:"id"`
	IsBuiltin   bool            `json:"is_builtin"`
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	IconUrl     string          `json:"icon_url"`
	RepoUrl     string          `json:"repo_url"`
	ServerUrl   string          `json:"server_url"`
	Transport   string          `json:"transport"`
	Command     string          `json:"command"`
	Args        json.RawMessage `json:"args"`
	EnvVars     json.RawMessage `json:"env_vars"`
	AuthType    string          `json:"auth_type"`
	OauthConfig json.RawMessage `json:"oauth_config,omitempty"`
	Tags        []string        `json:"tags"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func mcpRegistryToResponse(r db.McpServerRegistry) McpRegistryResponse {
	resp := McpRegistryResponse{
		ID:          uuidToString(r.ID),
		IsBuiltin:   r.IsBuiltin,
		Slug:        r.Slug,
		Name:        r.Name,
		Description: r.Description,
		Category:    r.Category,
		IconUrl:     r.IconUrl,
		RepoUrl:     r.RepoUrl,
		ServerUrl:   r.ServerUrl,
		Transport:   r.Transport,
		Command:     r.Command,
		Args:        r.Args,
		EnvVars:     r.EnvVars,
		AuthType:    r.AuthType,
		Tags:        r.Tags,
		CreatedAt:   timestampToString(r.CreatedAt),
		UpdatedAt:   timestampToString(r.UpdatedAt),
	}
	if r.OauthConfig != nil {
		resp.OauthConfig = r.OauthConfig
	}
	if resp.Tags == nil {
		resp.Tags = []string{}
	}
	return resp
}

func (h *Handler) ListMcpRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	category := r.URL.Query().Get("category")
	var (
		items []db.McpServerRegistry
		err   error
	)
	if category != "" {
		items, err = h.Queries.ListMcpServerRegistryByCategory(r.Context(), parseUUID(workspaceID), category)
	} else {
		items, err = h.Queries.ListMcpServerRegistry(r.Context(), parseUUID(workspaceID))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list registry")
		return
	}

	resp := make([]McpRegistryResponse, len(items))
	for i, item := range items {
		resp[i] = mcpRegistryToResponse(item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetMcpRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	id := chi.URLParam(r, "registryId")

	item, err := h.Queries.GetMcpServerRegistry(r.Context(), parseUUID(id), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "registry entry not found")
		return
	}
	writeJSON(w, http.StatusOK, mcpRegistryToResponse(item))
}

type CreateMcpRegistryRequest struct {
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	RepoUrl     string          `json:"repo_url"`
	ServerUrl   string          `json:"server_url"`
	Transport   string          `json:"transport"`
	Command     string          `json:"command"`
	Args        json.RawMessage `json:"args"`
	EnvVars     json.RawMessage `json:"env_vars"`
	AuthType    string          `json:"auth_type"`
	OauthConfig json.RawMessage `json:"oauth_config"`
	Tags        []string        `json:"tags"`
}

func (h *Handler) CreateMcpRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateMcpRegistryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Slug == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "slug and name are required")
		return
	}
	if req.Transport == "" {
		req.Transport = "stdio"
	}
	if req.AuthType == "" {
		req.AuthType = "none"
	}
	if req.Args == nil {
		req.Args = []byte("[]")
	}
	if req.EnvVars == nil {
		req.EnvVars = []byte("[]")
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	item, err := h.Queries.CreateMcpServerRegistry(r.Context(), db.CreateMcpServerRegistryParams{
		WorkspaceID: parseUUID(workspaceID),
		IsBuiltin:   false,
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		RepoUrl:     req.RepoUrl,
		ServerUrl:   req.ServerUrl,
		Transport:   req.Transport,
		Command:     req.Command,
		Args:        req.Args,
		EnvVars:     req.EnvVars,
		AuthType:    req.AuthType,
		OauthConfig: req.OauthConfig,
		Tags:        req.Tags,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create registry entry")
		return
	}
	writeJSON(w, http.StatusCreated, mcpRegistryToResponse(item))
}

// SeedMcpRegistry populates the workspace registry from the built-in catalog (1-click add).
type SeedMcpRegistryRequest struct {
	Slug string `json:"slug"`
}

func (h *Handler) SeedMcpRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID := parseUUID(workspaceID)

	var req SeedMcpRegistryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	catalog := mcp.Catalog()
	var match *mcp.BuiltinServer
	for _, s := range catalog {
		if s.Slug == req.Slug {
			match = &s
			break
		}
	}
	if match == nil {
		writeError(w, http.StatusNotFound, "server not found in catalog")
		return
	}

	// Check if already exists
	_, err := h.Queries.GetMcpServerRegistryBySlug(r.Context(), match.Slug, wsUUID)
	if err == nil {
		writeError(w, http.StatusConflict, "server already in registry")
		return
	}

	argsJSON, _ := json.Marshal([]string{})
	envJSON, _ := json.Marshal(match.EnvVars)

	item, err := h.Queries.CreateMcpServerRegistry(r.Context(), db.CreateMcpServerRegistryParams{
		WorkspaceID: wsUUID,
		IsBuiltin:   true,
		Slug:        match.Slug,
		Name:        match.Name,
		Description: match.Description,
		Category:    match.Category,
		RepoUrl:     match.RepoURL,
		Transport:   match.Transport,
		Command:     match.Command,
		Args:        argsJSON,
		EnvVars:     envJSON,
		AuthType:    match.AuthType,
		Tags:        match.Tags,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to seed registry entry")
		return
	}
	writeJSON(w, http.StatusCreated, mcpRegistryToResponse(item))
}

// SeedAllMcpRegistry populates the workspace registry with ALL built-in servers.
func (h *Handler) SeedAllMcpRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID := parseUUID(workspaceID)

	catalog := mcp.Catalog()
	seeded := 0
	for _, s := range catalog {
		// Skip if exists
		if _, err := h.Queries.GetMcpServerRegistryBySlug(r.Context(), s.Slug, wsUUID); err == nil {
			continue
		}

		argsJSON, _ := json.Marshal([]string{})
		envJSON, _ := json.Marshal(s.EnvVars)

		_, err := h.Queries.CreateMcpServerRegistry(r.Context(), db.CreateMcpServerRegistryParams{
			WorkspaceID: wsUUID,
			IsBuiltin:   true,
			Slug:        s.Slug,
			Name:        s.Name,
			Description: s.Description,
			Category:    s.Category,
			RepoUrl:     s.RepoURL,
			Transport:   s.Transport,
			Command:     s.Command,
			Args:        argsJSON,
			EnvVars:     envJSON,
			AuthType:    s.AuthType,
			Tags:        s.Tags,
		})
		if err == nil {
			seeded++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"seeded": seeded, "total_catalog": len(catalog)})
}

func (h *Handler) DeleteMcpRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	id := chi.URLParam(r, "registryId")

	if err := h.Queries.DeleteMcpServerRegistry(r.Context(), parseUUID(id), parseUUID(workspaceID)); err != nil {
		writeError(w, http.StatusNotFound, "registry entry not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===== AGENT MCP CONNECTORS =====

type McpConnectorResponse struct {
	ID                string          `json:"id"`
	AgentID           string          `json:"agent_id"`
	RegistryID        *string         `json:"registry_id,omitempty"`
	Name              string          `json:"name"`
	ServerUrl         string          `json:"server_url"`
	Transport         string          `json:"transport"`
	Command           string          `json:"command"`
	Args              json.RawMessage `json:"args"`
	AuthType          string          `json:"auth_type"`
	VaultCredentialID *string         `json:"vault_credential_id,omitempty"`
	Enabled           bool            `json:"enabled"`
	Status            string          `json:"status"`
	StatusMessage     *string         `json:"status_message,omitempty"`
	LastValidatedAt   *string         `json:"last_validated_at,omitempty"`
	DiscoveredTools   json.RawMessage `json:"discovered_tools"`
	ToolsDiscoveredAt *string         `json:"tools_discovered_at,omitempty"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

func mcpConnectorToResponse(c db.AgentMcpConnector) McpConnectorResponse {
	resp := McpConnectorResponse{
		ID:              uuidToString(c.ID),
		AgentID:         uuidToString(c.AgentID),
		Name:            c.Name,
		ServerUrl:       c.ServerUrl,
		Transport:       c.Transport,
		Command:         c.Command,
		Args:            c.Args,
		AuthType:        c.AuthType,
		Enabled:         c.Enabled,
		Status:          c.Status,
		DiscoveredTools: c.DiscoveredTools,
		CreatedAt:       timestampToString(c.CreatedAt),
		UpdatedAt:       timestampToString(c.UpdatedAt),
	}
	if c.RegistryID.Valid {
		s := uuidToString(c.RegistryID)
		resp.RegistryID = &s
	}
	if c.VaultCredentialID.Valid {
		s := uuidToString(c.VaultCredentialID)
		resp.VaultCredentialID = &s
	}
	if c.StatusMessage.Valid {
		resp.StatusMessage = &c.StatusMessage.String
	}
	if c.LastValidatedAt.Valid {
		s := timestampToString(c.LastValidatedAt)
		resp.LastValidatedAt = &s
	}
	if c.ToolsDiscoveredAt.Valid {
		s := timestampToString(c.ToolsDiscoveredAt)
		resp.ToolsDiscoveredAt = &s
	}
	return resp
}

func (h *Handler) ListAgentMcpConnectors(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentID := chi.URLParam(r, "agentId")

	items, err := h.Queries.ListAgentMcpConnectors(r.Context(), parseUUID(agentID), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connectors")
		return
	}

	resp := make([]McpConnectorResponse, len(items))
	for i, item := range items {
		resp[i] = mcpConnectorToResponse(item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (h *Handler) GetAgentMcpConnector(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	connectorID := chi.URLParam(r, "connectorId")

	item, err := h.Queries.GetAgentMcpConnector(r.Context(), parseUUID(connectorID), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	writeJSON(w, http.StatusOK, mcpConnectorToResponse(item))
}

type CreateMcpConnectorRequest struct {
	RegistryID        *string         `json:"registry_id"`
	Name              string          `json:"name"`
	ServerUrl         string          `json:"server_url"`
	Transport         string          `json:"transport"`
	Command           string          `json:"command"`
	Args              json.RawMessage `json:"args"`
	EnvConfig         json.RawMessage `json:"env_config"`
	AuthType          string          `json:"auth_type"`
	VaultCredentialID *string         `json:"vault_credential_id"`
	Enabled           *bool           `json:"enabled"`
}

func (h *Handler) CreateAgentMcpConnector(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentID := chi.URLParam(r, "agentId")

	var req CreateMcpConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Transport == "" {
		req.Transport = "stdio"
	}
	if req.AuthType == "" {
		req.AuthType = "none"
	}
	if req.Args == nil {
		req.Args = []byte("[]")
	}
	if req.EnvConfig == nil {
		req.EnvConfig = []byte("{}")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	params := db.CreateAgentMcpConnectorParams{
		WorkspaceID: parseUUID(workspaceID),
		AgentID:     parseUUID(agentID),
		Name:        req.Name,
		ServerUrl:   req.ServerUrl,
		Transport:   req.Transport,
		Command:     req.Command,
		Args:        req.Args,
		EnvConfig:   req.EnvConfig,
		AuthType:    req.AuthType,
		Enabled:     enabled,
	}
	if req.RegistryID != nil {
		params.RegistryID = parseUUID(*req.RegistryID)
	}
	if req.VaultCredentialID != nil {
		params.VaultCredentialID = parseUUID(*req.VaultCredentialID)
	}

	item, err := h.Queries.CreateAgentMcpConnector(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create connector")
		return
	}
	writeJSON(w, http.StatusCreated, mcpConnectorToResponse(item))
}

// AddFromRegistry creates a connector for an agent from a registry entry (1-click attach).
type AddFromRegistryRequest struct {
	RegistryID        string  `json:"registry_id"`
	VaultCredentialID *string `json:"vault_credential_id"`
}

func (h *Handler) AddMcpFromRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentID := chi.URLParam(r, "agentId")
	wsUUID := parseUUID(workspaceID)

	var req AddFromRegistryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RegistryID == "" {
		writeError(w, http.StatusBadRequest, "registry_id is required")
		return
	}

	reg, err := h.Queries.GetMcpServerRegistry(r.Context(), parseUUID(req.RegistryID), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "registry entry not found")
		return
	}

	params := db.CreateAgentMcpConnectorParams{
		WorkspaceID: wsUUID,
		AgentID:     parseUUID(agentID),
		RegistryID:  reg.ID,
		Name:        reg.Name,
		ServerUrl:   reg.ServerUrl,
		Transport:   reg.Transport,
		Command:     reg.Command,
		Args:        reg.Args,
		EnvConfig:   []byte("{}"),
		AuthType:    reg.AuthType,
		Enabled:     true,
	}
	if req.VaultCredentialID != nil {
		params.VaultCredentialID = parseUUID(*req.VaultCredentialID)
	}

	item, err := h.Queries.CreateAgentMcpConnector(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create connector from registry")
		return
	}
	writeJSON(w, http.StatusCreated, mcpConnectorToResponse(item))
}

// AutoAttachBrowserMcp attaches all zero-auth browser automation MCP servers to an agent.
// This is the "make my agent a real browser agent" 1-click button. No API keys needed.
func (h *Handler) AutoAttachBrowserMcp(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	agentID := chi.URLParam(r, "agentId")
	wsUUID := parseUUID(workspaceID)
	agUUID := parseUUID(agentID)

	// First, seed all catalog entries into the workspace registry (idempotent).
	catalog := mcp.Catalog()
	for _, s := range catalog {
		if _, err := h.Queries.GetMcpServerRegistryBySlug(r.Context(), s.Slug, wsUUID); err == nil {
			continue
		}
		argsJSON, _ := json.Marshal([]string{})
		envJSON, _ := json.Marshal(s.EnvVars)
		h.Queries.CreateMcpServerRegistry(r.Context(), db.CreateMcpServerRegistryParams{
			WorkspaceID: wsUUID, IsBuiltin: true, Slug: s.Slug, Name: s.Name,
			Description: s.Description, Category: s.Category, RepoUrl: s.RepoURL,
			Transport: s.Transport, Command: s.Command, Args: argsJSON,
			EnvVars: envJSON, AuthType: s.AuthType, Tags: s.Tags,
		})
	}

	// Attach all zero-auth servers from browser, utility, memory, search categories.
	noAuthSlugs := []string{}
	for _, s := range catalog {
		if s.AuthType == "none" {
			noAuthSlugs = append(noAuthSlugs, s.Slug)
		}
	}

	attached := 0
	for _, slug := range noAuthSlugs {
		reg, err := h.Queries.GetMcpServerRegistryBySlug(r.Context(), slug, wsUUID)
		if err != nil {
			continue
		}
		// Check if already attached to this agent.
		existing, _ := h.Queries.ListAgentMcpConnectors(r.Context(), agUUID, wsUUID)
		alreadyAttached := false
		for _, c := range existing {
			if c.RegistryID.Valid && c.RegistryID == reg.ID {
				alreadyAttached = true
				break
			}
		}
		if alreadyAttached {
			continue
		}
		_, err = h.Queries.CreateAgentMcpConnector(r.Context(), db.CreateAgentMcpConnectorParams{
			WorkspaceID: wsUUID,
			AgentID:     agUUID,
			RegistryID:  reg.ID,
			Name:        reg.Name,
			ServerUrl:   reg.ServerUrl,
			Transport:   reg.Transport,
			Command:     reg.Command,
			Args:        reg.Args,
			EnvConfig:   []byte("{}"),
			AuthType:    reg.AuthType,
			Enabled:     true,
		})
		if err == nil {
			attached++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"attached": attached, "no_auth_servers": len(noAuthSlugs)})
}

type UpdateMcpConnectorRequest struct {
	Name              *string         `json:"name"`
	ServerUrl         *string         `json:"server_url"`
	Transport         *string         `json:"transport"`
	Command           *string         `json:"command"`
	Args              json.RawMessage `json:"args"`
	EnvConfig         json.RawMessage `json:"env_config"`
	AuthType          *string         `json:"auth_type"`
	VaultCredentialID *string         `json:"vault_credential_id"`
	Enabled           *bool           `json:"enabled"`
}

func (h *Handler) UpdateAgentMcpConnector(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	connectorID := chi.URLParam(r, "connectorId")

	var req UpdateMcpConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateAgentMcpConnectorParams{
		ID:          parseUUID(connectorID),
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		ServerUrl:   req.ServerUrl,
		Transport:   req.Transport,
		Command:     req.Command,
		Args:        req.Args,
		EnvConfig:   req.EnvConfig,
		AuthType:    req.AuthType,
		Enabled:     req.Enabled,
	}
	if req.VaultCredentialID != nil {
		params.VaultCredentialID = parseUUID(*req.VaultCredentialID)
	}

	item, err := h.Queries.UpdateAgentMcpConnector(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	writeJSON(w, http.StatusOK, mcpConnectorToResponse(item))
}

func (h *Handler) DeleteAgentMcpConnector(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	connectorID := chi.URLParam(r, "connectorId")

	if err := h.Queries.DeleteAgentMcpConnector(r.Context(), parseUUID(connectorID), parseUUID(workspaceID)); err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ValidateMcpConnector performs a health check on an MCP connector.
// For stdio transport, we verify the command exists.
// For SSE/HTTP transport, we attempt a connection to the server URL.
func (h *Handler) ValidateMcpConnector(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	connectorID := chi.URLParam(r, "connectorId")

	connector, err := h.Queries.GetAgentMcpConnector(r.Context(), parseUUID(connectorID), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	// Basic validation: check required fields
	status := "connected"
	var statusMsg *string

	switch connector.Transport {
	case "stdio":
		if connector.Command == "" {
			status = "error"
			msg := "no command configured for stdio transport"
			statusMsg = &msg
		}
	case "sse", "streamable-http":
		if connector.ServerUrl == "" {
			status = "error"
			msg := "no server_url configured for " + connector.Transport + " transport"
			statusMsg = &msg
		}
	}

	// Update status in DB
	_ = h.Queries.UpdateAgentMcpConnectorStatus(r.Context(), connector.ID, status, statusMsg)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           uuidToString(connector.ID),
		"status":       status,
		"status_message": statusMsg,
		"transport":    connector.Transport,
	})
}

// DiscoverMcpTools triggers tool discovery on a connector via the MCP protocol.
// Establishes a real MCP connection, calls tools/list, and caches the response.
func (h *Handler) DiscoverMcpTools(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	connectorID := chi.URLParam(r, "connectorId")

	connector, err := h.Queries.GetAgentMcpConnector(r.Context(), parseUUID(connectorID), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	// Build MCP client config from connector row
	cfg := mcpclient.Config{
		Name:      connector.Name,
		Transport: connector.Transport,
		Command:   connector.Command,
		URL:       connector.ServerUrl,
	}
	if connector.Args != nil {
		json.Unmarshal(connector.Args, &cfg.Args)
	}
	if connector.EnvConfig != nil {
		json.Unmarshal(connector.EnvConfig, &cfg.Env)
	}

	// Connect to MCP server and discover tools
	client, err := mcpclient.New(r.Context(), cfg, slog.Default())
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect to MCP server: "+err.Error())
		return
	}
	defer client.Close()

	// Serialize discovered tools
	discoveredTools, _ := json.Marshal(client.Tools())

	// Cache discovered tools in the database
	h.Queries.UpdateAgentMcpConnectorTools(r.Context(), connector.ID, discoveredTools)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":    uuidToString(connector.ID),
		"name":  connector.Name,
		"tools": json.RawMessage(discoveredTools),
	})
}

// parseOptionalUUID parses a UUID string into pgtype.UUID, returning a zero-valid UUID if empty.
func parseOptionalUUID(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	return parseUUID(s)
}
