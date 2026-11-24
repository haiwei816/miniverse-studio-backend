package service

import (
	"net/http"
	"strings"
	"time"
)

func (s *Service) identityRoutes(m *http.ServeMux) {
	s.handle(m, "POST /v1/games", func(r *http.Request) (any, error) {
		if !s.Sandbox {
			return nil, bad(403, "sandbox_only")
		}
		if _, e := s.actor(r); e != nil {
			return nil, e
		}
		var b struct {
			ID   string `json:"game_id"`
			Name string `json:"name"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.ID) < 1 || len(b.ID) > 64 || len(b.Name) > 128 {
			return nil, bad(400, "invalid_game")
		}
		_, e := s.DB.ExecContext(r.Context(), "INSERT IGNORE INTO games(id,name) VALUES(?,?)", b.ID, b.Name)
		return b, e
	})
	s.handle(m, "GET /v1/games", func(r *http.Request) (any, error) {
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,name,config_version FROM games WHERE active=TRUE ORDER BY id")
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		a := []any{}
		for rows.Next() {
			var id, n string
			var v int
			if e = rows.Scan(&id, &n, &v); e != nil {
				return nil, e
			}
			a = append(a, map[string]any{"game_id": id, "name": n, "config_version": v})
		}
		return a, rows.Err()
	})
	s.handle(m, "POST /v1/auth/guest", func(r *http.Request) (any, error) {
		if !s.Sandbox {
			return nil, bad(403, "sandbox_only")
		}
		var b struct {
			Secret string `json:"secret"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		id := random()
		secret := b.Secret
		if secret == "" {
			secret = random()
			if _, e := s.DB.ExecContext(r.Context(), "INSERT INTO accounts(id,secret_hash) VALUES(?,?)", id, hash(secret)); e != nil {
				return nil, e
			}
		} else {
			if e := s.DB.QueryRowContext(r.Context(), "SELECT id FROM accounts WHERE secret_hash=?", hash(secret)).Scan(&id); e != nil {
				return nil, bad(401, "invalid_guest_secret")
			}
		}
		token := random()
		exp := time.Now().Add(time.Hour).Unix()
		_, e := s.DB.ExecContext(r.Context(), "INSERT INTO sessions(token_hash,player_id,expires_at) VALUES(?,?,?)", s.sessionHash(token), id, exp)
		return map[string]any{"player_id": id, "secret": secret, "access_token": token, "expires_at": exp, "adapter": "sandbox"}, e
	})
	s.handle(m, "POST /v1/auth/logout", func(r *http.Request) (any, error) {
		p, e := s.actor(r)
		if e != nil {
			return nil, e
		}
		e = s.logoutPlatformSession(r.Context(), p, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET /v1/games/{game}/profile", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		if _, e = s.DB.ExecContext(r.Context(), "INSERT IGNORE INTO profiles(game_id,player_id,data) VALUES(?,?,JSON_OBJECT())", g, p); e != nil {
			return nil, e
		}
		var n string
		var data []byte
		e = s.DB.QueryRowContext(r.Context(), "SELECT name,data FROM profiles WHERE game_id=? AND player_id=?", g, p).Scan(&n, &data)
		return map[string]any{"player_id": p, "game_id": g, "name": n, "data": string(data)}, e
	})
	s.handle(m, "PATCH /v1/games/{game}/profile", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		var b struct {
			Name string `json:"name"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.Name) > 64 {
			return nil, bad(400, "name_too_long")
		}
		_, e = s.DB.ExecContext(r.Context(), "INSERT INTO profiles(game_id,player_id,name,data) VALUES(?,?,?,JSON_OBJECT()) ON DUPLICATE KEY UPDATE name=?", g, p, b.Name, b.Name)
		return b, e
	})
}
