# PROGRESS.md — Managed Agents Architecture Implementation

## Overview

Implementation of the 3-component Managed Agents architecture (Session Store + Context Engineering + Cost Tracker) inspired by Anthropic's engineering blog.

## Status: Phase 1 — Core Infrastructure ✅

### Module 1: Session Store ✅
- **Migration**: `052_session_store_harness` — event_index, cost_event table, session metadata
- **Store**: `server/internal/session/store.go` — Create, Get, AppendEvent, GetEvents (positional slicing), Wake, Close
- **Generated queries**: `server/pkg/db/generated/session_store.sql.go` — 15 new query functions
- **Model updates**: ManagedSession + SessionEvent structs extended with new fields

### Module 2: Context Engineering ✅
- **Context builder**: `server/internal/session/context.go` — BuildContextWindow with 3 strategies
- **Strategies**: sliding_window (default), smart_summary, full_replay
- **Compaction**: ShouldCompact detects 80% threshold, BuildCompactionSummary creates summaries
- **Context resets**: context_reset events store compacted summaries, harness resumes from there

### Module 3: Cost Tracker ✅
- **Tracker**: `server/internal/session/cost.go` — Record, GetSessionCost, LookupPricing
- **Pricing tables**: Anthropic (Claude 4/4.6/Sonnet/Haiku), OpenAI (GPT-4o/o1/o3/o4-mini), Google (Gemini 2.5), Ollama (free)
- **Granular breakdown**: per-session, per-operation, per-tool, per-workspace, daily charts

### Module 4: Harness Integration ✅
- **Service**: `server/internal/service/managed_session.go` — Session Store + CostTracker wired into ManagedSessionService
- **drainAndStream**: Events persisted through Session Store with event indices (backward-compatible with legacy SSE)
- **mapMessageType/mapMessageData**: agent.Message → session.Event conversion

### Module 5: API Routes ✅
- **GET** `/api/v1/sessions/{id}/store/events` — Positional event slicing with type filtering
- **GET** `/api/v1/sessions/{id}/store/cost` — Session cost breakdown
- **POST** `/api/v1/sessions/{id}/store/wake` — Crash recovery
- **Handler**: `server/internal/handler/session_store.go`

### Module 6: Frontend Session View ✅
- **Component**: `packages/views/agents/components/session-view.tsx` — Real-time event timeline + cost sidebar
- **Types**: `packages/core/types/managed-agents.ts` — StoreEvent, SessionCostReport, SessionInfo, ContextStrategy
- **API Client**: `packages/core/api/client.ts` — getSessionStoreEvents, getSessionCost, wakeSession

### Module 7: Documentation ✅
- **CLAUDE.md** updated with Managed Agents Architecture section
- **PROGRESS.md** created (this file)

## Files Created/Modified

### New Files
| File | Lines | Purpose |
|------|-------|---------|
| `server/migrations/052_session_store_harness.up.sql` | ~80 | Schema: event_index, cost_event, session metadata |
| `server/migrations/052_session_store_harness.down.sql` | ~30 | Rollback migration |
| `server/internal/session/store.go` | ~530 | Core Session Store (Create/Get/Append/Wake/Close) |
| `server/internal/session/context.go` | ~300 | Context engineering (sliding_window, smart_summary) |
| `server/internal/session/cost.go` | ~230 | Cost tracker with provider pricing tables |
| `server/pkg/db/generated/session_store.sql.go` | ~340 | Generated query functions for new SQL |
| `server/internal/handler/session_store.go` | ~120 | API handlers for Session Store endpoints |
| `packages/views/agents/components/session-view.tsx` | ~380 | Frontend session event timeline |

### Modified Files
| File | Change |
|------|--------|
| `server/pkg/db/generated/models.go` | Added 5 fields to ManagedSession, 2 to SessionEvent |
| `server/pkg/db/queries/session_event.sql` | Added 6 positional slicing queries |
| `server/pkg/db/queries/cost_event.sql` | New file with 8 cost queries |
| `server/pkg/db/queries/managed_session.sql` | Added 4 wake/recovery queries |
| `server/internal/service/managed_session.go` | Session Store + CostTracker integration |
| `server/cmd/server/router.go` | Added 3 Session Store API routes |
| `packages/core/types/managed-agents.ts` | Added Store event types + cost report types |
| `packages/core/api/client.ts` | Added 3 Session Store API methods |
| `packages/views/agents/index.ts` | Export SessionView |
| `CLAUDE.md` | Added Managed Agents Architecture section |

## Phase 2 — Crash Recovery + Sessions Tab + Budget ✅

### Module 8: Crash Recovery ✅
- **main.go**: `recoverRunningSessions()` marks stale running sessions as interrupted on startup
- **NewRouter** returns `(chi.Router, *handler.Handler)` for service access
- **Store.Queries()** exposed for direct DB access

### Module 9: Sessions Tab ✅
- **SessionsTab**: `packages/views/agents/components/tabs/sessions-tab.tsx` — lists agent sessions, drills into SessionView
- **agent-detail.tsx**: Sessions tab added between Tasks and MCP Servers
- **ListManagedSessions handler**: supports `?agent_id=` query param filter
- **Client**: `listManagedSessions({ agentId })` method updated

### Module 10: Budget Enforcement ✅
- **CostTracker.CheckBudget()**: reads daily/monthly budget, checks current spending vs limit
- **GetWorkspaceBudget query**: dedicated SQL for budget columns
- **CreateManagedSession**: rejects with HTTP 402 when budget exceeded
- **GET /api/v1/sessions/budget**: API endpoint for frontend budget display
- **BudgetStatus type**: `packages/core/types/managed-agents.ts`

## Phase 3 — Agent Triggers, Budget UI, Compaction ✅

### Agent Trigger API
- **POST /api/v1/agents/{agentId}/trigger**: Creates a managed session and immediately executes it
- Request body: `{ prompt, title?, environment_id?, vault_ids?, source? }`
- Budget check before session creation (402 on exceed)
- Source tracking: "manual", "webhook", "api", "schedule"
- Handler: `TriggerAgent` in `managed_agents.go`

### Workspace Budget Settings UI
- **Settings > Usage tab**: New workspace settings tab with budget management
- Real-time spending display: daily and monthly with progress bars
- Budget limit configuration: daily and monthly USD limits
- Visual warnings at 80% usage, "Over limit" badge when exceeded
- **PUT /api/v1/sessions/budget**: Backend endpoint to update budget limits
- `UpdateWorkspaceBudget` query in `session_store.sql.go`

### Frontend Trigger UI
- **Sessions tab**: "Quick Run" input bar at the top of sessions list
- Enter prompt + click "Run" to trigger agent and auto-navigate to session view
- Also shown in empty state with guidance text

### Context Compaction Enhancement
- `BuildCompactionSummary` enhanced: now includes user request summaries
- `BuildCompactionPrompt`: generates prompt for cheaper model (Haiku) compaction
- `CompactFunc` type: allows harness to inject model-based compaction

## Phase 4 — TODO
- [ ] Docker-based sandbox (container-per-session, cattle pattern)
- [ ] Credential isolation via MCP proxy (vault tokens never in sandbox)
- [ ] Public webhook endpoint (unauthenticated agent trigger via token)
- [ ] Model-based compaction integration (call Haiku in harness loop)
- [ ] E2E tests for Session Store API
