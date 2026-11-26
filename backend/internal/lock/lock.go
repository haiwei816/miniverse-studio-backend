// Package lock implements a Redis Lua distributed mutex with Watchdog auto-renewal
// and owner verification to prevent accidental unlocks by slow holders.
package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrNotAcquired = errors.New("distributed_lock_not_acquired")
	ErrLockLost    = errors.New("distributed_lock_lost")
)

var (
	// renewScript extends TTL only if the caller still owns the key.
	renewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)
	// unlockScript deletes the key only if the caller still owns it (prevents deleting another holder's lock).
	unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)
)

// Store abstracts atomic lock primitives so unit tests can exercise Watchdog & owner checks without Redis.
type Store interface {
	SetNX(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	Renew(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	Unlock(ctx context.Context, key, owner string) (bool, error)
}

type RedisStore struct {
	rdb *redis.Client
}

func NewRedisStore(rdb *redis.Client) *RedisStore {
	if rdb == nil {
		return nil
	}
	return &RedisStore{rdb: rdb}
}

func (s *RedisStore) SetNX(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return s.rdb.SetNX(ctx, key, owner, ttl).Result()
}

func (s *RedisStore) Renew(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	res, err := renewScript.Run(ctx, s.rdb, []string{key}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func (s *RedisStore) Unlock(ctx context.Context, key, owner string) (bool, error) {
	res, err := unlockScript.Run(ctx, s.rdb, []string{key}, owner).Int64()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

// Lease represents an acquired distributed lock backed by a Watchdog renewal goroutine.
type Lease struct {
	store  Store
	key    string
	owner  string
	stop   func()
	lost   chan struct{}
	once   sync.Once
}

func newOwnerToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Acquire attempts to acquire key with TTL and spawns a Watchdog renewing at ttl/3.
// If store is nil, Acquire returns a no-op lease so single-instance degraded mode continues smoothly.
func Acquire(ctx context.Context, store Store, key string, ttl time.Duration) (*Lease, error) {
	if store == nil {
		return &Lease{lost: make(chan struct{}), stop: func() {}}, nil
	}
	if ttl < 150*time.Millisecond {
		ttl = 150 * time.Millisecond
	}
	owner := newOwnerToken()
	ok, err := store.SetNX(ctx, key, owner, ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotAcquired
	}

	watchCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lost := make(chan struct{})
	interval := max(30*time.Millisecond, ttl/3)

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				rCtx, rCancel := context.WithTimeout(watchCtx, interval)
				renewed, rErr := store.Renew(rCtx, key, owner, ttl)
				rCancel()
				if rErr != nil || !renewed {
					close(lost)
					return
				}
			}
		}
	}()

	return &Lease{
		store: store,
		key:   key,
		owner: owner,
		lost:  lost,
		stop:  func() { cancel(); <-done },
	}, nil
}

// Lost returns a channel closed if Watchdog fails to renew ownership.
func (l *Lease) Lost() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.lost
}

// Release stops the Watchdog and releases the lock only if still owned by this Lease.
func (l *Lease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}
	var err error
	l.once.Do(func() {
		if l.stop != nil {
			l.stop()
		}
		if l.store != nil && l.key != "" {
			relCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()
			_, err = l.store.Unlock(relCtx, l.key, l.owner)
		}
	})
	return err
}
