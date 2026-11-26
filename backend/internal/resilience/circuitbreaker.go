// Package resilience implements circuit breaking and multi-tier rate limiting primitives.
package resilience

import (
	"errors"
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

var ErrCircuitOpen = errors.New("circuit_open")

// CircuitBreaker implements a three-state (Closed / Open / Half-Open) breaker.
type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failures         int
	halfOpenInFlight bool
	openedAt         time.Time
	failureThreshold int
	openTimeout      time.Duration
	now              func() time.Time
}

// NewCircuitBreaker constructs a breaker that opens after failureThreshold
// consecutive failures and probes recovery after openTimeout.
func NewCircuitBreaker(failureThreshold int, openTimeout time.Duration) *CircuitBreaker {
	if failureThreshold < 1 {
		failureThreshold = 5
	}
	if openTimeout <= 0 {
		openTimeout = 3 * time.Second
	}
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
		now:              time.Now,
	}
}

// Allow checks if a request may proceed. For Half-Open, only a single probe is admitted.
func (cb *CircuitBreaker) Allow() (bool, State) {
	if cb == nil {
		return true, StateClosed
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true, StateClosed
	case StateOpen:
		if cb.now().Sub(cb.openedAt) >= cb.openTimeout {
			cb.state = StateHalfOpen
			cb.halfOpenInFlight = true
			return true, StateHalfOpen
		}
		return false, StateOpen
	case StateHalfOpen:
		if cb.halfOpenInFlight {
			return false, StateHalfOpen
		}
		cb.halfOpenInFlight = true
		return true, StateHalfOpen
	default:
		return true, StateClosed
	}
}

// Record records the outcome of an admitted call and returns the resulting breaker state.
func (cb *CircuitBreaker) Record(success bool) State {
	if cb == nil {
		return StateClosed
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if success {
		cb.failures = 0
		cb.halfOpenInFlight = false
		cb.state = StateClosed
		return cb.state
	}

	switch cb.state {
	case StateHalfOpen:
		cb.halfOpenInFlight = false
		cb.state = StateOpen
		cb.openedAt = cb.now()
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.failureThreshold {
			cb.state = StateOpen
			cb.openedAt = cb.now()
		}
	case StateOpen:
		cb.openedAt = cb.now()
	}
	return cb.state
}

// State returns the current effective state, promoting expired Open -> HalfOpen lazily.
func (cb *CircuitBreaker) State() State {
	if cb == nil {
		return StateClosed
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateOpen && cb.now().Sub(cb.openedAt) >= cb.openTimeout {
		return StateHalfOpen
	}
	return cb.state
}
