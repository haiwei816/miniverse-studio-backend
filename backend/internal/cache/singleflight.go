package cache

import "sync"

type call[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

// Group coalesces concurrent identical lookups by key so only one loader executes
// while all waiting callers share the same result (cache breakdown protection).
type Group[V any] struct {
	mu    sync.Mutex
	calls map[string]*call[V]
}

// Do executes fn at most once concurrently per key.
func (g *Group[V]) Do(key string, fn func() (V, error)) (V, error, bool) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*call[V])
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err, true
	}
	c := &call[V]{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	func() {
		defer func() {
			g.mu.Lock()
			delete(g.calls, key)
			g.mu.Unlock()
			c.wg.Done()
		}()
		c.val, c.err = fn()
	}()
	return c.val, c.err, false
}
