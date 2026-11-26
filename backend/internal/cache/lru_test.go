package cache

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestShardedLRU_EvictionAndTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	c := NewShardedLRU[int](16) // cap=1 per shard
	c.now = func() time.Time { return now }

	// Fill with enough keys to force eviction in at least one shard.
	for i := 0; i < 64; i++ {
		c.Set(fmt.Sprintf("k-%d", i), i, 10*time.Second)
	}
	if c.Len() > 16 {
		t.Fatalf("expected bounded len <= 16, got %d", c.Len())
	}

	c.SetNegative("missing", 2*time.Second)
	if _, neg, ok := c.Get("missing"); !ok || !neg {
		t.Fatalf("expected negative cache hit")
	}

	now = now.Add(3 * time.Second)
	if _, _, ok := c.Get("missing"); ok {
		t.Fatalf("expected expired entry to be evicted")
	}
}

func TestShardedLRU_ConcurrentAccess(t *testing.T) {
	c := NewShardedLRU[int](256)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("k-%d-%d", id, i%20)
				c.Set(key, i, time.Minute)
				_, _, _ = c.Get(key)
				if i%15 == 0 {
					c.Delete(key)
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestSingleflight_CoalescesConcurrentCalls(t *testing.T) {
	var grp Group[int]
	var calls atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, err, _ := grp.Do("hot-key", func() (int, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond)
				return 42, nil
			})
			if err != nil || v != 42 {
				t.Errorf("unexpected result: v=%d err=%v", v, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 loader execution, got %d", got)
	}
}
