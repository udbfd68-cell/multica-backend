// Package session — cost.go implements granular cost tracking.
//
// This is a Multica differentiator vs Anthropic Managed Agents:
// - Per-session, per-tool, per-provider cost breakdown
// - Budget alerts at workspace level
// - Pricing tables for all major providers
package session

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Pricing — Cost per million tokens for each provider/model
// ---------------------------------------------------------------------------

// ModelPricing holds input/output cost per million tokens.
type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
	CachedPerMillion float64 // price for cached input tokens (usually cheaper)
}

// pricingTable maps "provider/model" → pricing.
// Updated April 2026 pricing.
var pricingTable = map[string]ModelPricing{
	// Anthropic
	"anthropic/claude-opus-4":               {InputPerMillion: 15.0, OutputPerMillion: 75.0, CachedPerMillion: 1.875},
	"anthropic/claude-opus-4.6":             {InputPerMillion: 15.0, OutputPerMillion: 75.0, CachedPerMillion: 1.875},
	"anthropic/claude-sonnet-4":             {InputPerMillion: 3.0, OutputPerMillion: 15.0, CachedPerMillion: 0.375},
	"anthropic/claude-sonnet-4-20250514":    {InputPerMillion: 3.0, OutputPerMillion: 15.0, CachedPerMillion: 0.375},
	"anthropic/claude-haiku-4":              {InputPerMillion: 0.25, OutputPerMillion: 1.25, CachedPerMillion: 0.03},
	"anthropic/claude-haiku-4-5-20251001":   {InputPerMillion: 0.80, OutputPerMillion: 4.0, CachedPerMillion: 0.08},

	// OpenAI
	"openai/gpt-4o":           {InputPerMillion: 2.50, OutputPerMillion: 10.0, CachedPerMillion: 1.25},
	"openai/gpt-4o-mini":      {InputPerMillion: 0.15, OutputPerMillion: 0.60, CachedPerMillion: 0.075},
	"openai/o1":               {InputPerMillion: 15.0, OutputPerMillion: 60.0, CachedPerMillion: 7.50},
	"openai/o3":               {InputPerMillion: 10.0, OutputPerMillion: 40.0, CachedPerMillion: 2.50},
	"openai/o3-mini":          {InputPerMillion: 1.10, OutputPerMillion: 4.40, CachedPerMillion: 0.55},
	"openai/o4-mini":          {InputPerMillion: 1.10, OutputPerMillion: 4.40, CachedPerMillion: 0.55},

	// Google
	"google/gemini-2.5-pro":   {InputPerMillion: 1.25, OutputPerMillion: 10.0, CachedPerMillion: 0.3125},
	"google/gemini-2.0-flash": {InputPerMillion: 0.10, OutputPerMillion: 0.40, CachedPerMillion: 0.025},
	"google/gemini-2.5-flash": {InputPerMillion: 0.15, OutputPerMillion: 0.60, CachedPerMillion: 0.0375},

	// Local (free)
	"ollama/*": {InputPerMillion: 0, OutputPerMillion: 0, CachedPerMillion: 0},
}

// LookupPricing finds pricing for a provider/model combination.
func LookupPricing(model string) ModelPricing {
	// Exact match
	if p, ok := pricingTable[model]; ok {
		return p
	}

	// Try with provider prefix normalization
	lower := strings.ToLower(model)
	if p, ok := pricingTable[lower]; ok {
		return p
	}

	// Check for wildcard matches (e.g. "ollama/*")
	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		wildcard := parts[0] + "/*"
		if p, ok := pricingTable[wildcard]; ok {
			return p
		}
	}

	// Default: mid-range pricing (safe fallback)
	return ModelPricing{InputPerMillion: 3.0, OutputPerMillion: 15.0, CachedPerMillion: 0.375}
}

// ---------------------------------------------------------------------------
// CostTracker — Records and queries cost events
// ---------------------------------------------------------------------------

// CostTracker provides granular cost tracking per session/tool/provider.
type CostTracker struct {
	queries *db.Queries
}

// NewCostTracker creates a cost tracker.
func NewCostTracker(q *db.Queries) *CostTracker {
	return &CostTracker{queries: q}
}

// CostRecord is the data for a single cost event.
type CostRecord struct {
	SessionID   string
	WorkspaceID string
	Provider    string
	Model       string
	Operation   string // inference, tool_call, mcp_call, etc.
	TokensInput int64
	TokensOutput int64
	TokensCached int64
	DurationMs  int
	ToolName    string
	EventIndex  int
}

// SessionCostReport aggregates costs for a single session.
type SessionCostReport struct {
	TotalCost    float64              `json:"total_cost_usd"`
	TotalInput   int64                `json:"total_input_tokens"`
	TotalOutput  int64                `json:"total_output_tokens"`
	TotalCached  int64                `json:"total_cached_tokens"`
	EventCount   int                  `json:"event_count"`
	ByOperation  []OperationCost      `json:"by_operation"`
	ByTool       []ToolCost           `json:"by_tool"`
}

// OperationCost breaks down cost by operation type.
type OperationCost struct {
	Operation   string  `json:"operation"`
	TotalCost   float64 `json:"total_cost_usd"`
	CallCount   int     `json:"call_count"`
	TotalInput  int64   `json:"total_input_tokens"`
	TotalOutput int64   `json:"total_output_tokens"`
}

// ToolCost breaks down cost by tool name.
type ToolCost struct {
	ToolName   string  `json:"tool_name"`
	TotalCost  float64 `json:"total_cost_usd"`
	CallCount  int     `json:"call_count"`
	DurationMs int     `json:"total_duration_ms"`
}

// ---------------------------------------------------------------------------
// Record — Write a cost event
// ---------------------------------------------------------------------------

// Record calculates the cost and persists a cost event.
func (ct *CostTracker) Record(ctx context.Context, rec CostRecord) (float64, error) {
	pricing := LookupPricing(rec.Model)

	// Calculate cost
	costUSD := float64(rec.TokensInput) * pricing.InputPerMillion / 1_000_000
	costUSD += float64(rec.TokensOutput) * pricing.OutputPerMillion / 1_000_000
	if rec.TokensCached > 0 {
		costUSD += float64(rec.TokensCached) * pricing.CachedPerMillion / 1_000_000
	}

	// Round to 8 decimal places
	costUSD = math.Round(costUSD*1e8) / 1e8

	// Persist
	_, err := ct.queries.CreateCostEvent(ctx, db.CreateCostEventParams{
		SessionID:    util.ParseUUID(rec.SessionID),
		WorkspaceID:  util.ParseUUID(rec.WorkspaceID),
		Provider:     rec.Provider,
		Model:        rec.Model,
		Operation:    rec.Operation,
		TokensInput:  &rec.TokensInput,
		TokensOutput: &rec.TokensOutput,
		TokensCached: &rec.TokensCached,
		CostUsd:      fmt.Sprintf("%.8f", costUSD),
		DurationMs:   &rec.DurationMs,
		ToolName:     &rec.ToolName,
		EventIndex:   &rec.EventIndex,
	})
	if err != nil {
		return 0, fmt.Errorf("record cost event: %w", err)
	}

	// Update session total
	_ = ct.queries.UpdateManagedSessionCost(ctx, util.ParseUUID(rec.SessionID), fmt.Sprintf("%.8f", costUSD))

	return costUSD, nil
}

// ---------------------------------------------------------------------------
// GetSessionCost — Aggregate cost for a session
// ---------------------------------------------------------------------------

// GetSessionCost returns a full cost breakdown for a session.
func (ct *CostTracker) GetSessionCost(ctx context.Context, sessionID string) (*SessionCostReport, error) {
	sid := util.ParseUUID(sessionID)

	// Totals
	totals, err := ct.queries.GetSessionCost(ctx, sid)
	if err != nil {
		return nil, fmt.Errorf("get session cost: %w", err)
	}

	report := &SessionCostReport{
		TotalInput:  totals.TotalInput,
		TotalOutput: totals.TotalOutput,
		TotalCached: totals.TotalCached,
		EventCount:  int(totals.EventCount),
	}

	// Parse total_cost from the numeric field
	if tc, ok := totals.TotalCost.(string); ok {
		fmt.Sscanf(tc, "%f", &report.TotalCost)
	}

	// By operation
	ops, err := ct.queries.GetSessionCostByOperation(ctx, sid)
	if err == nil {
		for _, op := range ops {
			oc := OperationCost{
				Operation:   op.Operation,
				CallCount:   int(op.CallCount),
				TotalInput:  op.TotalInput,
				TotalOutput: op.TotalOutput,
			}
			if tc, ok := op.TotalCost.(string); ok {
				fmt.Sscanf(tc, "%f", &oc.TotalCost)
			}
			report.ByOperation = append(report.ByOperation, oc)
		}
	}

	// By tool
	tools, err := ct.queries.GetSessionCostByTool(ctx, sid)
	if err == nil {
		for _, t := range tools {
			tc := ToolCost{
				CallCount:  int(t.CallCount),
				DurationMs: int(t.TotalDurationMs),
			}
			if t.ToolName != nil {
				tc.ToolName = *t.ToolName
			}
			if cost, ok := t.TotalCost.(string); ok {
				fmt.Sscanf(cost, "%f", &tc.TotalCost)
			}
			report.ByTool = append(report.ByTool, tc)
		}
	}

	return report, nil
}

// ---------------------------------------------------------------------------
// Budget enforcement
// ---------------------------------------------------------------------------

// BudgetStatus indicates whether a workspace is within its spending limits.
type BudgetStatus struct {
	Allowed      bool    `json:"allowed"`
	DailySpent   float64 `json:"daily_spent_usd"`
	MonthlySpent float64 `json:"monthly_spent_usd"`
	DailyLimit   float64 `json:"daily_limit_usd,omitempty"`
	MonthlyLimit float64 `json:"monthly_limit_usd,omitempty"`
	Reason       string  `json:"reason,omitempty"`
}

// CheckBudget verifies the workspace hasn't exceeded daily or monthly budget.
// Returns allowed=true if no budget is set or spending is within limits.
func (ct *CostTracker) CheckBudget(ctx context.Context, workspaceID string) (*BudgetStatus, error) {
	wid := util.ParseUUID(workspaceID)

	// Read workspace budget columns
	budget, err := ct.queries.GetWorkspaceBudget(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("get workspace budget: %w", err)
	}

	// Extract budget limits
	var dailyLimit, monthlyLimit float64
	var hasDailyLimit, hasMonthlyLimit bool

	if dl, ok := budget.DailyBudgetUsd.(string); ok && dl != "" {
		fmt.Sscanf(dl, "%f", &dailyLimit)
		hasDailyLimit = dailyLimit > 0
	}
	if ml, ok := budget.MonthlyBudgetUsd.(string); ok && ml != "" {
		fmt.Sscanf(ml, "%f", &monthlyLimit)
		hasMonthlyLimit = monthlyLimit > 0
	}

	// No budget set — always allowed
	if !hasDailyLimit && !hasMonthlyLimit {
		return &BudgetStatus{Allowed: true}, nil
	}

	now := time.Now().UTC()
	status := &BudgetStatus{Allowed: true}

	// Check daily budget
	if hasDailyLimit {
		status.DailyLimit = dailyLimit
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		dayEnd := dayStart.Add(24 * time.Hour)
		daily, err := ct.queries.GetWorkspaceCostPeriod(ctx, db.GetWorkspaceCostPeriodParams{
			WorkspaceID: wid,
			CreatedAt:   pgtype.Timestamptz{Time: dayStart, Valid: true},
			CreatedAt_2: pgtype.Timestamptz{Time: dayEnd, Valid: true},
		})
		if err == nil {
			if tc, ok := daily.TotalCost.(string); ok {
				fmt.Sscanf(tc, "%f", &status.DailySpent)
			}
		}
		if status.DailySpent >= dailyLimit {
			status.Allowed = false
			status.Reason = fmt.Sprintf("daily budget exceeded: $%.2f / $%.2f", status.DailySpent, dailyLimit)
		}
	}

	// Check monthly budget
	if hasMonthlyLimit {
		status.MonthlyLimit = monthlyLimit
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, 0)
		monthly, err := ct.queries.GetWorkspaceCostPeriod(ctx, db.GetWorkspaceCostPeriodParams{
			WorkspaceID: wid,
			CreatedAt:   pgtype.Timestamptz{Time: monthStart, Valid: true},
			CreatedAt_2: pgtype.Timestamptz{Time: monthEnd, Valid: true},
		})
		if err == nil {
			if tc, ok := monthly.TotalCost.(string); ok {
				fmt.Sscanf(tc, "%f", &status.MonthlySpent)
			}
		}
		if status.MonthlySpent >= monthlyLimit {
			status.Allowed = false
			status.Reason = fmt.Sprintf("monthly budget exceeded: $%.2f / $%.2f", status.MonthlySpent, monthlyLimit)
		}
	}

	return status, nil
}
