// Package redisx wraps go-redis with nil-safe degradation and Pub/Sub cache invalidation.
package redisx

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const InvalidationChannel = "miniverse:cache:invalidate"

// InvalidationEvent broadcasts cross-instance cache eviction and ban-kick events.
type InvalidationEvent struct {
	Kind     string `json:"kind"` // game | activity | ban | board
	GameID   string `json:"game_id,omitempty"`
	PlayerID string `json:"player_id,omitempty"`
	BoardKey string `json:"board_key,omitempty"`
	Origin   string `json:"origin,omitempty"`
}

// Client wraps *redis.Client with safe nil-receiver semantics when Redis is not configured.
type Client struct {
	rdb    *redis.Client
	nodeID string
}

// Open initializes a Redis client if addr is non-empty. Connection failure does not abort startup;
// operations degrade gracefully to MySQL / in-memory paths until Redis recovers.
func Open(addr, nodeID string) *Client {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return nil
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         trimmed,
		DialTimeout:  500 * time.Millisecond,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
		PoolSize:     32,
		MinIdleConns: 4,
	})
	return &Client{rdb: rdb, nodeID: nodeID}
}

// NewFromUniversal wraps an existing *redis.Client (useful for tests).
func NewFromClient(rdb *redis.Client, nodeID string) *Client {
	if rdb == nil {
		return nil
	}
	return &Client{rdb: rdb, nodeID: nodeID}
}

func (c *Client) Raw() *redis.Client {
	if c == nil {
		return nil
	}
	return c.rdb
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Ping(ctx).Err()
}

func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

// PublishInvalidation broadcasts a cache invalidation event to peer API instances.
func (c *Client) PublishInvalidation(ctx context.Context, ev InvalidationEvent) {
	if c == nil || c.rdb == nil {
		return
	}
	ev.Origin = c.nodeID
	raw, err := json.Marshal(ev)
	if err != nil {
		return
	}
	pubCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := c.rdb.Publish(pubCtx, InvalidationChannel, raw).Err(); err != nil {
		slog.Warn("redis_invalidation_publish_failed", "kind", ev.Kind, "error", err)
	}
}

// StartInvalidationSubscriber listens on InvalidationChannel until ctx is canceled.
func (c *Client) StartInvalidationSubscriber(ctx context.Context, onEvent func(InvalidationEvent)) func() {
	if c == nil || c.rdb == nil || onEvent == nil {
		return func() {}
	}
	subCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if subCtx.Err() != nil {
				return
			}
			ps := c.rdb.Subscribe(subCtx, InvalidationChannel)
			ch := ps.Channel()
		loop:
			for {
				select {
				case <-subCtx.Done():
					_ = ps.Close()
					return
				case msg, ok := <-ch:
					if !ok {
						break loop
					}
					var ev InvalidationEvent
					if json.Unmarshal([]byte(msg.Payload), &ev) != nil {
						continue
					}
					if ev.Origin != "" && ev.Origin == c.nodeID {
						continue
					}
					onEvent(ev)
				}
			}
			_ = ps.Close()
			select {
			case <-subCtx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	return func() { cancel(); <-done }
}
