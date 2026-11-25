package service

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"miniverse/backend/internal/httpserver"
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
		if _, e := s.DB.Exec("INSERT IGNORE INTO admins(id,password_hash,salt,role) VALUES(?,?,?,?)", v.id, passwordHash("sandbox-"+v.id, salt), salt, v.role); e != nil {
			return e
		}
		if _, e := s.DB.Exec("INSERT IGNORE INTO gm_access(admin_id,game_id) VALUES(?,'sky-island')", v.id); e != nil {
			return e
		}
		if v.role == "admin" || v.role == "operator" || v.role == "reviewer" {
			if _, e := s.DB.Exec("INSERT IGNORE INTO gm_access(admin_id,game_id) VALUES(?,'witch-cauldron')", v.id); e != nil {
				return e
			}
		}
	}
	return nil
}

type GM struct{ ID, Role, Game string }

func (s *Service) gmSessionToken(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth, nil
	}
	c, err := r.Cookie("mv_gm_session")
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return "", bad(401, "gm_unauthorized")
	}
	if err := requireCookieCSRF(r); err != nil {
		return "", err
	}
	return "Bearer " + strings.TrimSpace(c.Value), nil
}

func (s *Service) gmSession(r *http.Request) (GM, error) {
	var a GM
	auth, err := s.gmSessionToken(r)
	if err != nil {
		return a, err
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
func (s *Service) authorizeGMGame(ctx context.Context, a GM, game, permission string) (GM, error) {
	a.Game = game
	var n int
	if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM gm_access WHERE admin_id=? AND game_id=?", a.ID, a.Game).Scan(&n); e != nil {
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
func (s *Service) gm(r *http.Request, permission string) (GM, error) {
	a, e := s.gmSession(r)
	if e != nil {
		return a, e
	}
	return s.authorizeGMGame(r.Context(), a, r.PathValue("game"), permission)
}
func audit(r *http.Request, tx *sql.Tx, a GM, action, reason string, details any) error {
	return auditCtx(r.Context(), tx, a, action, reason, details)
}
func auditCtx(ctx context.Context, tx *sql.Tx, a GM, action, reason string, details any) error {
	if len(reason) < 3 || len(reason) > 256 {
		return bad(400, "reason_required")
	}
	reqID := httpserver.RequestID(ctx)
	payload := details
	if reqID != "" {
		switch m := details.(type) {
		case map[string]any:
			cloned := make(map[string]any, len(m)+1)
			for k, v := range m {
				cloned[k] = v
			}
			if _, ok := cloned["request_id"]; !ok {
				cloned["request_id"] = reqID
			}
			payload = cloned
		case map[string]string:
			cloned := make(map[string]any, len(m)+1)
			for k, v := range m {
				cloned[k] = v
			}
			if _, ok := cloned["request_id"]; !ok {
				cloned["request_id"] = reqID
			}
			payload = cloned
		}
	}
	b, _ := json.Marshal(payload)
	_, e := tx.ExecContext(ctx, "INSERT INTO audits(admin_id,game_id,action,reason,details,created_at) VALUES(?,?,?,?,?,?)", a.ID, a.Game, action, reason, b, time.Now().Unix())
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
		username := strings.TrimSpace(b.Username)
		if !s.allowGMAuth(clientIP(r, s.TrustedProxies), username, time.Now()) {
			return nil, bad(429, "auth_rate_limited")
		}
		if username == "" || len(username) > 64 || len(b.Password) == 0 || len(b.Password) > 128 {
			return nil, bad(401, "invalid_credentials")
		}
		var expected, salt, role string
		e := s.DB.QueryRowContext(r.Context(), "SELECT password_hash,salt,role FROM admins WHERE id=?", username).Scan(&expected, &salt, &role)
		if errors.Is(e, sql.ErrNoRows) {
			if _, hashErr := s.platformPasswordHash(b.Password, "missing-gm-account"); hashErr != nil {
				return nil, hashErr
			}
			return nil, bad(401, "invalid_credentials")
		}
		if e != nil {
			return nil, e
		}
		if !s.Sandbox && strings.HasPrefix(salt, "sandbox-fixture-") {
			return nil, bad(403, "sandbox_admin_disabled")
		}
		derived, hashErr := s.platformPasswordHash(b.Password, salt)
		if hashErr != nil {
			return nil, hashErr
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(derived)) != 1 {
			return nil, bad(401, "invalid_credentials")
		}
		token := random()
		_, e = s.DB.ExecContext(r.Context(), "INSERT INTO gm_sessions(token_hash,admin_id,expires_at) VALUES(?,?,?)", hash("Bearer "+token), username, time.Now().Add(30*time.Minute).Unix())
		if e == nil {
			setResponseCookie(r, gmSessionCookie(token, 1800, s.Sandbox))
		}
		return map[string]any{"access_token": token, "role": role, "username": username}, e
	})
	s.handle(m, "GET /gm/me", func(r *http.Request) (any, error) {
		a, e := s.gmSession(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT g.id,g.name FROM games g JOIN gm_access a ON a.game_id=g.id WHERE a.admin_id=? AND g.active=TRUE ORDER BY g.id", a.ID)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		games := []any{}
		for rows.Next() {
			var id, name string
			if e = rows.Scan(&id, &name); e != nil {
				return nil, e
			}
			games = append(games, map[string]string{"game_id": id, "name": name})
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		return map[string]any{"username": a.ID, "role": a.Role, "games": games}, nil
	})
	s.handle(m, "POST /gm/logout", func(r *http.Request) (any, error) {
		auth, e := s.gmSessionToken(r)
		if e != nil {
			return nil, e
		}
		if _, e := s.DB.ExecContext(r.Context(), "DELETE FROM gm_sessions WHERE token_hash=?", hash(auth)); e != nil {
			return nil, e
		}
		setResponseCookie(r, gmSessionCookie("", -1, s.Sandbox))
		return map[string]bool{"ok": true}, nil
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
			"grants":           "SELECT id,business_key,item,amount FROM grants WHERE game_id=? AND player_id=? ORDER BY id DESC LIMIT 100",
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
		var banReason, bannedBy string
		var bannedAt int64
		var expiresAt sql.NullInt64
		e = s.DB.QueryRowContext(r.Context(), "SELECT reason,banned_by,banned_at,expires_at FROM player_bans WHERE game_id=? AND player_id=? AND (expires_at IS NULL OR expires_at>?)", a.Game, p, time.Now().Unix()).Scan(&banReason, &bannedBy, &bannedAt, &expiresAt)
		if e == nil {
			banInfo := map[string]any{
				"banned":    true,
				"reason":    banReason,
				"banned_by": bannedBy,
				"banned_at": bannedAt,
			}
			if expiresAt.Valid {
				banInfo["expires_at"] = expiresAt.Int64
			}
			out["ban"] = banInfo
		} else if errors.Is(e, sql.ErrNoRows) {
			out["ban"] = nil
		} else {
			return nil, e
		}
		return out, nil
	})
	s.handle(m, "POST /gm/games/{game}/players/{player}/ban", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "approve")
		if e != nil {
			return nil, e
		}
		var b struct {
			Reason    string `json:"reason"`
			ExpiresAt *int64 `json:"expires_at"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		reason := strings.TrimSpace(b.Reason)
		if b.ExpiresAt != nil && *b.ExpiresAt <= time.Now().Unix() {
			return nil, bad(400, "invalid_ban_expiry")
		}
		p := r.PathValue("player")
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			return s.applyPlayerBanTx(r.Context(), tx, a, p, reason, b.ExpiresAt)
		})
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "POST /gm/games/{game}/players/{player}/unban", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "approve")
		if e != nil {
			return nil, e
		}
		var b struct {
			Reason string `json:"reason"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		reason := strings.TrimSpace(b.Reason)
		p := r.PathValue("player")
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			res, e := tx.ExecContext(r.Context(), "DELETE FROM player_bans WHERE game_id=? AND player_id=?", a.Game, p)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return bad(404, "ban_not_found")
			}
			return audit(r, tx, a, "player_unbanned", reason, map[string]string{"player_id": p})
		})
		return map[string]bool{"ok": true}, e
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

func (s *Service) applyPlayerBanTx(ctx context.Context, tx *sql.Tx, a GM, player, reason string, expiresAt *int64) error {
	var n int
	if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM profiles WHERE game_id=? AND player_id=?", a.Game, player).Scan(&n); e != nil {
		return e
	}
	if n != 1 {
		return bad(404, "player_not_found")
	}
	var exp any
	if expiresAt != nil {
		exp = *expiresAt
	}
	now := time.Now().Unix()
	if _, e := tx.ExecContext(ctx, "INSERT INTO player_bans(game_id,player_id,reason,banned_by,banned_at,expires_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE reason=VALUES(reason),banned_by=VALUES(banned_by),banned_at=VALUES(banned_at),expires_at=VALUES(expires_at)", a.Game, player, reason, a.ID, now, exp); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM game_sessions WHERE player_id=?", player); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM launch_grants WHERE player_id=?", player); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM sessions WHERE player_id=?", player); e != nil {
		return e
	}
	details := map[string]any{"player_id": player}
	if expiresAt != nil {
		details["expires_at"] = *expiresAt
	}
	return auditCtx(ctx, tx, a, "player_banned", reason, details)
}
