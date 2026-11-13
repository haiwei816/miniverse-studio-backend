package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"miniverse/backend/internal/rulepb"
	"net/http"
	"os"
	"strconv"
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
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if g != "card-game" || b.Version != "card-v1" || b.Seed == 0 || len(b.Actions) < 1 || len(b.Actions) > 128 || len(b.RequestID) > 128 {
			return nil, bad(400, "invalid_replay")
		}
		address := os.Getenv("RULECORE_ADDR")
		if address == "" {
			address = "rulecore:50051"
		}
		client, e := s.ruleClient(address)
		if e != nil {
			return nil, bad(503, "rulecore_unavailable")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		out, e := client.Replay(ctx, &rulepb.ReplayRequest{Version: b.Version, Seed: b.Seed, Actions: b.Actions})
		if e != nil {
			return nil, bad(503, "rulecore_unavailable")
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
			if b.Board != "" {
				if !out.Finished || !out.Victory {
					return bad(422, "replay_not_won")
				}
				var version string
				var start, end int64
				var frozen bool
				e = tx.QueryRowContext(r.Context(), "SELECT rb.rule_version,b.starts_at,b.ends_at,b.frozen FROM rule_boards rb JOIN boards b ON b.game_id=rb.game_id AND b.id=rb.board_id AND b.season=rb.season WHERE rb.game_id=? AND rb.board_id=? AND rb.season=? FOR UPDATE", g, b.Board, b.Season).Scan(&version, &start, &end, &frozen)
				if e != nil || version != b.Version {
					return bad(400, "invalid_verified_board")
				}
				if frozen || time.Now().Unix() < start || time.Now().Unix() >= end {
					return bad(409, "season_closed")
				}
				var old int64
				e = tx.QueryRowContext(r.Context(), "SELECT score FROM scores WHERE game_id=? AND board_id=? AND season=? AND player_id=?", g, b.Board, b.Season, p).Scan(&old)
				if e != nil && e != sql.ErrNoRows {
					return e
				}
				if e == sql.ErrNoRows || out.Score > old {
					if _, e = tx.ExecContext(r.Context(), "INSERT INTO scores VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE score=VALUES(score),reached_at=VALUES(reached_at)", g, b.Board, b.Season, p, out.Score, time.Now().UnixNano()); e != nil {
						return e
					}
				}
			}
			_, e = tx.ExecContext(r.Context(), "INSERT INTO replays VALUES(?,?,?,?,?,?,?,?,?)", g, p, id, payloadHash, b.Version, b.Seed, actions, out.Score, strconv.FormatUint(out.StateHash, 10))
			return e
		})
		return map[string]any{"valid": out.Valid, "score": out.Score, "state_hash": strconv.FormatUint(out.StateHash, 10), "finished": out.Finished, "victory": out.Victory, "trust": "deterministic_replay"}, e
	})
}
