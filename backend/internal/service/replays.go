package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"math/big"
	"miniverse/backend/internal/rulepb"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Service) ruleClient(address string) (rulepb.RuleCoreClient, error) {
	s.ruleMu.Lock()
	defer s.ruleMu.Unlock()
	if s.ruleConn != nil && s.ruleAddr == address {
		return rulepb.NewRuleCoreClient(s.ruleConn), nil
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	if s.ruleConn != nil {
		_ = s.ruleConn.Close()
	}
	s.ruleConn = conn
	s.ruleAddr = address
	return rulepb.NewRuleCoreClient(conn), nil
}

func (s *Service) replayRoutes(m *http.ServeMux) {
	s.handle(m, "POST /v1/games/{game}/matches/start", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		var b struct {
			Board       string `json:"board"`
			Season      string `json:"season"`
			RuleVersion string `json:"rule_version"`
		}
		if r.ContentLength != 0 {
			if e = decode(r, &b); e != nil {
				return nil, e
			}
		}
		version := strings.TrimSpace(b.RuleVersion)
		if version == "" {
			version = "card-v1"
		}
		if g != "card-game" || version != "card-v1" || len(b.Board) > 64 || len(b.Season) > 64 || (b.Board == "") != (b.Season == "") {
			return nil, bad(400, "invalid_match_config")
		}
		if b.Board != "" {
			var rv string
			var start, end int64
			var frozen bool
			e = s.DB.QueryRowContext(r.Context(), "SELECT rb.rule_version,b.starts_at,b.ends_at,b.frozen FROM rule_boards rb JOIN boards b ON b.game_id=rb.game_id AND b.id=rb.board_id AND b.season=rb.season WHERE rb.game_id=? AND rb.board_id=? AND rb.season=?", g, b.Board, b.Season).Scan(&rv, &start, &end, &frozen)
			if e != nil || rv != version {
				return nil, bad(400, "invalid_verified_board")
			}
			now := time.Now().Unix()
			if frozen || now < start || now >= end {
				return nil, bad(409, "season_closed")
			}
		}
		n, err := rand.Int(rand.Reader, big.NewInt(2147483647))
		if err != nil {
			return nil, err
		}
		seed := n.Int64() + 1
		matchID := "m_" + random()[:24]
		issuedAt := time.Now().Unix()
		expiresAt := issuedAt + 15*60
		if _, e = s.DB.ExecContext(r.Context(), "INSERT INTO match_tickets(id,game_id,player_id,board_id,season,rule_version,seed,issued_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?)", matchID, g, p, b.Board, b.Season, version, seed, issuedAt, expiresAt); e != nil {
			return nil, e
		}
		return map[string]any{
			"match_id":     matchID,
			"game_id":      g,
			"board":        b.Board,
			"season":       b.Season,
			"rule_version": version,
			"seed":         seed,
			"issued_at":    issuedAt,
			"expires_at":   expiresAt,
		}, nil
	})
	s.handle(m, "POST /v1/games/{game}/replays/validate", func(r *http.Request) (any, error) {
		g, p, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		var b struct {
			Version   string   `json:"version"`
			Seed      uint32   `json:"seed"`
			Actions   []uint32 `json:"actions"`
			Claimed   *int64   `json:"claimed_score"`
			RequestID string   `json:"request_id"`
			Board     string   `json:"board"`
			Season    string   `json:"season"`
			MatchID   string   `json:"match_id"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if g != "card-game" || b.Version != "card-v1" || b.Seed == 0 || len(b.Actions) < 1 || len(b.Actions) > 128 || len(b.RequestID) > 128 || len(b.MatchID) > 64 {
			return nil, bad(400, "invalid_replay")
		}
		address := strings.TrimSpace(s.RuleCoreAddr)
		if address == "" {
			address = "rulecore:50051"
		}
		client, e := s.ruleClient(address)
		if e != nil {
			if s.Telemetry != nil {
				s.Telemetry.RecordRuleCore(false, 0, "rulecore_unavailable")
			}
			return nil, bad(503, "rulecore_unavailable")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		rpcStart := time.Now()
		out, e := client.Replay(ctx, &rulepb.ReplayRequest{Version: b.Version, Seed: b.Seed, Actions: b.Actions})
		rpcElapsed := time.Since(rpcStart)
		if e != nil {
			if s.Telemetry != nil {
				s.Telemetry.RecordRuleCore(false, rpcElapsed, "rulecore_unavailable")
			}
			return nil, bad(503, "rulecore_unavailable")
		}
		if s.Telemetry != nil {
			s.Telemetry.RecordRuleCore(true, rpcElapsed, "")
		}
		if !out.Valid {
			return nil, bad(422, "illegal_replay")
		}
		if b.Claimed != nil && *b.Claimed != out.Score {
			return nil, bad(422, "score_mismatch")
		}
		raw, _ := json.Marshal(b)
		payloadHash := hash(string(raw))
		id := b.RequestID
		if id == "" {
			id = payloadHash
		}
		actions, _ := json.Marshal(b.Actions)
		trust := "deterministic_replay"
		if b.MatchID != "" {
			trust = "server_seeded_replay"
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			var existing string
			e := tx.QueryRowContext(r.Context(), "SELECT payload_hash FROM replays WHERE game_id=? AND player_id=? AND request_id=? FOR UPDATE", g, p, id).Scan(&existing)
			if e == nil {
				if existing != payloadHash {
					return bad(409, "idempotency_conflict")
				}
				return nil
			}
			if e != sql.ErrNoRows {
				return e
			}
			nowUnix := time.Now().Unix()
			if b.Board != "" {
				if !out.Finished || !out.Victory {
					return bad(422, "replay_not_won")
				}
				var version string
				var requireTicket, frozen bool
				var start, end int64
				e = tx.QueryRowContext(r.Context(), "SELECT rb.rule_version,rb.require_ticket,b.starts_at,b.ends_at,b.frozen FROM rule_boards rb JOIN boards b ON b.game_id=rb.game_id AND b.id=rb.board_id AND b.season=rb.season WHERE rb.game_id=? AND rb.board_id=? AND rb.season=? FOR SHARE", g, b.Board, b.Season).Scan(&version, &requireTicket, &start, &end, &frozen)
				if e != nil || version != b.Version {
					return bad(400, "invalid_verified_board")
				}
				if frozen || nowUnix < start || nowUnix >= end {
					return bad(409, "season_closed")
				}
				if requireTicket && b.MatchID == "" {
					return bad(400, "match_ticket_required")
				}
			}
			if b.MatchID != "" {
				var tGame, tPlayer, tBoard, tSeason, tVersion string
				var tSeed, ticketExpires int64
				var consumedAt sql.NullInt64
				e = tx.QueryRowContext(r.Context(), "SELECT game_id,player_id,board_id,season,rule_version,seed,expires_at,consumed_at FROM match_tickets WHERE id=? FOR UPDATE", b.MatchID).Scan(&tGame, &tPlayer, &tBoard, &tSeason, &tVersion, &tSeed, &ticketExpires, &consumedAt)
				if errors.Is(e, sql.ErrNoRows) {
					return bad(404, "match_ticket_not_found")
				}
				if e != nil {
					return e
				}
				if tGame != g || tPlayer != p || tVersion != b.Version || (tBoard != "" && (tBoard != b.Board || tSeason != b.Season)) {
					return bad(403, "invalid_match_ticket")
				}
				if consumedAt.Valid {
					return bad(409, "match_ticket_consumed")
				}
				if nowUnix > ticketExpires {
					return bad(409, "match_ticket_expired")
				}
				if tSeed != int64(b.Seed) {
					return bad(422, "seed_mismatch")
				}
				if _, e = tx.ExecContext(r.Context(), "UPDATE match_tickets SET consumed_at=?,request_id=? WHERE id=?", nowUnix, id, b.MatchID); e != nil {
					return e
				}
			}
			if b.Board != "" {
				var old int64
				e = tx.QueryRowContext(r.Context(), "SELECT score FROM scores WHERE game_id=? AND board_id=? AND season=? AND player_id=?", g, b.Board, b.Season, p).Scan(&old)
				if e != nil && e != sql.ErrNoRows {
					return e
				}
				if e == sql.ErrNoRows || out.Score > old {
					if _, e = tx.ExecContext(r.Context(), "INSERT INTO scores(game_id,board_id,season,player_id,score,reached_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE score=VALUES(score),reached_at=VALUES(reached_at)", g, b.Board, b.Season, p, out.Score, time.Now().UnixNano()); e != nil {
						return e
					}
				}
			}
			_, e = tx.ExecContext(r.Context(), "INSERT INTO replays(game_id,player_id,request_id,payload_hash,rule_version,seed,actions,score,state_hash) VALUES(?,?,?,?,?,?,?,?,?)", g, p, id, payloadHash, b.Version, b.Seed, actions, out.Score, strconv.FormatUint(out.StateHash, 10))
			return e
		})
		return map[string]any{"valid": out.Valid, "score": out.Score, "state_hash": strconv.FormatUint(out.StateHash, 10), "finished": out.Finished, "victory": out.Victory, "trust": trust}, e
	})
}
