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
	if len(f.calls) != 5 {
		t.Fatalf("expected all expiry tables despite one failure, got %d", len(f.calls))
	}
	for i, table := range []string{"sessions", "gm_sessions", "platform_nonces", "launch_grants", "game_sessions"} {
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
