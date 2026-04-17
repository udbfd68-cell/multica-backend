package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// ForceTaskRunning transitions a task directly from any non-terminal state to running.
// Used by cloud/demo execution which bypasses the normal dispatch flow.
func (q *Queries) ForceTaskRunning(ctx context.Context, id pgtype.UUID) error {
	_, err := q.db.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'running', dispatched_at = COALESCE(dispatched_at, now()), started_at = now() WHERE id = $1 AND status IN ('queued', 'dispatched')`,
		id,
	)
	return err
}
