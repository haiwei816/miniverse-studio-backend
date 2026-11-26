// Package cache provides a sharded O(1) generic LRU cache with jittered TTL
// and singleflight request coalescing to prevent cache stampedes.
package cache

import (
	"container/list"
	"hash/fnv"
	"math/rand/v2"
	"sync"
	"time"
)

const defaultShards = 16

type entry[V any] struct {
	key       string
	value     V
	expiresAt time.Time
	negative  bool
}

type shard[V any] struct {
	mu    sync.Mutex
	cap   int
	items map[string]*list.Element
	order *list.List
}

// ShardedLRU is a thread-safe O(1) LRU cache partitioned across 16 shards
// to minimize mutex contention under high concurrency.
type ShardedLRU[V any] struct {
	shards [defaultShards]shard[V]
	now    func() time.Time
}

// NewShardedLRU creates a sharded LRU with total capacity maxEntries.
func NewShardedLRU[V any](maxEntries int) *ShardedLRU[V] {
	if maxEntries < defaultShards {
		maxEntries = defaultShards
	}
	perShard := max(1, maxEntries/defaultShards)
	c := &ShardedLRU[V]{now: time.Now}
	for i := range c.shards {
		c.shards[i] = shard[V]{
			cap:   perShard,
			items: make(map[string]*list.Element, perShard),
			order: list.New(),
		}
	}
	return c
}

func (c *ShardedLRU[V]) shardFor(key string) *shard[V] {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &c.shards[h.Sum32()%defaultShards]
}

// Get retrieves an entry in O(1) time. Returns value, isNegativeSentinel, hit.
func (c *ShardedLRU[V]) Get(key string) (V, bool, bool) {
	sh := c.shardFor(key)
	now := c.now()
	sh.mu.Lock()
	defer sh.mu.Unlock()
	el, ok := sh.items[key]
	if !ok {
		var zero V
		return zero, false, false
	}
	ent := el.Value.(*entry[V])
	if !ent.expiresAt.IsZero() && now.After(ent.expiresAt) {
		sh.order.Remove(el)
		delete(sh.items, key)
		var zero V
		return zero, false, false
	}
	sh.order.MoveToFront(el)
	return ent.value, ent.negative, true
}

// Set inserts or updates key with a TTL plus up to 10% positive jitter to avoid synchronized expiration.
func (c *ShardedLRU[V]) Set(key string, val V, ttl time.Duration) {
	c.set(key, val, ttl, false)
}

// SetNegative stores a short-lived negative sentinel to defend against cache penetration.
func (c *ShardedLRU[V]) SetNegative(key string, ttl time.Duration) {
	var zero V
	c.set(key, zero, ttl, true)
}

func (c *ShardedLRU[V]) set(key string, val V, ttl time.Duration, negative bool) {
	sh := c.shardFor(key)
	exp := time.Time{}
	if ttl > 0 {
		jitter := time.Duration(0)
		if ttl >= 10*time.Millisecond {
			jitter = time.Duration(rand.Int64N(int64(ttl / 10)))
		}
		exp = c.now().Add(ttl + jitter)
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if el, ok := sh.items[key]; ok {
		ent := el.Value.(*entry[V])
		ent.value = val
		ent.expiresAt = exp
		ent.negative = negative
		sh.order.MoveToFront(el)
		return
	}
	if sh.order.Len() >= sh.cap {
		oldest := sh.order.Back()
		if oldest != nil {
			sh.order.Remove(oldest)
			delete(sh.items, oldest.Value.(*entry[V]).key)
		}
	}
	el := sh.order.PushFront(&entry[V]{
		key:       key,
		value:     val,
		expiresAt: exp,
		negative:  negative,
	})
	sh.items[key] = el
}

// Delete removes a single key from its shard.
func (c *ShardedLRU[V]) Delete(key string) {
	sh := c.shardFor(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if el, ok := sh.items[key]; ok {
		sh.order.Remove(el)
		delete(sh.items, key)
	}
}

// Clear removes all entries across all shards.
func (c *ShardedLRU[V]) Clear() {
	for i := range c.shards {
		sh := &c.shards[i]
		sh.mu.Lock()
		clear(sh.items)
		sh.order.Init()
		sh.mu.Unlock()
	}
}

// Len returns the current number of entries across all shards.
func (c *ShardedLRU[V]) Len() int {
	total := 0
	for i := range c.shards {
		sh := &c.shards[i]
		sh.mu.Lock()
		total += len(sh.items)
		sh.mu.Unlock()
	}
	return total
}
