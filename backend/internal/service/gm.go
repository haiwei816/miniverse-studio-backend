package service

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func passwordHash(password, salt string) string {
	b, e := pbkdf2.Key(sha256.New, password, []byte(salt), 100000, 32)
	if e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (s *Service) seedGM() error {
	if !s.Sandbox {
		return nil
	}
	for _, v := range []struct{ id, role string }{{"admin", "admin"}, {"reviewer", "reviewer"}, {"support", "support"}, {"operator", "operator"}} {
		salt := "sandbox-fixture-" + v.id
		if _, e := s.DB.Exec("INSERT IGNORE INTO admins VALUES(?,?,?,?)", v.id, passwordHash("sandbox-"+v.id, salt), salt, v.role); e != nil {
			return e
		}
		if _, e := s.DB.Exec("INSERT IGNORE INTO gm_access VALUES(?,'sky-island')", v.id); e != nil {
			return e
		}
	}
	return nil
}

type GM struct{ ID, Role, Game string }

func (s *Service) gmSession(r *http.Request) (GM, error) {
	var a GM
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return a, bad(401, "gm_unauthorized")
	}
	var salt string
	e := s.DB.QueryRowContext(r.Context(), "SELECT a.id,a.role,a.salt FROM gm_sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=? AND s.expires_at>?", hash(auth), time.Now().Unix()).Scan(&a.ID, &a.Role, &salt)
	if e != nil {
		return a, bad(401, "gm_unauthorized")
	}
	if !s.Sandbox && strings.HasPrefix(salt, "sandbox-fixture-") {
		return a, bad(403, "sandbox_admin_disabled")
	}
	return a, nil
}
func (s *Service) gm(r *http.Request, permission string) (GM, error) {
	a, e := s.gmSession(r)
	if e != nil {
		return a, e
	}
	a.Game = r.PathValue("game")
	var n int
	if e = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM gm_access WHERE admin_id=? AND game_id=?", a.ID, a.Game).Scan(&n); e != nil {
		return a, e
	}
	if n != 1 {
		return a, bad(403, "game_forbidden")
	}
	allowed := a.Role == "admin" || permission == "read" || (permission == "write" && a.Role == "operator") || (permission == "approve" && a.Role == "reviewer")
	if !allowed {
		return a, bad(403, "permission_denied")
	}
	return a, nil
}
func audit(r *http.Request, tx *sql.Tx, a GM, action, reason string, details any) error {
	if len(reason) < 3 || len(reason) > 256 {
		return bad(400, "reason_required")
	}
	b, _ := json.Marshal(details)
	_, e := tx.ExecContext(r.Context(), "INSERT INTO audits(admin_id,game_id,action,reason,details,created_at) VALUES(?,?,?,?,?,?)", a.ID, a.Game, action, reason, b, time.Now().Unix())
	return e
}
func (s *Service) gmRoutes(m *http.ServeMux) {
	s.handle(m, "POST /gm/login", func(r *http.Request) (any, error) {
		var b struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		var expected, salt, role string
		if e := s.DB.QueryRowContext(r.Context(), "SELECT password_hash,salt,role FROM admins WHERE id=?", b.Username).Scan(&expected, &salt, &role); e != nil {
			return nil, bad(401, "invalid_credentials")
		}
		if !s.Sandbox && strings.HasPrefix(salt, "sandbox-fixture-") {
			return nil, bad(403, "sandbox_admin_disabled")
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(passwordHash(b.Password, salt))) != 1 {
			return nil, bad(401, "invalid_credentials")
		}
		token := random()
		_, e := s.DB.ExecContext(r.Context(), "INSERT INTO gm_sessions VALUES(?,?,?)", hash("Bearer "+token), b.Username, time.Now().Add(30*time.Minute).Unix())
		return map[string]any{"access_token": token, "role": role, "username": b.Username}, e
	})
	s.handle(m, "GET /gm/games", func(r *http.Request) (any, error) {
		a, e := s.gmSession(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT g.id,g.name FROM games g JOIN gm_access a ON a.game_id=g.id WHERE a.admin_id=? AND g.active=TRUE ORDER BY g.id", a.ID)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, name string
			if e = rows.Scan(&id, &name); e != nil {
				return nil, e
			}
			out = append(out, map[string]string{"game_id": id, "name": name})
		}
		return out, rows.Err()
	})
	s.handle(m, "GET /gm/games/{game}/players", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		q := r.URL.Query().Get("q")
		rows, e := s.DB.QueryContext(r.Context(), "SELECT player_id,name FROM profiles WHERE game_id=? AND (player_id=? OR name LIKE ?) ORDER BY player_id LIMIT 100", a.Game, q, "%"+q+"%")
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, n string
			if e = rows.Scan(&id, &n); e != nil {
				return nil, e
			}
			out = append(out, map[string]string{"player_id": id, "name": n})
		}
		return out, rows.Err()
	})
	s.handle(m, "GET /gm/games/{game}/players/{player}", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		p := r.PathValue("player")
		var name string
		if e = s.DB.QueryRowContext(r.Context(), "SELECT name FROM profiles WHERE game_id=? AND player_id=?", a.Game, p).Scan(&name); e != nil {
			return nil, bad(404, "player_not_found")
		}
		out := map[string]any{"player_id": p, "name": name}
		for key, query := range map[string]string{
			"saves":            "SELECT slot,revision,schema_version,deleted FROM saves WHERE game_id=? AND player_id=?",
			"grants":           "SELECT id,business_key,item,amount FROM grants WHERE game_id=? AND player_id=?",
			"inventory":        "SELECT item,amount FROM inventory WHERE game_id=? AND player_id=? ORDER BY item",
			"inventory_ledger": "SELECT id,item,delta,balance,reason,business_key,created_at FROM inventory_ledger WHERE game_id=? AND player_id=? ORDER BY id DESC LIMIT 50",
			"mail":             "SELECT m.id,m.title,m.claimed,COALESCE(st.read_at,0)>0 AS is_read,COALESCE(st.deleted_at,0)>0 AS deleted,m.expires_at FROM mail m LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.player_id=? ORDER BY m.expires_at DESC,m.id LIMIT 100",
		} {
			rows, e := s.DB.QueryContext(r.Context(), query, a.Game, p)
			if e != nil {
				return nil, e
			}
			cols, _ := rows.Columns()
			items := []any{}
			for rows.Next() {
				vals := make([]any, len(cols))
				refs := make([]any, len(cols))
				for i := range vals {
					refs[i] = &vals[i]
				}
				if e = rows.Scan(refs...); e != nil {
					rows.Close()
					return nil, e
				}
				v := map[string]any{}
				for i, c := range cols {
					if b, ok := vals[i].([]byte); ok {
						v[c] = string(b)
					} else {
						v[c] = vals[i]
					}
				}
				items = append(items, v)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, e
			}
			out[key] = items
		}
		return out, nil
	})
	s.handle(m, "GET /gm/games/{game}/audit", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,admin_id,action,reason,details,created_at FROM audits WHERE game_id=? ORDER BY id DESC LIMIT 200", a.Game)
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
