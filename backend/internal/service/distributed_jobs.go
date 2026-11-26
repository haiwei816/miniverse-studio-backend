package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"miniverse/backend/internal/lock"
	"miniverse/backend/internal/mq"
	"miniverse/backend/internal/redisx"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type boardData struct {
	Top    []Rank `json:"top"`
	Nearby []Rank `json:"nearby"`
	Trust  string `json:"trust"`
}

type rewardBatchPayload struct {
	JobID        string   `json:"job_id"`
	GameID       string   `json:"game_id"`
	RequestID    string   `json:"request_id"`
	Item         string   `json:"item"`
	Amount       int64    `json:"amount"`
	Reason       string   `json:"reason"`
	Players      []string `json:"players"`
	BroadcastAll bool     `json:"broadcast_all"`
}

var boardZSetUpsertScript = redis.NewScript(`
local zkey = KEYS[1]
local hkey = KEYS[2]
local ekey = KEYS[3]
local player = ARGV[1]
local member = ARGV[2]
local ttlMs = tonumber(ARGV[3])
local old = redis.call("HGET", hkey, player)
if old then
	redis.call("ZREM", zkey, old)
end
redis.call("ZADD", zkey, 0, member)
redis.call("HSET", hkey, player, member)
redis.call("DEL", ekey)
redis.call("PEXPIRE", zkey, ttlMs)
redis.call("PEXPIRE", hkey, ttlMs)
return 1
`)

const maxLexScore = int64(9999999999999999)

func encodeBoardMember(mode string, score, reached int64, playerID string) string {
	s := max(int64(0), min(score, maxLexScore))
	if mode != "min" {
		s = maxLexScore - s
	}
	r := max(int64(0), reached)
	return fmt.Sprintf("%016d:%020d:%s", s, r, playerID)
}

func decodeBoardMember(mode, member string) (Rank, bool) {
	p1 := strings.IndexByte(member, ':')
	if p1 <= 0 {
		return Rank{}, false
	}
	rest := member[p1+1:]
	p2 := strings.IndexByte(rest, ':')
	if p2 <= 0 {
		return Rank{}, false
	}
	rawScore, err1 := strconv.ParseInt(member[:p1], 10, 64)
	reached, err2 := strconv.ParseInt(rest[:p2], 10, 64)
	if err1 != nil || err2 != nil {
		return Rank{}, false
	}
	score := rawScore
	if mode != "min" {
		score = maxLexScore - rawScore
	}
	return Rank{
		Player:  rest[p2+1:],
		Score:   score,
		Reached: reached,
	}, true
}

func boardRedisKeys(game, board, season string) (zkey, hkey, emptyKey string) {
	base := game + ":" + board + ":" + season
	return "mv:board:z:" + base, "mv:board:h:" + base, "mv:board:empty:" + base
}

func jitterBoardTTL() time.Duration {
	return 5*time.Minute + time.Duration(rand.Int64N(int64(60*time.Second)))
}

func (s *Service) invalidateBoardRedis(ctx context.Context, game, board, season string) {
	if s.Redis == nil || s.Redis.Raw() == nil {
		return
	}
	zkey, hkey, ekey := boardRedisKeys(game, board, season)
	rCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_ = s.Redis.Raw().Del(rCtx, zkey, hkey, ekey).Err()
	s.Redis.PublishInvalidation(ctx, redisx.InvalidationEvent{
		Kind:     "board",
		GameID:   game,
		BoardKey: board + ":" + season,
	})
}

func (s *Service) updateBoardScoreRedis(ctx context.Context, game, board, season, mode, player string, score, reached int64) {
	if s.Redis == nil || s.Redis.Raw() == nil {
		return
	}
	zkey, hkey, ekey := boardRedisKeys(game, board, season)
	rCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	// Only update hot ZSET in-place if already hydrated; otherwise clear empty sentinel so next read hydrates cleanly.
	exists, err := s.Redis.Raw().Exists(rCtx, zkey).Result()
	if err != nil {
		return
	}
	if exists == 0 {
		_ = s.Redis.Raw().Del(rCtx, ekey).Err()
		return
	}
	member := encodeBoardMember(mode, score, reached, player)
	_ = boardZSetUpsertScript.Run(rCtx, s.Redis.Raw(), []string{zkey, hkey, ekey}, player, member, jitterBoardTTL().Milliseconds()).Err()
}

func (s *Service) readBoardFromRedis(ctx context.Context, game, board, season, mode, player string) (top, near []Rank, ok bool) {
	if s.Redis == nil || s.Redis.Raw() == nil {
		return nil, nil, false
	}
	rdb := s.Redis.Raw()
	zkey, hkey, ekey := boardRedisKeys(game, board, season)

	rCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	// Check negative sentinel (cache penetration defense).
	if empty, err := rdb.Exists(rCtx, ekey).Result(); err == nil && empty > 0 {
		if s.Telemetry != nil {
			s.Telemetry.RecordCacheHit()
		}
		return []Rank{}, []Rank{}, true
	}

	// Check ZSET presence; on miss, hydrate via Singleflight (cache breakdown defense).
	if exists, err := rdb.Exists(rCtx, zkey).Result(); err != nil {
		return nil, nil, false
	} else if exists == 0 {
		if s.Telemetry != nil {
			s.Telemetry.RecordCacheMiss()
		}
		_, err, _ := s.boardSF.Do(game+":"+board+":"+season, func() (boardData, error) {
			return boardData{}, s.hydrateBoardZSet(ctx, game, board, season, mode)
		})
		if err != nil {
			return nil, nil, false
		}
		if empty, err := rdb.Exists(rCtx, ekey).Result(); err == nil && empty > 0 {
			return []Rank{}, []Rank{}, true
		}
	} else if s.Telemetry != nil {
		s.Telemetry.RecordCacheHit()
	}

	topRaw, err := rdb.ZRange(rCtx, zkey, 0, 49).Result()
	if err != nil {
		return nil, nil, false
	}
	top = make([]Rank, 0, len(topRaw))
	for i, m := range topRaw {
		r, valid := decodeBoardMember(mode, m)
		if !valid {
			return nil, nil, false
		}
		r.Rank = i + 1
		top = append(top, r)
	}

	near = []Rank{}
	member, err := rdb.HGet(rCtx, hkey, player).Result()
	if err == nil && member != "" {
		rank0, rErr := rdb.ZRank(rCtx, zkey, member).Result()
		if rErr == nil {
			start0 := max(int64(0), rank0-2)
			stop0 := rank0 + 2
			nearRaw, nErr := rdb.ZRange(rCtx, zkey, start0, stop0).Result()
			if nErr == nil {
				for idx, m := range nearRaw {
					r, valid := decodeBoardMember(mode, m)
					if valid {
						r.Rank = int(start0) + idx + 1
						near = append(near, r)
					}
				}
			}
		}
	}
	return top, near, true
}

func (s *Service) hydrateBoardZSet(ctx context.Context, game, board, season, mode string) error {
	if s.Redis == nil || s.Redis.Raw() == nil || s.DB == nil {
		return nil
	}
	rdb := s.Redis.Raw()
	zkey, hkey, ekey := boardRedisKeys(game, board, season)
	ttl := jitterBoardTTL()

	rows, err := s.DB.QueryContext(ctx, "SELECT player_id,score,reached_at FROM scores WHERE game_id=? AND board_id=? AND season=?", game, board, season)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rowItem struct {
		player  string
		score   int64
		reached int64
	}
	var items []rowItem
	for rows.Next() {
		var it rowItem
		if err := rows.Scan(&it.player, &it.score, &it.reached); err != nil {
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	rCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if len(items) == 0 {
		return rdb.Set(rCtx, ekey, "1", 15*time.Second).Err()
	}

	const chunk = 500
	for i := 0; i < len(items); i += chunk {
		end := min(len(items), i+chunk)
		pipe := rdb.Pipeline()
		zs := make([]redis.Z, 0, end-i)
		hvals := make([]any, 0, (end-i)*2)
		for _, it := range items[i:end] {
			member := encodeBoardMember(mode, it.score, it.reached, it.player)
			zs = append(zs, redis.Z{Score: 0, Member: member})
			hvals = append(hvals, it.player, member)
		}
		pipe.ZAdd(rCtx, zkey, zs...)
		pipe.HSet(rCtx, hkey, hvals...)
		if _, err := pipe.Exec(rCtx); err != nil {
			return err
		}
	}
	pipe := rdb.Pipeline()
	pipe.Expire(rCtx, zkey, ttl)
	pipe.Expire(rCtx, hkey, ttl)
	pipe.Del(rCtx, ekey)
	_, err = pipe.Exec(rCtx)
	return err
}

func (s *Service) handleRewardBatchMessage(ctx context.Context, msg mq.Message) error {
	var p rewardBatchPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		return err
	}
	if s.DB == nil {
		return nil
	}
	players := p.Players
	if p.BroadcastAll {
		rows, err := s.DB.QueryContext(ctx, "SELECT player_id FROM profiles WHERE game_id=? ORDER BY player_id", p.GameID)
		if err != nil {
			return err
		}
		players = players[:0]
		for rows.Next() {
			var pid string
			if err := rows.Scan(&pid); err != nil {
				rows.Close()
				return err
			}
			players = append(players, pid)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	now := time.Now().Unix()
	_, _ = s.DB.ExecContext(ctx, "UPDATE batch_grant_jobs SET total_players=?,state='running',updated_at=? WHERE id=?", len(players), now, p.JobID)

	const batchSize = 200
	var succeeded, failed int
	var lastErr string
	for i := 0; i < len(players); i += batchSize {
		end := min(len(players), i+batchSize)
		err := s.transaction(ctx, func(tx *sql.Tx) error {
			for _, pid := range players[i:end] {
				if err := grant(ctx, tx, p.GameID, pid, "batch:"+p.RequestID, p.Item, p.Amount); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			failed += end - i
			lastErr = err.Error()
		} else {
			succeeded += end - i
		}
		_, _ = s.DB.ExecContext(ctx, "UPDATE batch_grant_jobs SET succeeded_players=?,failed_players=?,last_error=?,updated_at=? WHERE id=?", succeeded, failed, lastErr, time.Now().Unix(), p.JobID)
	}

	finalState := "completed"
	if failed > 0 && succeeded == 0 {
		finalState = "failed"
	} else if failed > 0 {
		finalState = "partial_failed"
	}
	_, _ = s.DB.ExecContext(ctx, "UPDATE batch_grant_jobs SET state=?,succeeded_players=?,failed_players=?,last_error=?,updated_at=? WHERE id=?", finalState, succeeded, failed, lastErr, time.Now().Unix(), p.JobID)
	return nil
}

// startSeasonAutoSettler periodically checks for expired, unsettled seasons and settles them
// under a Redis Lua distributed lock + Watchdog lease (with MySQL FOR UPDATE as final guard).
func (s *Service) startSeasonAutoSettler() func() {
	if s.DB == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runAutoSettleCycle(ctx)
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (s *Service) runAutoSettleCycle(ctx context.Context) {
	if s.DB == nil {
		return
	}
	now := time.Now().Unix()
	qCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	// Wait 60s past ends_at before auto-settling so manual GM settlement flows never race right at close time.
	rows, err := s.DB.QueryContext(qCtx, "SELECT game_id,id,season FROM boards WHERE settled=FALSE AND ends_at<=? LIMIT 10", now-60)
	cancel()
	if err != nil {
		return
	}
	type dueBoard struct{ game, id, season string }
	var due []dueBoard
	for rows.Next() {
		var d dueBoard
		if err := rows.Scan(&d.game, &d.id, &d.season); err == nil {
			due = append(due, d)
		}
	}
	rows.Close()

	for _, b := range due {
		if ctx.Err() != nil {
			return
		}
		lockKey := fmt.Sprintf("mv:lock:settle:%s:%s:%s", b.game, b.id, b.season)
		lease, lErr := lock.Acquire(ctx, s.LockStore, lockKey, 10*time.Second)
		if lErr != nil {
			continue
		}
		sCtx, sCancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.transaction(sCtx, func(tx *sql.Tx) error {
			return settleBoardTx(sCtx, tx, b.game, b.id, b.season)
		})
		sCancel()
		if err == nil {
			s.invalidateBoardRedis(ctx, b.game, b.id, b.season)
		} else {
			slog.Warn("auto_settle_board_failed", "game_id", b.game, "board_id", b.id, "season", b.season, "error", err)
		}
		_ = lease.Release(context.Background())
	}
}
