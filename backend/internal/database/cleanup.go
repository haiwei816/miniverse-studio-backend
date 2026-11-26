package database

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

const (
	cleanupInterval      = 5 * time.Minute
	cleanupGrace         = time.Hour
	opsEventsMaxAge      = 7 * 24 * time.Hour
	opsAgentMaxAge       = 30 * 24 * time.Hour
	outboxDispatchedAge  = 7 * 24 * time.Hour
	mqConsumedMaxAge     = 14 * 24 * time.Hour
	aiGatewayAuditMaxAge = 30 * 24 * time.Hour
	saveHistoryKeep      = 50
	cleanupBatch         = 1000
)

type cleanupExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// cleanupExpired bounds work per table and retains expired records for a grace
// period. In particular, a recently expired nonce must not be removed while a
// slightly slow API clock might still accept its ticket.
func cleanupExpired(ctx context.Context, db cleanupExecutor, now time.Time) {
	cutoff := now.Add(-cleanupGrace).Unix()
	for _, table := range []string{"sessions", "gm_sessions", "platform_nonces", "launch_grants", "game_sessions", "match_tickets"} {
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

// cleanupOpsEvents removes diagnostic events older than 7 days in bounded batches.
func cleanupOpsEvents(ctx context.Context, db cleanupExecutor, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	cutoff := now.Add(-opsEventsMaxAge).Unix()
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	result, err := db.ExecContext(queryCtx, "DELETE FROM ops_events WHERE created_at < ? ORDER BY created_at LIMIT ?", cutoff, cleanupBatch)
	cancel()
	if err != nil {
		slog.Error("ops_events_cleanup_failed", "error", err)
		return
	}
	if count, err := result.RowsAffected(); err == nil && count > 0 {
		slog.Info("ops_events_cleanup", "deleted", count)
	}
}

// cleanupOpsAgentHistory removes AI Ops runs and step traces older than 30 days in bounded batches.
func cleanupOpsAgentHistory(ctx context.Context, db cleanupExecutor, now time.Time) {
	cutoff := now.Add(-opsAgentMaxAge).Unix()
	for _, table := range []string{"ops_agent_steps", "ops_agent_runs"} {
		if ctx.Err() != nil {
			return
		}
		queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := db.ExecContext(queryCtx, "DELETE FROM "+table+" WHERE created_at < ? ORDER BY created_at LIMIT ?", cutoff, cleanupBatch)
		cancel()
		if err != nil {
			slog.Error("ops_agent_cleanup_failed", "table", table, "error", err)
			continue
		}
		if count, err := result.RowsAffected(); err == nil && count > 0 {
			slog.Info("ops_agent_cleanup", "table", table, "deleted", count)
		}
	}
}

// cleanupSaveHistory retains the latest 50 revisions per save slot and trims older revisions in bounded batches.
func cleanupSaveHistory(ctx context.Context, db cleanupExecutor) {
	if ctx.Err() != nil {
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	result, err := db.ExecContext(queryCtx,
		"DELETE sh FROM save_history sh JOIN saves s ON s.game_id=sh.game_id AND s.player_id=sh.player_id AND s.slot=sh.slot WHERE s.revision > ? AND sh.revision <= s.revision - ? LIMIT ?",
		saveHistoryKeep, saveHistoryKeep, cleanupBatch,
	)
	cancel()
	if err != nil {
		slog.Error("save_history_cleanup_failed", "error", err)
		return
	}
	if count, err := result.RowsAffected(); err == nil && count > 0 {
		slog.Info("save_history_cleanup", "deleted", count)
	}
}

// cleanupDistributedAndAIAudits removes dispatched outbox rows, old consumer dedupe keys, and AI gateway audits in bounded batches.
func cleanupDistributedAndAIAudits(ctx context.Context, db cleanupExecutor, now time.Time) {
	type cleanupTarget struct {
		name  string
		query string
		args  []any
	}
	targets := []cleanupTarget{
		{
			name:  "outbox_events",
			query: "DELETE FROM outbox_events WHERE status='dispatched' AND created_at < ? ORDER BY created_at LIMIT ?",
			args:  []any{now.Add(-outboxDispatchedAge).Unix(), cleanupBatch},
		},
		{
			name:  "mq_consumed_events",
			query: "DELETE FROM mq_consumed_events WHERE consumed_at < ? ORDER BY consumed_at LIMIT ?",
			args:  []any{now.Add(-mqConsumedMaxAge).Unix(), cleanupBatch},
		},
		{
			name:  "ai_gateway_audits",
			query: "DELETE FROM ai_gateway_audits WHERE created_at < ? ORDER BY created_at LIMIT ?",
			args:  []any{now.Add(-aiGatewayAuditMaxAge).Unix(), cleanupBatch},
		},
	}
	for _, tgt := range targets {
		if ctx.Err() != nil {
			return
		}
		queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, err := db.ExecContext(queryCtx, tgt.query, tgt.args...)
		cancel()
		if err != nil {
			slog.Error("distributed_cleanup_failed", "table", tgt.name, "error", err)
			continue
		}
		if count, err := result.RowsAffected(); err == nil && count > 0 {
			slog.Info("distributed_cleanup", "table", tgt.name, "deleted", count)
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
		now := time.Now()
		cleanupExpired(ctx, db, now)
		cleanupOpsEvents(ctx, db, now)
		cleanupOpsAgentHistory(ctx, db, now)
		cleanupSaveHistory(ctx, db)
		cleanupDistributedAndAIAudits(ctx, db, now)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				cleanupExpired(ctx, db, now)
				cleanupOpsEvents(ctx, db, now)
				cleanupOpsAgentHistory(ctx, db, now)
				cleanupSaveHistory(ctx, db)
				cleanupDistributedAndAIAudits(ctx, db, now)
			}
		}
	}()
	return func() { cancel(); <-done }
}
