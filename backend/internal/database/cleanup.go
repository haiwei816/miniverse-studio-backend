package database

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

const (
	cleanupInterval = 5 * time.Minute
	cleanupGrace    = time.Hour
	cleanupBatch    = 1000
)

type cleanupExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// cleanupExpired bounds work per table and retains expired records for a grace
// period. In particular, a recently expired nonce must not be removed while a
// slightly slow API clock might still accept its ticket.
func cleanupExpired(ctx context.Context, db cleanupExecutor, now time.Time) {
	cutoff := now.Add(-cleanupGrace).Unix()
	for _, table := range []string{"sessions", "gm_sessions", "platform_nonces", "launch_grants", "game_sessions"} {
		if ctx.Err() != nil {
			return
		}
		queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := db.ExecContext(queryCtx, "DELETE FROM "+table+" WHERE expires_at < ? ORDER BY expires_at LIMIT ?", cutoff, cleanupBatch)
		cancel()
		if err != nil {
			slog.Error("expired_records_cleanup_failed", "table", table, "error", err)
			continue
		}
		count, err := result.RowsAffected()
		if err != nil {
			slog.Error("expired_records_cleanup_count_failed", "table", table, "error", err)
		} else if count == cleanupBatch {
			slog.Warn("expired_records_cleanup_batch_full", "table", table, "deleted", count)
		} else if count > 0 {
			slog.Info("expired_records_cleanup", "table", table, "deleted", count)
		}
	}
}

// StartCleanup runs bounded maintenance until its returned stop function is
// called. The caller must stop it before closing the database.
func StartCleanup(ctx context.Context, db *sql.DB) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		cleanupExpired(ctx, db, time.Now())
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				cleanupExpired(ctx, db, now)
			}
		}
	}()
	return func() { cancel(); <-done }
}
