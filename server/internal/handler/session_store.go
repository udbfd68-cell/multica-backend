package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/session"
)

// ---------------------------------------------------------------------------
// Session Store API — Managed Agents Architecture
// ---------------------------------------------------------------------------

// GetSessionStoreEvents returns events from the session store with positional
// slicing support. Query params:
//   - from: start index (inclusive, default 0)
//   - to: end index (exclusive, default all)
//   - types: comma-separated event types filter
func (h *Handler) GetSessionStoreEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id required")
		return
	}

	opts := &session.GetEventsOptions{}

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if from, err := strconv.Atoi(fromStr); err == nil {
			opts.From = from
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if to, err := strconv.Atoi(toStr); err == nil {
			opts.To = to
		}
	}
	if typesStr := r.URL.Query().Get("types"); typesStr != "" {
		for _, t := range splitComma(typesStr) {
			opts.Types = append(opts.Types, session.EventType(t))
		}
	}

	store := h.ManagedSessionService.Store
	events, err := store.GetEvents(r.Context(), sessionID, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

// GetSessionCost returns the cost breakdown for a session.
func (h *Handler) GetSessionCost(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id required")
		return
	}

	tracker := h.ManagedSessionService.CostTracker
	report, err := tracker.GetSessionCost(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// WakeSession recovers a session after a crash. The harness calls this to
// resume where the previous execution left off.
func (h *Handler) WakeSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id required")
		return
	}

	store := h.ManagedSessionService.Store
	info, err := store.Wake(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, info)
}

// GetWorkspaceBudget returns the current budget status for the workspace.
func (h *Handler) GetWorkspaceBudget(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	tracker := h.ManagedSessionService.CostTracker
	status, err := tracker.CheckBudget(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// UpdateWorkspaceBudget sets the daily and monthly budget limits for the workspace.
func (h *Handler) UpdateWorkspaceBudget(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req struct {
		DailyBudgetUSD  float64 `json:"daily_budget_usd"`
		MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.Queries.UpdateWorkspaceBudget(r.Context(), db.UpdateWorkspaceBudgetParams{
		ID:              parseUUID(workspaceID),
		DailyBudgetUsd:  req.DailyBudgetUSD,
		MonthlyBudgetUsd: req.MonthlyBudgetUSD,
	})
	if err != nil {
		slog.Error("failed to update workspace budget", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update budget")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func splitComma(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ',' {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
