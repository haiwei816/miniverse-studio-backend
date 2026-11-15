package service

import (
	"fmt"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"
)

func TestLimiterBoundAndExpiration(t *testing.T) {
	s := New(nil, true)
	now := time.Now()
	for i := 0; i < 20000; i++ {
		s.allowRequest(fmt.Sprint(i), now)
	}
	if len(s.limits) != maxLimitEntries {
		t.Fatal(len(s.limits))
	}
	if s.allowRequest("new-source", now) {
		t.Fatal("capacity bypass")
	}
	if !s.allowRequest("0", now) {
		t.Fatal("existing source blocked")
	}
	if !s.allowRequest("new-source", now.Add(time.Minute)) {
		t.Fatal("expired entries not reclaimed")
	}
	if len(s.limits) != 1 {
		t.Fatal(len(s.limits))
	}
	for i := 1; i < 600; i++ {
		if !s.allowRequest("new-source", now.Add(time.Minute)) {
			t.Fatal(i)
		}
	}
	if s.allowRequest("new-source", now.Add(time.Minute)) {
		t.Fatal("rate limit bypass")
	}
}
func TestLimiterConcurrentSources(t *testing.T) {
	s := New(nil, true)
	now := time.Now()
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				s.allowRequest(fmt.Sprintf("%d-%d", worker, i), now)
			}
		}(worker)
	}
	wg.Wait()
	if len(s.limits) > maxLimitEntries {
		t.Fatal("unbounded concurrent growth")
	}
}

func TestClientIPTrustedProxyHandling(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.18.0.5/32"), netip.MustParsePrefix("10.0.0.2/32")}
	for _, tc := range []struct{ peer, xff, real, want string }{
		{"127.0.0.1:4321", "203.0.113.9", "", "127.0.0.1"},
		{"172.18.0.6:1", "203.0.113.9", "", "172.18.0.6"},
		{"172.18.0.5:1", "203.0.113.9, 10.0.0.2", "", "203.0.113.9"},
		{"172.18.0.5:1", "203.0.113.9, 10.0.0.3", "", "10.0.0.3"},
		{"172.18.0.5:1", "broken", "203.0.113.9", "172.18.0.5"},
		{"172.18.0.5:1", "", "203.0.113.42", "203.0.113.42"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.peer
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if tc.real != "" {
			r.Header.Set("X-Real-IP", tc.real)
		}
		if got := clientIP(r, trusted); got != tc.want {
			t.Errorf("peer=%s xff=%q: got %s, want %s", tc.peer, tc.xff, got, tc.want)
		}
	}
}
