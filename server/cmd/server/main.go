package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func main() {
	logger.Init()

	// Warn about missing configuration
	if os.Getenv("JWT_SECRET") == "" {
		slog.Warn("JWT_SECRET is not set — using insecure default. Set JWT_SECRET for production use.")
	}
	if os.Getenv("RESEND_API_KEY") == "" {
		slog.Warn("RESEND_API_KEY is not set — email verification codes will be printed to the log instead of emailed.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://aurion:aurion@localhost:5432/aurion?sslmode=disable"
	}

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	registerListeners(bus, hub)

	queries := db.New(pool)
	// Order matters: subscriber listeners must register BEFORE notification listeners.
	// The notification listener queries the subscriber table to determine recipients,
	// so subscribers must be written first within the same synchronous event dispatch.
	registerSubscriberListeners(bus, queries)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)

	// Create default user/workspace when auth is disabled
	EnsureDefaultUser(ctx, pool)

	r, h := NewRouter(pool, hub, bus)

	// Recover sessions that were running when the server last stopped.
	recoverRunningSessions(ctx, h)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	// Start background workers.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	autopilotCtx, autopilotCancel := context.WithCancel(context.Background())
	taskSvc := service.NewTaskService(queries, hub, bus)
	autopilotSvc := service.NewAutopilotService(queries, pool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	// Start background sweeper to mark stale runtimes as offline.
	go runRuntimeSweeper(sweepCtx, queries, bus)
	go runAutopilotScheduler(autopilotCtx, queries, autopilotSvc)

	// Graceful shutdown
	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	sweepCancel()
	autopilotCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

// recoverRunningSessions marks any sessions that were "running" at shutdown
// as terminated and reinitialises their Session Store counters so they can
// be woken later.  This prevents stale "running" sessions from persisting
// across restarts.
func recoverRunningSessions(ctx context.Context, h *handler.Handler) {
	store := h.ManagedSessionService.Store
	sessions, err := store.Queries().GetRunningSessions(ctx)
	if err != nil {
		slog.Warn("failed to query running sessions for recovery", "error", err)
		return
	}
	if len(sessions) == 0 {
		return
	}

	slog.Info("recovering stale sessions", "count", len(sessions))
	for _, s := range sessions {
		sessionID := util.UUIDToString(s.ID)

		// Mark interrupted — the session wasn't properly closed.
		// Users can manually wake them via POST /store/wake.
		_, err := store.Wake(ctx, sessionID)
		if err != nil {
			slog.Warn("failed to recover session", "session_id", sessionID, "error", err)
			continue
		}

		// Immediately terminate: the agent loop is gone, don't leave them "running".
		_ = store.Close(ctx, sessionID, "interrupted", map[string]any{
			"reason": "server_restart",
		})
		slog.Info("recovered session", "session_id", sessionID)
	}
}
