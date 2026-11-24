package service

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type RewardRequest struct {
	Players []string `json:"players"`
	Item    string   `json:"item"`
	Amount  int64    `json:"amount"`
}

func (s *Service) validatePayload(r *http.Request, g, kind string, raw json.RawMessage) error {
	switch kind {
	case "reward":
		var b RewardRequest
		if e := json.Unmarshal(raw, &b); e != nil || len(b.Players) < 1 || len(b.Players) > 100 || b.Amount < 1 || b.Amount > 1000000 {
			return bad(400, "invalid_reward_request")
		}
		var n int
		if e := s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, b.Item).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return bad(400, "unknown_item")
		}
		seen := make(map[string]struct{}, len(b.Players))
		unique := make([]string, 0, len(b.Players))
		for _, p := range b.Players {
			if p == "" {
				return bad(400, "unknown_player")
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			unique = append(unique, p)
		}
		placeholders := make([]string, len(unique))
		args := make([]any, 0, 1+len(unique))
		args = append(args, g)
		for i, p := range unique {
			placeholders[i] = "?"
			args = append(args, p)
		}
		if e := s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM profiles WHERE game_id=? AND player_id IN ("+strings.Join(placeholders, ",")+")", args...).Scan(&n); e != nil {
			return e
		}
		if n != len(unique) {
			return bad(400, "unknown_player")
		}
	case "activity":
		a, e := readActivity(raw)
		if e != nil {
			return bad(400, "invalid_activity")
		}
		if e = validateActivity(a); e != nil {
			return e
		}
		var n int
		if e = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, a.Item).Scan(&n); e != nil {
			return e
		}
		if a.Amount > 0 && n != 1 {
			return bad(400, "unknown_item")
		}
	default:
		return bad(400, "invalid_request_kind")
	}
	return nil
}
func (s *Service) operationsRoutes(m *http.ServeMux) {
	base := "/gm/games/{game}"
	s.handle(m, "POST "+base+"/requests", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var b struct {
			ID      string          `json:"id"`
			Kind    string          `json:"kind"`
			Reason  string          `json:"reason"`
			Payload json.RawMessage `json:"payload"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if b.ID == "" || len(b.ID) > 64 {
			return nil, bad(400, "invalid_request_id")
		}
		if e = s.validatePayload(r, a.Game, b.Kind, b.Payload); e != nil {
			return nil, e
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var creator, kind string
			var old []byte
			e := tx.QueryRowContext(r.Context(), "SELECT creator,kind,payload FROM gm_requests WHERE game_id=? AND id=? FOR UPDATE", a.Game, b.ID).Scan(&creator, &kind, &old)
			if e == nil {
				var x, y any
				json.Unmarshal(old, &x)
				json.Unmarshal(b.Payload, &y)
				cx, _ := json.Marshal(x)
				cy, _ := json.Marshal(y)
				if creator != a.ID || kind != b.Kind || string(cx) != string(cy) {
					return bad(409, "idempotency_conflict")
				}
				return nil
			}
			if e != sql.ErrNoRows {
				return e
			}
			if _, e = tx.ExecContext(r.Context(), "INSERT INTO gm_requests(game_id,id,kind,creator,state,reason,payload,created_at) VALUES(?,?,?,?,'pending',?,?,?)", a.Game, b.ID, b.Kind, a.ID, b.Reason, b.Payload, time.Now().Unix()); e != nil {
				return e
			}
			return audit(r, tx, a, "request_created", b.Reason, map[string]any{"request_id": b.ID, "kind": b.Kind})
		})
		return map[string]string{"id": b.ID}, e
	})
	s.handle(m, "GET "+base+"/requests", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,kind,creator,COALESCE(reviewer,''),state,reason,payload FROM gm_requests WHERE game_id=? ORDER BY created_at DESC,id LIMIT 200", a.Game)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, kind, creator, reviewer, state, reason string
			var payload json.RawMessage
			if e = rows.Scan(&id, &kind, &creator, &reviewer, &state, &reason, &payload); e != nil {
				return nil, e
			}
			out = append(out, map[string]any{"id": id, "kind": kind, "creator": creator, "reviewer": reviewer, "state": state, "reason": reason, "payload": payload})
		}
		return out, rows.Err()
	})
	for _, action := range []string{"approve", "cancel"} {
		action := action
		s.handle(m, "POST "+base+"/requests/{request}/"+action, func(r *http.Request) (any, error) {
			perm := "approve"
			if action == "cancel" {
				perm = "write"
			}
			a, e := s.gm(r, perm)
			if e != nil {
				return nil, e
			}
			var b struct {
				Reason string `json:"reason"`
			}
			if e = decode(r, &b); e != nil {
				return nil, e
			}
			e = s.transaction(r.Context(), func(tx *sql.Tx) error {
				var creator, state, kind string
				var raw []byte
				id := r.PathValue("request")
				if e := tx.QueryRowContext(r.Context(), "SELECT creator,state,kind,payload FROM gm_requests WHERE game_id=? AND id=? FOR UPDATE", a.Game, id).Scan(&creator, &state, &kind, &raw); e != nil {
					return bad(404, "request_not_found")
				}
				if action == "approve" && creator == a.ID {
					return bad(403, "self_approval_forbidden")
				}
				if state == "executed" && action == "approve" {
					return nil
				}
				if state != "pending" {
					return bad(409, "request_not_pending")
				}
				next := "cancelled"
				if action == "approve" {
					next = "executed"
					switch kind {
					case "reward":
						var v RewardRequest
						if e := json.Unmarshal(raw, &v); e != nil {
							return e
						}
						for _, p := range v.Players {
							if e := grant(r.Context(), tx, a.Game, p, "gm:"+id, v.Item, v.Amount); e != nil {
								return e
							}
						}
					case "activity":
						v, e := readActivity(raw)
						if e != nil {
							return e
						}
						var current int
						e = tx.QueryRowContext(r.Context(), "SELECT version FROM activities WHERE game_id=? AND id=? FOR UPDATE", a.Game, v.ID).Scan(&current)
						if e != nil && e != sql.ErrNoRows {
							return e
						}
						if v.Version <= current {
							return bad(409, "activity_version_conflict")
						}
						if _, e = tx.ExecContext(r.Context(), "INSERT INTO activity_versions(game_id,id,version,config) VALUES(?,?,?,?)", a.Game, v.ID, v.Version, raw); e != nil {
							return e
						}
						if _, e = tx.ExecContext(r.Context(), "INSERT INTO activities(game_id,id,version,config,active) VALUES(?,?,?,?,TRUE) ON DUPLICATE KEY UPDATE version=VALUES(version),config=VALUES(config),active=TRUE", a.Game, v.ID, v.Version, raw); e != nil {
							return e
						}
					}
				}
				if _, e := tx.ExecContext(r.Context(), "UPDATE gm_requests SET state=?,reviewer=? WHERE game_id=? AND id=?", next, a.ID, a.Game, id); e != nil {
					return e
				}
				return audit(r, tx, a, "request_"+action, b.Reason, map[string]string{"request_id": id, "kind": kind})
			})
			return map[string]bool{"ok": true}, e
		})
	}
	s.handle(m, "POST "+base+"/activities/{activity}/stop", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var b struct {
			Reason string `json:"reason"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			res, e := tx.ExecContext(r.Context(), "UPDATE activities SET active=FALSE WHERE game_id=? AND id=?", a.Game, r.PathValue("activity"))
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return bad(404, "activity_not_found")
			}
			return audit(r, tx, a, "activity_stopped", b.Reason, map[string]string{"activity_id": r.PathValue("activity")})
		})
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET "+base+"/activities/{activity}/versions", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT config FROM activity_versions WHERE game_id=? AND id=? ORDER BY version DESC", a.Game, r.PathValue("activity"))
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []json.RawMessage{}
		for rows.Next() {
			var v json.RawMessage
			if e = rows.Scan(&v); e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		return out, rows.Err()
	})
	s.handle(m, "PUT "+base+"/announcements/{announcement}", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var b struct {
			Body    string `json:"body"`
			Expires int64  `json:"expires_at"`
			Reason  string `json:"reason"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.Body) > 2000 || b.Expires <= time.Now().Unix() {
			return nil, bad(400, "invalid_announcement")
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(r.Context(), "INSERT INTO announcements(game_id,id,body,expires_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE body=VALUES(body),expires_at=VALUES(expires_at)", a.Game, r.PathValue("announcement"), b.Body, b.Expires); e != nil {
				return e
			}
			return audit(r, tx, a, "announcement_updated", b.Reason, map[string]string{"id": r.PathValue("announcement")})
		})
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET /v1/games/{game}/announcements", func(r *http.Request) (any, error) {
		g, _, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,body FROM announcements WHERE game_id=? AND expires_at>?", g, time.Now().Unix())
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, body string
			if e = rows.Scan(&id, &body); e != nil {
				return nil, e
			}
			out = append(out, map[string]string{"id": id, "body": body})
		}
		return out, rows.Err()
	})
}
