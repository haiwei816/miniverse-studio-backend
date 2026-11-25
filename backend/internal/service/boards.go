package service

import (
	"context"
	"database/sql"
	"errors"
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

func ranks(ctx context.Context, tx *sql.Tx, g, id, season, mode string, first, last int) ([]Rank, error) {
	order := "DESC"
	if mode == "min" {
		order = "ASC"
	}
	rows, e := tx.QueryContext(ctx, "SELECT player_id,score,reached_at FROM scores WHERE game_id=? AND board_id=? AND season=? ORDER BY score "+order+",reached_at ASC,player_id ASC LIMIT ? OFFSET ?", g, id, season, last-first+1, first-1)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	a := []Rank{}
	for rows.Next() {
		var v Rank
		if e = rows.Scan(&v.Player, &v.Score, &v.Reached); e != nil {
			return nil, e
		}
		v.Rank = first + len(a)
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
		_, e = s.DB.ExecContext(r.Context(), "INSERT IGNORE INTO boards(game_id,id,season,mode,starts_at,ends_at,frozen,settled,item,reward_amount) VALUES(?,?,?,?,?,?,FALSE,FALSE,?,?)", g, b.ID, b.Season, b.Mode, b.Starts, b.Ends, b.Item, b.Reward)
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
			if e := tx.QueryRowContext(r.Context(), "SELECT mode,starts_at,ends_at,frozen FROM boards WHERE game_id=? AND id=? AND season=? FOR SHARE", g, id, season).Scan(&mode, &start, &end, &frozen); e != nil {
				return bad(404, "board_not_found")
			}
			now := time.Now()
			if frozen || now.Unix() < start || now.Unix() >= end {
				return bad(409, "season_closed")
			}
			res, e := tx.ExecContext(r.Context(), "INSERT IGNORE INTO score_events(game_id,board_id,season,player_id,request_id) VALUES(?,?,?,?,?)", g, id, season, p, b.ID)
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
			_, e = tx.ExecContext(r.Context(), "INSERT INTO scores(game_id,board_id,season,player_id,score,reached_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE score=VALUES(score),reached_at=VALUES(reached_at)", g, id, season, p, next, now.UnixNano())
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
			top, e := ranks(r.Context(), tx, g, r.PathValue("board"), r.PathValue("season"), mode, 1, 50)
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
				near, e = ranks(r.Context(), tx, g, r.PathValue("board"), r.PathValue("season"), mode, max(1, better-1), better+3)
				if e != nil {
					return e
				}
			}
			trust := "client_unverified"
			var requireTicket bool
			e = tx.QueryRowContext(r.Context(), "SELECT require_ticket FROM rule_boards WHERE game_id=? AND board_id=? AND season=? LIMIT 1", g, r.PathValue("board"), r.PathValue("season")).Scan(&requireTicket)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if e == nil {
				if requireTicket {
					trust = "server_seeded_replay"
				} else {
					trust = "deterministic_replay"
				}
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
			return settleBoardTx(r.Context(), tx, g, id, season)
		})
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET /gm/games/{game}/boards", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT b.id,b.season,b.mode,b.starts_at,b.ends_at,b.frozen,b.settled,b.item,b.reward_amount,COALESCE(rb.rule_version,''),COALESCE(rb.require_ticket,FALSE) FROM boards b LEFT JOIN rule_boards rb ON rb.game_id=b.game_id AND rb.board_id=b.id AND rb.season=b.season WHERE b.game_id=? ORDER BY b.ends_at DESC,b.id,b.season", a.Game)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, season, mode, item, ruleVersion string
			var starts, ends, reward int64
			var frozen, settled, requireTicket bool
			if e = rows.Scan(&id, &season, &mode, &starts, &ends, &frozen, &settled, &item, &reward, &ruleVersion, &requireTicket); e != nil {
				return nil, e
			}
			trust := "client_unverified"
			if ruleVersion != "" {
				if requireTicket {
					trust = "server_seeded_replay"
				} else {
					trust = "deterministic_replay"
				}
			}
			out = append(out, map[string]any{
				"id":             id,
				"season":         season,
				"mode":           mode,
				"starts_at":      starts,
				"ends_at":        ends,
				"frozen":         frozen,
				"settled":        settled,
				"item":           item,
				"reward_amount":  reward,
				"rule_version":   ruleVersion,
				"require_ticket": requireTicket,
				"trust":          trust,
			})
		}
		return out, rows.Err()
	})
}

func settleBoardTx(ctx context.Context, tx *sql.Tx, g, id, season string) error {
	var mode, item string
	var settled bool
	var amount, end int64
	if e := tx.QueryRowContext(ctx, "SELECT mode,settled,item,reward_amount,ends_at FROM boards WHERE game_id=? AND id=? AND season=? FOR UPDATE", g, id, season).Scan(&mode, &settled, &item, &amount, &end); e != nil {
		return bad(404, "board_not_found")
	}
	if settled {
		return nil
	}
	if time.Now().Unix() < end {
		return bad(409, "season_still_open")
	}
	a, e := ranks(ctx, tx, g, id, season, mode, 1, 3)
	if e != nil {
		return e
	}
	for _, v := range a {
		if v.Rank > 3 || amount == 0 {
			continue
		}
		if e = grant(ctx, tx, g, v.Player, fmt.Sprintf("season:%s:%s", id, season), item, amount); e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, "UPDATE boards SET frozen=TRUE,settled=TRUE WHERE game_id=? AND id=? AND season=?", g, id, season)
	return e
}
