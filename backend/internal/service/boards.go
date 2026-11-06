package service

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

type Board struct {
	ID     string `json:"id"`
	Season string `json:"season"`
	Mode   string `json:"mode"`
	Starts int64  `json:"starts_at"`
	Ends   int64  `json:"ends_at"`
	Item   string `json:"item"`
	Reward int64  `json:"reward_amount"`
}
type Rank struct {
	Player  string `json:"player_id"`
	Score   int64  `json:"score"`
	Reached int64  `json:"reached_at"`
	Rank    int    `json:"rank"`
}

func ranks(r *http.Request, tx *sql.Tx, g, id, season, mode string, first, last int) ([]Rank, error) {
	order := "DESC"
	if mode == "min" {
		order = "ASC"
	}
	rows, e := tx.QueryContext(r.Context(), "WITH ranked AS (SELECT player_id,score,reached_at,ROW_NUMBER() OVER (ORDER BY score "+order+",reached_at ASC,player_id ASC) AS place FROM scores WHERE game_id=? AND board_id=? AND season=?) SELECT player_id,score,reached_at,place FROM ranked WHERE place BETWEEN ? AND ? ORDER BY place", g, id, season, first, last)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	a := []Rank{}
	for rows.Next() {
		var v Rank
		if e = rows.Scan(&v.Player, &v.Score, &v.Reached, &v.Rank); e != nil {
			return nil, e
		}
		a = append(a, v)
	}
	return a, rows.Err()
}
func (s *Service) boardRoutes(m *http.ServeMux) {
	base := "/v1/games/{game}/boards/{board}/{season}"
	s.handle(m, "PUT /v1/games/{game}/boards", func(r *http.Request) (any, error) {
		g, _, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		if !s.Sandbox {
			return nil, bad(403, "sandbox_only")
		}
		var b Board
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if b.ID == "" || len(b.ID) > 64 || b.Season == "" || len(b.Season) > 64 || b.Starts >= b.Ends || (b.Mode != "max" && b.Mode != "min" && b.Mode != "sum") || b.Reward < 0 || b.Reward > 1000000 {
			return nil, bad(400, "invalid_board")
		}
		_, e = s.DB.ExecContext(r.Context(), "INSERT IGNORE INTO boards VALUES(?,?,?,?,?,?,FALSE,FALSE,?,?)", g, b.ID, b.Season, b.Mode, b.Starts, b.Ends, b.Item, b.Reward)
		return b, e
	})
	s.handle(m, "POST "+base+"/scores", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		var b struct {
			ID    string `json:"request_id"`
			Score int64  `json:"score"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if b.ID == "" || len(b.ID) > 128 || b.Score < 0 || b.Score > 1000000 {
			return nil, bad(400, "abnormal_score")
		}
		id, season := r.PathValue("board"), r.PathValue("season")
		var requiresReplay int
		if e = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM rule_boards WHERE game_id=? AND board_id=? AND season=?", g, id, season).Scan(&requiresReplay); e != nil {
			return nil, e
		}
		if requiresReplay > 0 {
			return nil, bad(403, "replay_required")
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var mode string
			var start, end int64
			var frozen bool
			if e := tx.QueryRowContext(r.Context(), "SELECT mode,starts_at,ends_at,frozen FROM boards WHERE game_id=? AND id=? AND season=? FOR UPDATE", g, id, season).Scan(&mode, &start, &end, &frozen); e != nil {
				return bad(404, "board_not_found")
			}
			now := time.Now()
			if frozen || now.Unix() < start || now.Unix() >= end {
				return bad(409, "season_closed")
			}
			res, e := tx.ExecContext(r.Context(), "INSERT IGNORE INTO score_events VALUES(?,?,?,?,?)", g, id, season, p, b.ID)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return nil
			}
			var old int64
			e = tx.QueryRowContext(r.Context(), "SELECT score FROM scores WHERE game_id=? AND board_id=? AND season=? AND player_id=?", g, id, season, p).Scan(&old)
			if e != nil && e != sql.ErrNoRows {
				return e
			}
			next := b.Score
			if e == nil {
				switch mode {
				case "max":
					if old >= next {
						return nil
					}
				case "min":
					if old <= next {
						return nil
					}
				case "sum":
					next += old
					if next > 1000000000 {
						return bad(400, "abnormal_total")
					}
				}
			}
			_, e = tx.ExecContext(r.Context(), "INSERT INTO scores VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE score=VALUES(score),reached_at=VALUES(reached_at)", g, id, season, p, next, now.UnixNano())
			return e
		})
		return map[string]any{"ok": true, "trust": "client_unverified"}, e
	})
	s.handle(m, "GET "+base, func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		var out any
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var mode string
			if e := tx.QueryRowContext(r.Context(), "SELECT mode FROM boards WHERE game_id=? AND id=? AND season=?", g, r.PathValue("board"), r.PathValue("season")).Scan(&mode); e != nil {
				return bad(404, "board_not_found")
			}
			top, e := ranks(r, tx, g, r.PathValue("board"), r.PathValue("season"), mode, 1, 50)
			if e != nil {
				return e
			}
			near := []Rank{}
			var score, reached int64
			e = tx.QueryRowContext(r.Context(), "SELECT score,reached_at FROM scores WHERE game_id=? AND board_id=? AND season=? AND player_id=?", g, r.PathValue("board"), r.PathValue("season"), p).Scan(&score, &reached)
			if e != nil && e != sql.ErrNoRows {
				return e
			}
			if e == nil {
				op := ">"
				if mode == "min" {
					op = "<"
				}
				var better int
				if e = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM scores WHERE game_id=? AND board_id=? AND season=? AND (score "+op+" ? OR (score=? AND reached_at<?) OR (score=? AND reached_at=? AND player_id<?))", g, r.PathValue("board"), r.PathValue("season"), score, score, reached, score, reached, p).Scan(&better); e != nil {
					return e
				}
				near, e = ranks(r, tx, g, r.PathValue("board"), r.PathValue("season"), mode, max(1, better-1), better+3)
				if e != nil {
					return e
				}
			}
			trust := "client_unverified"
			var n int
			if e = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM rule_boards WHERE game_id=? AND board_id=? AND season=?", g, r.PathValue("board"), r.PathValue("season")).Scan(&n); e != nil {
				return e
			}
			if n > 0 {
				trust = "deterministic_replay"
			}
			out = map[string]any{"top": top, "nearby": near, "trust": trust}
			return nil
		})
		return out, e
	})
	s.handle(m, "POST "+base+"/settle", func(r *http.Request) (any, error) {
		g, _, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		if !s.Sandbox {
			return nil, bad(403, "sandbox_only")
		}
		id, season := r.PathValue("board"), r.PathValue("season")
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var mode, item string
			var settled bool
			var amount, end int64
			if e := tx.QueryRowContext(r.Context(), "SELECT mode,settled,item,reward_amount,ends_at FROM boards WHERE game_id=? AND id=? AND season=? FOR UPDATE", g, id, season).Scan(&mode, &settled, &item, &amount, &end); e != nil {
				return bad(404, "board_not_found")
			}
			if settled {
				return nil
			}
			if time.Now().Unix() < end {
				return bad(409, "season_still_open")
			}
			a, e := ranks(r, tx, g, id, season, mode, 1, 3)
			if e != nil {
				return e
			}
			for _, v := range a {
				if v.Rank > 3 || amount == 0 {
					continue
				}
				if e = grant(r.Context(), tx, g, v.Player, fmt.Sprintf("season:%s:%s", id, season), item, amount); e != nil {
					return e
				}
			}
			_, e = tx.ExecContext(r.Context(), "UPDATE boards SET frozen=TRUE,settled=TRUE WHERE game_id=? AND id=? AND season=?", g, id, season)
			return e
		})
		return map[string]bool{"ok": true}, e
	})
}
