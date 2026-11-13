package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

func grant(ctx context.Context, tx *sql.Tx, g, p, key, item string, amount int64) error {
	if amount < 1 || amount > 1000000 {
		return bad(400, "invalid_reward_amount")
	}
	var n int
	if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, item).Scan(&n); e != nil {
		return e
	}
	if n != 1 {
		return bad(400, "unknown_item")
	}
	id := hash(g + "/" + p + "/" + key)[:48]
	if _, e := tx.ExecContext(ctx, "INSERT IGNORE INTO grants VALUES(?,?,?,?,?,?,?)", id, g, p, key, item, amount, time.Now().Unix()); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, "INSERT IGNORE INTO mail VALUES(?,?,?,?,?,?,FALSE)", id, g, p, id, "奖励到账", time.Now().Add(30*24*time.Hour).Unix())
	return e
}
func (s *Service) rewardRoutes(m *http.ServeMux) {
	s.handle(m, "POST /v1/games/{game}/activities/{activity}/reward", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var raw []byte
			if e := tx.QueryRowContext(r.Context(), "SELECT config FROM activities WHERE game_id=? AND id=? AND active=TRUE", g, r.PathValue("activity")).Scan(&raw); e != nil {
				return bad(404, "activity_not_found")
			}
			a, e := readActivity(raw)
			if e != nil {
				return e
			}
			var n int64
			bucket := bucketFor(a, time.Now())
			if e = tx.QueryRowContext(r.Context(), "SELECT progress FROM activity_progress WHERE game_id=? AND player_id=? AND activity_id=? AND version=? AND bucket=?", g, p, a.ID, a.Version, bucket).Scan(&n); e != nil || n < a.Target {
				return bad(409, "activity_incomplete")
			}
			return grant(r.Context(), tx, g, p, fmt.Sprintf("activity:%s:%d:%s", a.ID, a.Version, bucket), a.Item, a.Amount)
		})
		return map[string]bool{"ok": true}, e
	})
}
