package database

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type cleanupCall struct {
	query string
	args  []any
}
type cleanupFake struct{ calls []cleanupCall }

func (f *cleanupFake) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	f.calls = append(f.calls, cleanupCall{q, args})
	if strings.Contains(q, "gm_sessions") {
		return nil, errors.New("temporary database failure")
	}
	return cleanupResult(1), nil
}

type cleanupResult int64

func (r cleanupResult) LastInsertId() (int64, error) { return 0, nil }
func (r cleanupResult) RowsAffected() (int64, error) { return int64(r), nil }

func TestCleanupExpiredUsesBoundedCutoffAndContinuesAfterFailure(t *testing.T) {
	f := &cleanupFake{}
	now := time.Unix(2000000000, 0)
	cleanupExpired(context.Background(), f, now)
	if len(f.calls) != 6 {
		t.Fatalf("expected all expiry tables despite one failure, got %d", len(f.calls))
	}
	for i, table := range []string{"sessions", "gm_sessions", "platform_nonces", "launch_grants", "game_sessions", "match_tickets"} {
		want := "DELETE FROM " + table + " WHERE expires_at < ? ORDER BY expires_at LIMIT ?"
		if f.calls[i].query != want || !reflect.DeepEqual(f.calls[i].args, []any{now.Add(-time.Hour).Unix(), cleanupBatch}) {
			t.Fatalf("unexpected cleanup for %s: %#v", table, f.calls[i])
		}
	}
}

func TestCleanupExpiredHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &cleanupFake{}
	cleanupExpired(ctx, f, time.Now())
	if len(f.calls) != 0 {
		t.Fatal("cleanup ran after cancellation")
	}
}

func TestCleanupOpsEventsUses7DayCutoff(t *testing.T) {
	f := &cleanupFake{}
	now := time.Unix(2000000000, 0)
	cleanupOpsEvents(context.Background(), f, now)
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 cleanup call, got %d", len(f.calls))
	}
	wantQuery := "DELETE FROM ops_events WHERE created_at < ? ORDER BY created_at LIMIT ?"
	wantArgs := []any{now.Add(-opsEventsMaxAge).Unix(), cleanupBatch}
	if f.calls[0].query != wantQuery || !reflect.DeepEqual(f.calls[0].args, wantArgs) {
		t.Fatalf("unexpected ops_events cleanup: %#v", f.calls[0])
	}
}

func TestCleanupOpsAgentHistoryAndSaveHistory(t *testing.T) {
	f := &cleanupFake{}
	now := time.Unix(2000000000, 0)
	cleanupOpsAgentHistory(context.Background(), f, now)
	cleanupSaveHistory(context.Background(), f)
	if len(f.calls) != 3 {
		t.Fatalf("expected 3 cleanup calls, got %d", len(f.calls))
	}
	if !strings.Contains(f.calls[0].query, "DELETE FROM ops_agent_steps") || !strings.Contains(f.calls[1].query, "DELETE FROM ops_agent_runs") {
		t.Fatalf("unexpected ops_agent cleanup queries: %#v", f.calls[:2])
	}
	if !strings.Contains(f.calls[2].query, "DELETE sh FROM save_history sh") {
		t.Fatalf("unexpected save_history cleanup query: %#v", f.calls[2])
	}
}
