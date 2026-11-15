package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlatformAuthLimitsBothAddressAndAccount(t *testing.T) {
	now := time.Now()
	s := New(nil, true)
	for i := 0; i < 30; i++ {
		if !s.allowPlatformAuth("192.0.2.1", fmt.Sprintf("user-%d", i), now) {
			t.Fatal("address quota rejected early")
		}
	}
	if s.allowPlatformAuth("192.0.2.1", "another-user", now) {
		t.Fatal("new usernames bypassed address limit")
	}
	for i := 0; i < 10; i++ {
		if !s.allowPlatformAuth(fmt.Sprintf("198.51.100.%d", i), "same-account", now) {
			t.Fatal("account quota rejected early")
		}
	}
	if s.allowPlatformAuth("203.0.113.1", "same-account", now) {
		t.Fatal("new address bypassed account limit")
	}
	if !s.allowPlatformAuth("192.0.2.1", "same-account", now.Add(time.Minute)) {
		t.Fatal("expired limits did not reset")
	}
}

func TestPlatformAuthStateBoundAndConcurrentCharging(t *testing.T) {
	now := time.Now()
	s := New(nil, true)
	s.nextAuthSweep = now.Add(time.Minute)
	for i := 0; i < maxPlatformAuthEntries-1; i++ {
		s.authLimits[fmt.Sprintf("existing-%d", i)] = bucket{start: now}
	}
	if s.allowPlatformAuth("new-source", "new-user", now) {
		t.Fatal("limit table exceeded bound")
	}
	if len(s.authLimits) != maxPlatformAuthEntries-1 {
		t.Fatal("failed request partially allocated state")
	}
	if !s.allowPlatformAuth("new-source", "new-user", now.Add(time.Minute)) {
		t.Fatal("expired full table did not recover")
	}
	if len(s.authLimits) != 2 {
		t.Fatal("expired entries retained")
	}
	s = New(nil, true)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if s.allowPlatformAuth("one-source", fmt.Sprintf("user-%d", i), now) {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 30 {
		t.Fatalf("concurrent address quota accepted %d", accepted.Load())
	}
}

func TestPlatformAuthRejectsBeforeDatabaseAndIgnoresForgedForwarding(t *testing.T) {
	s := New(nil, true)
	now := time.Now()
	for i := 0; i < 30; i++ {
		s.allowPlatformAuth("127.0.0.1", fmt.Sprintf("old-%d", i), now)
	}
	mux := http.NewServeMux()
	s.playerPlatformRoutes(mux)
	for _, action := range []string{"register", "login"} {
		r := httptest.NewRequest("POST", "/v1/platform/auth/"+action, strings.NewReader(`{"username":"fresh-user","password":"password-123"}`))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Forwarded-For", "203.0.113.88")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 429 || !strings.Contains(w.Body.String(), "auth_rate_limited") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestPlatformPasswordHashConcurrencyBound(t *testing.T) {
	s := New(nil, true)
	for i := 0; i < 4; i++ {
		s.authHashes <- struct{}{}
	}
	if _, e := s.platformPasswordHash("password-123", "salt"); e == nil || e.Error() != "auth_busy" {
		t.Fatalf("hash overload accepted: %v", e)
	}
	<-s.authHashes
	if value, e := s.platformPasswordHash("password-123", "salt"); e != nil || value == "" {
		t.Fatalf("available slot did not recover: %v", e)
	}
	if len(s.authHashes) != 3 {
		t.Fatal("hash slot leaked")
	}
}
