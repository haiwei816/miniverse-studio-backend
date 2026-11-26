package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"miniverse/backend/internal/mq"
	"net/http"
	"strings"
	"time"
)

type RewardRequest struct {
	Players []string `json:"players"`
	Item    string   `json:"item"`
	Amount  int64    `json:"amount"`
}

type RewardBatchRequest struct {
	Players      []string `json:"players"`
	BroadcastAll bool     `json:"broadcast_all"`
	Item         string   `json:"item"`
	Amount       int64    `json:"amount"`
}

type ActivityStopRequest struct {
	ActivityID string `json:"activity_id"`
}

type BoardUpsertRequest struct {
	ID            string `json:"id"`
	Season        string `json:"season"`
	Mode          string `json:"mode"`
	Starts        int64  `json:"starts_at"`
	Ends          int64  `json:"ends_at"`
	Frozen        bool   `json:"frozen"`
	Item          string `json:"item"`
	Reward        int64  `json:"reward_amount"`
	RuleVersion   string `json:"rule_version,omitempty"`
	RequireTicket bool   `json:"require_ticket,omitempty"`
}

type SeasonSettleRequest struct {
	Board  string `json:"board"`
	Season string `json:"season"`
}

type PlayerBanRequest struct {
	PlayerID  string `json:"player_id"`
	Reason    string `json:"reason"`
	ExpiresAt *int64 `json:"expires_at,omitempty"`
}

func (s *Service) validatePayload(ctx context.Context, g, kind string, raw json.RawMessage) error {
	switch kind {
	case "reward":
		var b RewardRequest
		if e := json.Unmarshal(raw, &b); e != nil || len(b.Players) < 1 || len(b.Players) > 100 || b.Amount < 1 || b.Amount > 1000000 {
			return bad(400, "invalid_reward_request")
		}
		var n int
		if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, b.Item).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return bad(400, "unknown_item")
		}
		seen := make(map[string]struct{}, len(b.Players))
		unique := make([]string, 0, len(b.Players))
		for _, p := range b.Players {
			if p == "" {
				return bad(400, "unknown_player")
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			unique = append(unique, p)
		}
		placeholders := make([]string, len(unique))
		args := make([]any, 0, 1+len(unique))
		args = append(args, g)
		for i, p := range unique {
			placeholders[i] = "?"
			args = append(args, p)
		}
		if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM profiles WHERE game_id=? AND player_id IN ("+strings.Join(placeholders, ",")+")", args...).Scan(&n); e != nil {
			return e
		}
		if n != len(unique) {
			return bad(400, "unknown_player")
		}
	case "reward_batch":
		var b RewardBatchRequest
		if e := json.Unmarshal(raw, &b); e != nil || b.Amount < 1 || b.Amount > 1000000 {
			return bad(400, "invalid_reward_request")
		}
		if !b.BroadcastAll && (len(b.Players) < 1 || len(b.Players) > 50000) {
			return bad(400, "invalid_reward_request")
		}
		var n int
		if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, b.Item).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return bad(400, "unknown_item")
		}
	case "activity":
		a, e := readActivity(raw)
		if e != nil {
			return bad(400, "invalid_activity")
		}
		if e = validateActivity(a); e != nil {
			return e
		}
		var n int
		if e = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, a.Item).Scan(&n); e != nil {
			return e
		}
		if a.Amount > 0 && n != 1 {
			return bad(400, "unknown_item")
		}
	case "activity_stop":
		var b ActivityStopRequest
		if e := json.Unmarshal(raw, &b); e != nil || strings.TrimSpace(b.ActivityID) == "" || len(b.ActivityID) > 64 {
			return bad(400, "invalid_activity_stop")
		}
		var n int
		if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM activities WHERE game_id=? AND id=?", g, b.ActivityID).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return bad(404, "activity_not_found")
		}
	case "board_upsert":
		var b BoardUpsertRequest
		if e := json.Unmarshal(raw, &b); e != nil || strings.TrimSpace(b.ID) == "" || len(b.ID) > 64 || strings.TrimSpace(b.Season) == "" || len(b.Season) > 64 || b.Starts >= b.Ends || (b.Mode != "max" && b.Mode != "min" && b.Mode != "sum") || b.Reward < 0 || b.Reward > 1000000 || len(b.RuleVersion) > 32 {
			return bad(400, "invalid_board")
		}
		if b.Reward > 0 {
			var n int
			if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE game_id=? AND id=?", g, b.Item).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				return bad(400, "unknown_item")
			}
		}
		if b.RuleVersion != "" && b.RuleVersion != "card-v1" {
			return bad(400, "invalid_board")
		}
	case "season_settle":
		var b SeasonSettleRequest
		if e := json.Unmarshal(raw, &b); e != nil || strings.TrimSpace(b.Board) == "" || len(b.Board) > 64 || strings.TrimSpace(b.Season) == "" || len(b.Season) > 64 {
			return bad(400, "invalid_board")
		}
		var settled bool
		var end int64
		if e := s.DB.QueryRowContext(ctx, "SELECT settled,ends_at FROM boards WHERE game_id=? AND id=? AND season=?", g, b.Board, b.Season).Scan(&settled, &end); e != nil {
			return bad(404, "board_not_found")
		}
		if settled {
			return bad(409, "season_already_settled")
		}
		if time.Now().Unix() < end {
			return bad(409, "season_still_open")
		}
	case "player_ban":
		var b PlayerBanRequest
		if e := json.Unmarshal(raw, &b); e != nil || strings.TrimSpace(b.PlayerID) == "" || len(b.PlayerID) > 64 {
			return bad(400, "invalid_player_ban")
		}
		trimmed := strings.TrimSpace(b.Reason)
		if len(trimmed) < 3 || len(trimmed) > 256 {
			return bad(400, "reason_required")
		}
		if b.ExpiresAt != nil && *b.ExpiresAt <= time.Now().Unix() {
			return bad(400, "invalid_ban_expiry")
		}
		var n int
		if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM profiles WHERE game_id=? AND player_id=?", g, b.PlayerID).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return bad(404, "player_not_found")
		}
	default:
		return bad(400, "invalid_request_kind")
	}
	return nil
}

func (s *Service) createGMRequest(ctx context.Context, a GM, id, kind, reason string, payload json.RawMessage) (string, error) {
	if id == "" || len(id) > 64 {
		return "", bad(400, "invalid_request_id")
	}
	if e := s.validatePayload(ctx, a.Game, kind, payload); e != nil {
		return "", e
	}
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		var creator, existingKind string
		var old []byte
		e := tx.QueryRowContext(ctx, "SELECT creator,kind,payload FROM gm_requests WHERE game_id=? AND id=? FOR UPDATE", a.Game, id).Scan(&creator, &existingKind, &old)
		if e == nil {
			var x, y any
			json.Unmarshal(old, &x)
			json.Unmarshal(payload, &y)
			cx, _ := json.Marshal(x)
			cy, _ := json.Marshal(y)
			if creator != a.ID || existingKind != kind || string(cx) != string(cy) {
				return bad(409, "idempotency_conflict")
			}
			return nil
		}
		if e != sql.ErrNoRows {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO gm_requests(game_id,id,kind,creator,state,reason,payload,created_at) VALUES(?,?,?,?,'pending',?,?,?)", a.Game, id, kind, a.ID, reason, payload, time.Now().Unix()); e != nil {
			return e
		}
		return auditCtx(ctx, tx, a, "request_created", reason, map[string]any{"request_id": id, "kind": kind})
	})
	return id, e
}

func (s *Service) operationsRoutes(m *http.ServeMux) {
	base := "/gm/games/{game}"
	s.handle(m, "POST "+base+"/requests", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var b struct {
			ID      string          `json:"id"`
			Kind    string          `json:"kind"`
			Reason  string          `json:"reason"`
			Payload json.RawMessage `json:"payload"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		id, e := s.createGMRequest(r.Context(), a, b.ID, b.Kind, b.Reason, b.Payload)
		return map[string]string{"id": id}, e
	})
	s.handle(m, "GET "+base+"/requests", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,kind,creator,COALESCE(reviewer,''),state,reason,payload,created_at FROM gm_requests WHERE game_id=? ORDER BY created_at DESC,id LIMIT 200", a.Game)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, kind, creator, reviewer, state, reason string
			var payload json.RawMessage
			var createdAt int64
			if e = rows.Scan(&id, &kind, &creator, &reviewer, &state, &reason, &payload, &createdAt); e != nil {
				return nil, e
			}
			out = append(out, map[string]any{"id": id, "kind": kind, "creator": creator, "reviewer": reviewer, "state": state, "reason": reason, "payload": payload, "created_at": createdAt})
		}
		return out, rows.Err()
	})
	for _, action := range []string{"approve", "cancel"} {
		action := action
		s.handle(m, "POST "+base+"/requests/{request}/"+action, func(r *http.Request) (any, error) {
			perm := "approve"
			if action == "cancel" {
				perm = "write"
			}
			a, e := s.gm(r, perm)
			if e != nil {
				return nil, e
			}
			var b struct {
				Reason string `json:"reason"`
			}
			if e = decode(r, &b); e != nil {
				return nil, e
			}
			var approvedKind string
			var approvedRaw []byte
			e = s.transaction(r.Context(), func(tx *sql.Tx) error {
				var creator, state, kind string
				var raw []byte
				id := r.PathValue("request")
				if e := tx.QueryRowContext(r.Context(), "SELECT creator,state,kind,payload FROM gm_requests WHERE game_id=? AND id=? FOR UPDATE", a.Game, id).Scan(&creator, &state, &kind, &raw); e != nil {
					return bad(404, "request_not_found")
				}
				approvedKind = kind
				approvedRaw = raw
				if action == "approve" && creator == a.ID {
					return bad(403, "self_approval_forbidden")
				}
				if state == "executed" && action == "approve" {
					return nil
				}
				if state != "pending" {
					return bad(409, "request_not_pending")
				}
				next := "cancelled"
				if action == "approve" {
					next = "executed"
					switch kind {
					case "reward":
						var v RewardRequest
						if e := json.Unmarshal(raw, &v); e != nil {
							return e
						}
						for _, p := range v.Players {
							if e := grant(r.Context(), tx, a.Game, p, "gm:"+id, v.Item, v.Amount); e != nil {
								return e
							}
						}
					case "activity":
						v, e := readActivity(raw)
						if e != nil {
							return e
						}
						var current int
						e = tx.QueryRowContext(r.Context(), "SELECT version FROM activities WHERE game_id=? AND id=? FOR UPDATE", a.Game, v.ID).Scan(&current)
						if e != nil && e != sql.ErrNoRows {
							return e
						}
						if v.Version <= current {
							return bad(409, "activity_version_conflict")
						}
						if _, e = tx.ExecContext(r.Context(), "INSERT INTO activity_versions(game_id,id,version,config) VALUES(?,?,?,?)", a.Game, v.ID, v.Version, raw); e != nil {
							return e
						}
						if _, e = tx.ExecContext(r.Context(), "INSERT INTO activities(game_id,id,version,config,active) VALUES(?,?,?,?,TRUE) ON DUPLICATE KEY UPDATE version=VALUES(version),config=VALUES(config),active=TRUE", a.Game, v.ID, v.Version, raw); e != nil {
							return e
						}
					case "activity_stop":
						var v ActivityStopRequest
						if e := json.Unmarshal(raw, &v); e != nil {
							return e
						}
						var exists int
						if e := tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM activities WHERE game_id=? AND id=? FOR UPDATE", a.Game, v.ActivityID).Scan(&exists); e != nil {
							return e
						}
						if exists != 1 {
							return bad(404, "activity_not_found")
						}
						if _, e := tx.ExecContext(r.Context(), "UPDATE activities SET active=FALSE WHERE game_id=? AND id=?", a.Game, v.ActivityID); e != nil {
							return e
						}
					case "board_upsert":
						var v BoardUpsertRequest
						if e := json.Unmarshal(raw, &v); e != nil {
							return e
						}
						if _, e := tx.ExecContext(r.Context(), "INSERT INTO boards(game_id,id,season,mode,starts_at,ends_at,frozen,settled,item,reward_amount) VALUES(?,?,?,?,?,?,?,FALSE,?,?) ON DUPLICATE KEY UPDATE mode=VALUES(mode),starts_at=VALUES(starts_at),ends_at=VALUES(ends_at),frozen=VALUES(frozen),item=VALUES(item),reward_amount=VALUES(reward_amount)", a.Game, v.ID, v.Season, v.Mode, v.Starts, v.Ends, v.Frozen, v.Item, v.Reward); e != nil {
							return e
						}
						if v.RuleVersion != "" {
							if _, e := tx.ExecContext(r.Context(), "INSERT INTO rule_boards(game_id,board_id,season,rule_version,require_ticket) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE rule_version=VALUES(rule_version),require_ticket=VALUES(require_ticket)", a.Game, v.ID, v.Season, v.RuleVersion, v.RequireTicket); e != nil {
								return e
							}
						}
					case "season_settle":
						var v SeasonSettleRequest
						if e := json.Unmarshal(raw, &v); e != nil {
							return e
						}
						if e := settleBoardTx(r.Context(), tx, a.Game, v.Board, v.Season); e != nil {
							return e
						}
					case "player_ban":
						var v PlayerBanRequest
						if e := json.Unmarshal(raw, &v); e != nil {
							return e
						}
						if e := s.applyPlayerBanTx(r.Context(), tx, a, v.PlayerID, v.Reason, v.ExpiresAt); e != nil {
							return e
						}
					case "reward_batch":
						var v RewardBatchRequest
						if e := json.Unmarshal(raw, &v); e != nil {
							return e
						}
						jobID := "job-" + random()[:16]
						now := time.Now().Unix()
						if _, e := tx.ExecContext(r.Context(),
							"INSERT INTO batch_grant_jobs(id,game_id,request_id,item,amount,reason,total_players,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'queued',?,?)",
							jobID, a.Game, id, v.Item, v.Amount, b.Reason, len(v.Players), now, now,
						); e != nil {
							return e
						}
						if e := mq.EnqueueTx(r.Context(), tx, "outbox-"+jobID, mq.TopicRewardBatch, a.Game, rewardBatchPayload{
							JobID:        jobID,
							GameID:       a.Game,
							RequestID:    id,
							Item:         v.Item,
							Amount:       v.Amount,
							Reason:       b.Reason,
							Players:      v.Players,
							BroadcastAll: v.BroadcastAll,
						}); e != nil {
							return e
						}
					}
				}
				if _, e := tx.ExecContext(r.Context(), "UPDATE gm_requests SET state=?,reviewer=? WHERE game_id=? AND id=?", next, a.ID, a.Game, id); e != nil {
					return e
				}
				return audit(r, tx, a, "request_"+action, b.Reason, map[string]string{"request_id": id, "kind": kind})
			})
			if e == nil && action == "approve" {
				switch approvedKind {
				case "activity", "activity_stop":
					s.InvalidateActivitiesCache(a.Game)
				case "board_upsert":
					var v BoardUpsertRequest
					if json.Unmarshal(approvedRaw, &v) == nil {
						s.invalidateBoardRedis(r.Context(), a.Game, v.ID, v.Season)
					}
				case "season_settle":
					var v SeasonSettleRequest
					if json.Unmarshal(approvedRaw, &v) == nil {
						s.invalidateBoardRedis(r.Context(), a.Game, v.Board, v.Season)
					}
				case "player_ban":
					var v PlayerBanRequest
					if json.Unmarshal(approvedRaw, &v) == nil {
						s.InvalidateBanCache(v.PlayerID)
					}
				case "reward_batch":
					if s.OutboxRelay != nil {
						s.OutboxRelay.Wake()
					}
				}
			}
			return map[string]bool{"ok": true}, e
		})
	}
	s.handle(m, "POST "+base+"/activities/{activity}/stop", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var b struct {
			Reason string `json:"reason"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			res, e := tx.ExecContext(r.Context(), "UPDATE activities SET active=FALSE WHERE game_id=? AND id=?", a.Game, r.PathValue("activity"))
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return bad(404, "activity_not_found")
			}
			return audit(r, tx, a, "activity_stopped", b.Reason, map[string]string{"activity_id": r.PathValue("activity")})
		})
		if e == nil {
			s.InvalidateActivitiesCache(a.Game)
		}
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET "+base+"/activities/{activity}/versions", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT config FROM activity_versions WHERE game_id=? AND id=? ORDER BY version DESC", a.Game, r.PathValue("activity"))
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []json.RawMessage{}
		for rows.Next() {
			var v json.RawMessage
			if e = rows.Scan(&v); e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		return out, rows.Err()
	})
	s.handle(m, "PUT "+base+"/announcements/{announcement}", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var b struct {
			Body    string `json:"body"`
			Expires int64  `json:"expires_at"`
			Reason  string `json:"reason"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.Body) > 2000 || b.Expires <= time.Now().Unix() {
			return nil, bad(400, "invalid_announcement")
		}
		e = s.transaction(r.Context(), func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(r.Context(), "INSERT INTO announcements(game_id,id,body,expires_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE body=VALUES(body),expires_at=VALUES(expires_at)", a.Game, r.PathValue("announcement"), b.Body, b.Expires); e != nil {
				return e
			}
			return audit(r, tx, a, "announcement_updated", b.Reason, map[string]string{"id": r.PathValue("announcement")})
		})
		return map[string]bool{"ok": true}, e
	})
	s.handle(m, "GET /v1/games/{game}/announcements", func(r *http.Request) (any, error) {
		g, _, e := s.scope(r)
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,body FROM announcements WHERE game_id=? AND expires_at>?", g, time.Now().Unix())
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, body string
			if e = rows.Scan(&id, &body); e != nil {
				return nil, e
			}
			out = append(out, map[string]string{"id": id, "body": body})
		}
		return out, rows.Err()
	})
	s.handle(m, "GET "+base+"/batch-jobs", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		jobs, e := s.queryRowsGeneric(r.Context(),
			"SELECT id,request_id,item,amount,reason,total_players,succeeded_players,failed_players,state,last_error,created_at,updated_at FROM batch_grant_jobs WHERE game_id=? ORDER BY created_at DESC,id DESC LIMIT 50",
			a.Game,
		)
		if e != nil {
			return nil, e
		}
		return map[string]any{"jobs": jobs}, nil
	})
}
