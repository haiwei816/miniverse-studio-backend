package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

type Mail struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	ExpiresAt int64  `json:"expires_at"`
	Claimed   bool   `json:"claimed"`
	Read      bool   `json:"read"`
	Item      string `json:"item"`
	Amount    int64  `json:"amount"`
}

// mailCursor carries the full sort key, so paging survives deletion of the last
// row and rows with equal creation times. It is not an authorization token.
type mailCursor struct {
	Created int64  `json:"created"`
	ID      string `json:"id"`
}

func decodeMailCursor(raw string) (mailCursor, error) {
	var c mailCursor
	if raw == "" {
		return c, nil
	}
	if len(raw) > 512 {
		return c, bad(400, "invalid_cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, &c) != nil || c.Created < 0 || c.ID == "" || len(c.ID) > 64 {
		return mailCursor{}, bad(400, "invalid_cursor")
	}
	return c, nil
}

func encodeMailCursor(c mailCursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// The mail row serializes claim/read/delete. Mail and inventory changes commit
// together; deleted mail never returns to the player's inbox.
func claimMail(ctx context.Context, tx *sql.Tx, game, player, id string, now int64) (bool, error) {
	var claimed bool
	var expires, amount, deleted int64
	var item string
	err := tx.QueryRowContext(ctx, "SELECT m.claimed,m.expires_at,g.item,g.amount,COALESCE(st.deleted_at,0) FROM mail m JOIN grants g ON g.id=m.grant_id LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.id=? AND m.game_id=? AND m.player_id=? FOR UPDATE", id, game, player).Scan(&claimed, &expires, &item, &amount, &deleted)
	if err == sql.ErrNoRows || (err == nil && deleted != 0) {
		return false, bad(404, "mail_not_found")
	}
	if err != nil {
		return false, err
	}
	if claimed {
		return false, nil
	}
	if now >= expires {
		return false, bad(410, "mail_expired")
	}
	if _, err = creditInventory(ctx, tx, game, player, item, amount, "mail:"+id, "mail_claim", now); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mail SET claimed=TRUE WHERE id=?", id); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO mail_states(mail_id,read_at) VALUES(?,?) ON DUPLICATE KEY UPDATE read_at=IF(read_at=0,VALUES(read_at),read_at)", id, now)
	return true, err
}

func (s *Service) mailRoutes(m *http.ServeMux) {
	base := "/v1/games/{game}/mail"
	s.handle(m, "GET "+base, func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		limit, err := queryLimit(r, 200)
		if err != nil {
			return nil, err
		}
		filter := r.URL.Query().Get("filter")
		condition := ""
		switch filter {
		case "", "all":
		case "unread":
			condition = " AND COALESCE(st.read_at,0)=0"
		case "unclaimed":
			condition = " AND m.claimed=FALSE AND m.expires_at>UNIX_TIMESTAMP()"
		default:
			return nil, bad(400, "invalid_filter")
		}
		rows, err := s.DB.QueryContext(r.Context(), "SELECT m.id,m.title,m.expires_at,m.claimed,COALESCE(st.read_at,0)>0,g.item,g.amount FROM mail m JOIN grants g ON g.id=m.grant_id LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.player_id=? AND COALESCE(st.deleted_at,0)=0"+condition+" ORDER BY g.created_at DESC,m.id LIMIT ?", g, p, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []Mail{}
		for rows.Next() {
			var v Mail
			if err = rows.Scan(&v.ID, &v.Title, &v.ExpiresAt, &v.Claimed, &v.Read, &v.Item, &v.Amount); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, rows.Err()
	})
	// Keep the original array-returning endpoint compatible; clients opt into
	// pagination through /mail/page.
	s.handle(m, "GET "+base+"/page", func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		limit, err := queryLimit(r, 200)
		if err != nil {
			return nil, err
		}
		cursor, err := decodeMailCursor(r.URL.Query().Get("cursor"))
		if err != nil {
			return nil, err
		}
		condition := ""
		switch r.URL.Query().Get("filter") {
		case "", "all":
		case "unread":
			condition = " AND COALESCE(st.read_at,0)=0"
		case "unclaimed":
			condition = " AND m.claimed=FALSE AND m.expires_at>UNIX_TIMESTAMP()"
		default:
			return nil, bad(400, "invalid_filter")
		}
		args := []any{g, p}
		if cursor.ID != "" {
			condition += " AND (g.created_at<? OR (g.created_at=? AND m.id>?))"
			args = append(args, cursor.Created, cursor.Created, cursor.ID)
		}
		args = append(args, limit+1)
		rows, err := s.DB.QueryContext(r.Context(), "SELECT m.id,m.title,m.expires_at,m.claimed,COALESCE(st.read_at,0)>0,g.item,g.amount,g.created_at FROM mail m JOIN grants g ON g.id=m.grant_id LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.player_id=? AND COALESCE(st.deleted_at,0)=0"+condition+" ORDER BY g.created_at DESC,m.id ASC LIMIT ?", args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []Mail{}
		var last mailCursor
		hasMore := false
		for rows.Next() {
			var v Mail
			var created int64
			if err = rows.Scan(&v.ID, &v.Title, &v.ExpiresAt, &v.Claimed, &v.Read, &v.Item, &v.Amount, &created); err != nil {
				return nil, err
			}
			if len(items) == limit {
				hasMore = true
				break
			}
			items = append(items, v)
			last = mailCursor{Created: created, ID: v.ID}
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		next := ""
		if hasMore {
			next = encodeMailCursor(last)
		}
		return map[string]any{"items": items, "next_cursor": next}, nil
	})
	s.handle(m, "POST "+base+"/{mail}/claim", func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		err = s.transaction(r.Context(), func(tx *sql.Tx) error {
			_, err := claimMail(r.Context(), tx, g, p, r.PathValue("mail"), time.Now().Unix())
			return err
		})
		return map[string]bool{"ok": true}, err
	})
	s.handle(m, "POST "+base+"/claim-all", func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		claimed := []string{}
		remaining := 0
		err = s.transaction(r.Context(), func(tx *sql.Tx) error {
			// Reset response state when the transaction is retried after a deadlock.
			claimed = []string{}
			now := time.Now().Unix()
			rows, err := tx.QueryContext(r.Context(), "SELECT m.id FROM mail m LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.player_id=? AND m.claimed=FALSE AND m.expires_at>? AND COALESCE(st.deleted_at,0)=0 ORDER BY m.id LIMIT 100 FOR UPDATE", g, p, now)
			if err != nil {
				return err
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, id := range ids {
				ok, err := claimMail(r.Context(), tx, g, p, id, now)
				if err != nil {
					return err
				}
				if ok {
					claimed = append(claimed, id)
				}
			}
			return tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM mail m LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.player_id=? AND m.claimed=FALSE AND m.expires_at>? AND COALESCE(st.deleted_at,0)=0", g, p, now).Scan(&remaining)
		})
		return map[string]any{"claimed": claimed, "count": len(claimed), "has_more": remaining > 0}, err
	})
	state := func(r *http.Request) (any, error) {
		g, p, err := s.scope(r)
		if err != nil {
			return nil, err
		}
		err = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var claimed bool
			var expires int64
			id := r.PathValue("mail")
			err := tx.QueryRowContext(r.Context(), "SELECT claimed,expires_at FROM mail WHERE id=? AND game_id=? AND player_id=? FOR UPDATE", id, g, p).Scan(&claimed, &expires)
			if err == sql.ErrNoRows {
				return bad(404, "mail_not_found")
			}
			if err != nil {
				return err
			}
			now := time.Now().Unix()
			if r.Method == "DELETE" {
				if !claimed && expires > now {
					return bad(409, "unclaimed_attachment")
				}
				_, err = tx.ExecContext(r.Context(), "INSERT INTO mail_states(mail_id,read_at,deleted_at) VALUES(?,?,?) ON DUPLICATE KEY UPDATE deleted_at=IF(deleted_at=0,VALUES(deleted_at),deleted_at)", id, now, now)
				return err
			}
			var deleted int64
			if err = tx.QueryRowContext(r.Context(), "SELECT COALESCE((SELECT deleted_at FROM mail_states WHERE mail_id=?),0)", id).Scan(&deleted); err != nil {
				return err
			}
			if deleted != 0 {
				return bad(404, "mail_not_found")
			}
			_, err = tx.ExecContext(r.Context(), "INSERT INTO mail_states(mail_id,read_at) VALUES(?,?) ON DUPLICATE KEY UPDATE read_at=IF(read_at=0,VALUES(read_at),read_at)", id, now)
			return err
		})
		return map[string]bool{"ok": true}, err
	}
	s.handle(m, "POST "+base+"/{mail}/read", state)
	s.handle(m, "DELETE "+base+"/{mail}", state)
}
