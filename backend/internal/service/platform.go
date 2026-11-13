package service

import (
	"database/sql"
	"miniverse/backend/internal/platformauth"
	"net/http"
	"os"
	"time"
)

func (s *Service) adapter(channel string) (platformauth.Sandbox, error) {
	if channel != "steam" && channel != "taptap" {
		return platformauth.Sandbox{}, bad(400, "unknown_channel")
	}
	key := os.Getenv("SANDBOX_PLATFORM_SECRET")
	if key == "" {
		key = "local-sandbox-not-a-platform-key"
	}
	return platformauth.Sandbox{Channel: channel, Key: []byte(key), Enabled: s.Sandbox}, nil
}
func (s *Service) platformRoutes(m *http.ServeMux) {
	s.handle(m, "POST /v1/sandbox/tickets/{channel}", func(r *http.Request) (any, error) {
		if !s.Sandbox {
			return nil, bad(403, "sandbox_only")
		}
		if _, e := s.actor(r); e != nil {
			return nil, e
		}
		var b struct {
			Game    string `json:"game_id"`
			Subject string `json:"subject"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		a, e := s.adapter(r.PathValue("channel"))
		if e != nil {
			return nil, e
		}
		ticket, e := a.Sign(platformauth.Identity{Channel: a.Channel, App: b.Game, Subject: b.Subject, Nonce: random(), Expires: time.Now().Add(2 * time.Minute).Unix()})
		return map[string]string{"ticket": ticket, "adapter": "sandbox", "real_platform": "unverified"}, e
	})
	for _, action := range []string{"login", "bind"} {
		action := action
		s.handle(m, "POST /v1/platform/{channel}/"+action, func(r *http.Request) (any, error) {
			var b struct {
				Game   string `json:"game_id"`
				Ticket string `json:"ticket"`
			}
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			a, e := s.adapter(r.PathValue("channel"))
			if e != nil {
				return nil, e
			}
			if !s.Sandbox {
				return nil, bad(503, "real_provider_not_configured")
			}
			v, e := a.Verify(r.Context(), b.Game, b.Ticket)
			if e != nil {
				return nil, bad(401, "invalid_platform_proof")
			}
			var active bool
			if e = s.DB.QueryRowContext(r.Context(), "SELECT active FROM games WHERE id=?", b.Game).Scan(&active); e != nil || !active {
				return nil, bad(404, "game_not_found")
			}
			player := ""
			if action == "bind" {
				player, e = s.actor(r)
				if e != nil {
					return nil, e
				}
			}
			token := random()
			e = s.transaction(r.Context(), func(tx *sql.Tx) error {
				res, e := tx.ExecContext(r.Context(), "INSERT IGNORE INTO platform_nonces VALUES(?,?)", v.Nonce, v.Expires)
				if e != nil {
					return e
				}
				n, _ := res.RowsAffected()
				if n == 0 {
					return bad(409, "platform_proof_replayed")
				}
				var owner string
				e = tx.QueryRowContext(r.Context(), "SELECT player_id FROM platform_identities WHERE channel=? AND app_id=? AND subject=? FOR UPDATE", v.Channel, v.App, v.Subject).Scan(&owner)
				if e != nil && e != sql.ErrNoRows {
					return e
				}
				if owner != "" {
					if action == "bind" && owner != player {
						return bad(409, "identity_already_bound")
					}
					player = owner
				} else {
					if action == "login" {
						player = random()
						if _, e = tx.ExecContext(r.Context(), "INSERT INTO accounts VALUES(?,?)", player, hash(random())); e != nil {
							return e
						}
					}
					if _, e = tx.ExecContext(r.Context(), "INSERT INTO platform_identities VALUES(?,?,?,?)", v.Channel, v.App, v.Subject, player); e != nil {
						return e
					}
				}
				_, e = tx.ExecContext(r.Context(), "INSERT INTO sessions VALUES(?,?,?)", s.sessionHash(token), player, time.Now().Add(time.Hour).Unix())
				return e
			})
			return map[string]any{"player_id": player, "access_token": token, "adapter": "sandbox", "real_platform": "unverified"}, e
		})
	}
	s.handle(m, "POST /v1/platform/{channel}/unbind", func(r *http.Request) (any, error) {
		p, e := s.actor(r)
		if e != nil {
			return nil, e
		}
		var b struct {
			Game   string `json:"game_id"`
			Ticket string `json:"ticket"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		a, e := s.adapter(r.PathValue("channel"))
		if e != nil {
			return nil, e
		}
		v, e := a.Verify(r.Context(), b.Game, b.Ticket)
		if e != nil {
			return nil, bad(401, "invalid_platform_proof")
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var account string
			if e := tx.QueryRowContext(r.Context(), "SELECT id FROM accounts WHERE id=? FOR UPDATE", p).Scan(&account); e != nil {
				return e
			}
			var count int
			if e := tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM platform_identities WHERE player_id=?", p).Scan(&count); e != nil {
				return e
			}
			if count < 2 {
				return bad(409, "last_identity_cannot_unbind")
			}
			res, e := tx.ExecContext(r.Context(), "INSERT IGNORE INTO platform_nonces VALUES(?,?)", v.Nonce, v.Expires)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return bad(409, "platform_proof_replayed")
			}
			res, e = tx.ExecContext(r.Context(), "DELETE FROM platform_identities WHERE channel=? AND app_id=? AND subject=? AND player_id=?", v.Channel, v.App, v.Subject, p)
			if e != nil {
				return e
			}
			n, _ = res.RowsAffected()
			if n != 1 {
				return bad(404, "identity_not_found")
			}
			return nil
		})
		return map[string]bool{"ok": true}, e
	})
}
