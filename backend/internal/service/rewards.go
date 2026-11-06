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
			if e := tx.QueryRowContext(r.Context(), "SELECT config FROM activities WHERE game_id=? AND id=?", g, r.PathValue("activity")).Scan(&raw); e != nil {
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
	s.handle(m, "GET /v1/games/{game}/mail", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT m.id,m.title,m.expires_at,m.claimed,g.item,g.amount FROM mail m JOIN grants g ON g.id=m.grant_id WHERE m.game_id=? AND m.player_id=? ORDER BY g.created_at DESC,m.id LIMIT 200", g, p)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		a := []any{}
		for rows.Next() {
			var id, title, item string
			var exp, n int64
			var claimed bool
			if e = rows.Scan(&id, &title, &exp, &claimed, &item, &n); e != nil {
				return nil, e
			}
			a = append(a, map[string]any{"id": id, "title": title, "expires_at": exp, "claimed": claimed, "item": item, "amount": n})
		}
		return a, rows.Err()
	})
	s.handle(m, "POST /v1/games/{game}/mail/{mail}/claim", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var claimed bool
			var exp int64
			var item string
			var n int64
			e := tx.QueryRowContext(r.Context(), "SELECT m.claimed,m.expires_at,g.item,g.amount FROM mail m JOIN grants g ON g.id=m.grant_id WHERE m.id=? AND m.game_id=? AND m.player_id=? FOR UPDATE", r.PathValue("mail"), g, p).Scan(&claimed, &exp, &item, &n)
			if e == sql.ErrNoRows {
				return bad(404, "mail_not_found")
			}
			if e != nil {
				return e
			}
			if claimed {
				return nil
			}
			if time.Now().Unix() >= exp {
				return bad(410, "mail_expired")
			}
			if _, e = tx.ExecContext(r.Context(), "INSERT INTO inventory VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE amount=amount+?", g, p, item, n, n); e != nil {
				return e
			}
			_, e = tx.ExecContext(r.Context(), "UPDATE mail SET claimed=TRUE WHERE id=?", r.PathValue("mail"))
			return e
		})
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET /v1/games/{game}/inventory", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT item,amount FROM inventory WHERE game_id=? AND player_id=?", g, p)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		v := map[string]int64{}
		for rows.Next() {
			var k string
			var n int64
			if e = rows.Scan(&k, &n); e != nil {
				return nil, e
			}
			v[k] = n
		}
		return v, rows.Err()
	})
}
