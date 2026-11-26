package resilience

import (
	"testing"
	"time"
)

func TestCircuitBreaker_Transitions(t *testing.T) {
	now := time.Unix(1000, 0)
	cb := NewCircuitBreaker(3, 5*time.Second)
	cb.now = func() time.Time { return now }

	if cb.State() != StateClosed {
		t.Fatalf("expected closed")
	}

	for i := 0; i < 3; i++ {
		ok, st := cb.Allow()
		if !ok || st != StateClosed {
			t.Fatalf("expected allow in closed")
		}
		cb.Record(false)
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected open after 3 failures, got %s", cb.State())
	}

	if ok, _ := cb.Allow(); ok {
		t.Fatalf("expected reject while open")
	}

	// Advance past openTimeout -> HalfOpen allows exactly 1 probe.
	now = now.Add(6 * time.Second)
	if ok, st := cb.Allow(); !ok || st != StateHalfOpen {
		t.Fatalf("expected single half-open probe, got ok=%v st=%s", ok, st)
	}
	if ok, _ := cb.Allow(); ok {
		t.Fatalf("expected concurrent second probe rejected while half-open in flight")
	}

	// Failed probe re-opens immediately.
	cb.Record(false)
	if cb.State() != StateOpen {
		t.Fatalf("expected open after failed half-open probe")
	}

	// Advance again and succeed -> Closed.
	now = now.Add(6 * time.Second)
	if ok, _ := cb.Allow(); !ok {
		t.Fatalf("expected half-open probe")
	}
	cb.Record(true)
	if cb.State() != StateClosed {
		t.Fatalf("expected closed after successful probe")
	}
}
