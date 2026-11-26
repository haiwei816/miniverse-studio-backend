package lock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memItem struct {
	owner     string
	expiresAt time.Time
}

type memLockStore struct {
	mu     sync.Mutex
	items  map[string]memItem
	renews int
}

func newMemLockStore() *memLockStore {
	return &memLockStore{items: map[string]memItem{}}
}

func (m *memLockStore) SetNX(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if cur, ok := m.items[key]; ok && now.Before(cur.expiresAt) {
		return false, nil
	}
	m.items[key] = memItem{owner: owner, expiresAt: now.Add(ttl)}
	return true, nil
}

func (m *memLockStore) Renew(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.items[key]
	if !ok || time.Now().After(cur.expiresAt) || cur.owner != owner {
		return false, nil
	}
	m.items[key] = memItem{owner: owner, expiresAt: time.Now().Add(ttl)}
	m.renews++
	return true, nil
}

func (m *memLockStore) Unlock(_ context.Context, key, owner string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.items[key]
	if !ok || cur.owner != owner {
		return false, nil
	}
	delete(m.items, key)
	return true, nil
}

func TestDistributedLock_WatchdogAndAntiAccidentalDelete(t *testing.T) {
	store := newMemLockStore()
	ctx := context.Background()

	lease1, err := Acquire(ctx, store, "settle:board1", 150*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}

	// Second caller cannot acquire while lease1 is active.
	if _, err := Acquire(ctx, store, "settle:board1", 150*time.Millisecond); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("expected ErrNotAcquired, got %v", err)
	}

	// Wait long enough for Watchdog to renew multiple times past initial TTL.
	time.Sleep(260 * time.Millisecond)

	store.mu.Lock()
	renews := store.renews
	store.mu.Unlock()
	if renews < 2 {
		t.Fatalf("expected watchdog to renew at least twice, got %d", renews)
	}

	// Simulate lease1 lost / stolen by another owner -> Release must NOT delete owner2's lock.
	store.mu.Lock()
	store.items["settle:board1"] = memItem{owner: "other-owner", expiresAt: time.Now().Add(time.Minute)}
	store.mu.Unlock()

	if err := lease1.Release(ctx); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	store.mu.Lock()
	cur := store.items["settle:board1"]
	store.mu.Unlock()
	if cur.owner != "other-owner" {
		t.Fatalf("owner check failed: lease1 deleted another holder's lock")
	}
}
