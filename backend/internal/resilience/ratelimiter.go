package resilience

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisWindowLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
if current > tonumber(ARGV[1]) then
	return 0
end
return 1
`)

type rateBucket struct {
	start time.Time
	n     int
}

// TwoTierLimiter combines a bounded in-memory per-node fast path with a Redis
// atomic fixed-window limiter, gracefully falling back to local limiting on Redis errors.
type TwoTierLimiter struct {
	mu             sync.Mutex
	buckets        map[string]rateBucket
	nextSweep      time.Time
	maxEntries     int
	limitPerMinute int
	rdb            *redis.Client
	prefix         string
}

func NewTwoTierLimiter(rdb *redis.Client, prefix string, limitPerMinute, maxLocalEntries int) *TwoTierLimiter {
	if limitPerMinute <= 0 {
		limitPerMinute = 600
	}
	if maxLocalEntries <= 0 {
		maxLocalEntries = 10000
	}
	return &TwoTierLimiter{
		buckets:        make(map[string]rateBucket),
		maxEntries:     maxLocalEntries,
		limitPerMinute: limitPerMinute,
		rdb:            rdb,
		prefix:         prefix,
	}
}

func (l *TwoTierLimiter) SetRedis(rdb *redis.Client) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rdb = rdb
}

func (l *TwoTierLimiter) allowLocal(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.nextSweep) {
		for k, b := range l.buckets {
			if now.Sub(b.start) >= time.Minute {
				delete(l.buckets, k)
			}
		}
		l.nextSweep = now.Add(time.Minute)
	}
	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= l.maxEntries {
			return false
		}
		b = rateBucket{start: now}
	} else if now.Sub(b.start) >= time.Minute {
		b = rateBucket{start: now}
	}
	if b.n >= l.limitPerMinute {
		return false
	}
	b.n++
	l.buckets[key] = b
	return true
}

func (l *TwoTierLimiter) Allow(ctx context.Context, key string, now time.Time) bool {
	if !l.allowLocal(key, now) {
		return false
	}
	l.mu.Lock()
	rdb := l.rdb
	l.mu.Unlock()
	if rdb == nil {
		return true
	}
	rCtx, cancel := context.WithTimeout(ctx, 120*time.Millisecond)
	defer cancel()
	minuteSlot := now.Unix() / 60
	redisKey := l.prefix + ":" + key + ":" + time.Unix(minuteSlot*60, 0).UTC().Format("200601021504")
	res, err := redisWindowLimitScript.Run(rCtx, rdb, []string{redisKey}, l.limitPerMinute, int64(70000)).Int64()
	if err != nil {
		// Graceful degradation: local tier already enforced per-node bound.
		return true
	}
	return res == 1
}
