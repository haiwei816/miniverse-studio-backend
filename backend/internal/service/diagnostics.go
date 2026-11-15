package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Recording after rollback preserves failed-operation diagnostics.
func (s *Service) recordFailure(r *http.Request, code string) {
	g := r.PathValue("game")
	if g == "" {
		return
	}
	action, actor := "", ""
	details := map[string]string{"path": r.URL.Path, "method": r.Method, "error": code}
	if strings.HasPrefix(r.URL.Path, "/gm/") && r.Method != "GET" {
		a, e := s.gm(r, "read")
		if e != nil {
			return
		}
		actor = a.ID
		action = "operation_failed"
	} else if code == "abnormal_score" || code == "abnormal_total" || code == "score_mismatch" || code == "illegal_replay" {
		_, p, e := s.scope(r)
		if e != nil {
			return
		}
		actor = "system"
		details["player_id"] = p
		action = "player_anomaly"
	}
	if action == "" {
		return
	}
	raw, _ := json.Marshal(details)
	s.DB.ExecContext(r.Context(), "INSERT INTO audits(admin_id,game_id,action,reason,details,created_at) VALUES(?,?,?,?,?,?)", actor, g, action, code, raw, time.Now().Unix())
}
func (s *Service) diagnosticRoutes(m *http.ServeMux) {
	s.handle(m, "GET /gm/games/{game}/failures", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,admin_id,action,reason,details,created_at FROM audits WHERE game_id=? AND action IN ('operation_failed','player_anomaly') ORDER BY id DESC LIMIT 200", a.Game)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, ts int64
			var admin, action, reason string
			var details json.RawMessage
			if e = rows.Scan(&id, &admin, &action, &reason, &details, &ts); e != nil {
				return nil, e
			}
			out = append(out, map[string]any{"id": id, "admin": admin, "action": action, "reason": reason, "details": details, "created_at": ts})
		}
		return out, rows.Err()
	})
}
