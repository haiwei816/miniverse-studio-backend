package service

import (
	"fmt"
	"net/http/httptest"
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
	direct := httptest.NewRequest("GET", "/", nil)
	direct.RemoteAddr = "198.51.100.10:12345"
	direct.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(direct); got != "198.51.100.10" {
		t.Fatalf("untrusted proxy header accepted: got %s", got)
	}

	proxied := httptest.NewRequest("GET", "/", nil)
	proxied.RemoteAddr = "172.18.0.5:54321"
	proxied.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.20")
	if got := clientIP(proxied); got != "198.51.100.20" {
		t.Fatalf("expected rightmost client IP from trusted proxy, got %s", got)
	}

	realIP := httptest.NewRequest("GET", "/", nil)
	realIP.RemoteAddr = "127.0.0.1:4321"
	realIP.Header.Set("X-Real-IP", "203.0.113.42")
	if got := clientIP(realIP); got != "203.0.113.42" {
		t.Fatalf("expected X-Real-IP from loopback proxy, got %s", got)
	}
}
