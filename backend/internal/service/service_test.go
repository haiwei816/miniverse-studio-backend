package service

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStrictInput(t *testing.T) {
	for _, body := range []string{`{"name":"ok","admin":true}`, `{"name":"ok"} {"name":"trailing"}`, `{`} {
		var dst struct {
			Name string `json:"name"`
		}
		if decode(httptest.NewRequest("POST", "/", strings.NewReader(body)), &dst) == nil {
			t.Fatalf("accepted invalid body: %s", body)
		}
	}
}
func TestRandomCredential(t *testing.T) {
	a, b := random(), random()
	if len(a) != 48 || a == b || hash(a) == a {
		t.Fatal("invalid credential generation")
	}
}

func TestPlatformUnbindRequiresRealProviderOutsideSandbox(t *testing.T) {
	fixture := &sessionFixture{rows: map[string]sessionRecord{}}
	db := sql.OpenDB(fixture)
	defer db.Close()
	s := New(db, false)
	fixture.rows[s.sessionHash("tok")] = sessionRecord{"player-1", time.Now().Add(time.Hour).Unix()}

	mux := http.NewServeMux()
	s.Routes(mux)
	req := httptest.NewRequest("POST", "/v1/platform/steam/unbind", strings.NewReader(`{"game_id":"card-game","ticket":"mock"}`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "real_provider_not_configured") {
		t.Fatalf("got %d %s, want 503 real_provider_not_configured", rec.Code, rec.Body.String())
	}
}

func TestRuleClientReuseAndClose(t *testing.T) {
	s := New(nil, true)
	c1, err := s.ruleClient("127.0.0.1:50051")
	if err != nil || c1 == nil {
		t.Fatalf("ruleClient init failed: %v", err)
	}
	firstConn := s.ruleConn
	c2, err := s.ruleClient("127.0.0.1:50051")
	if err != nil || c2 == nil || s.ruleConn != firstConn {
		t.Fatal("ruleClient did not reuse existing connection")
	}
	if _, err := s.ruleClient("127.0.0.1:50052"); err != nil {
		t.Fatalf("ruleClient address switch failed: %v", err)
	}
	if s.ruleConn == firstConn {
		t.Fatal("ruleClient did not replace connection on address change")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if s.ruleConn != nil {
		t.Fatal("ruleConn not cleared after Close")
	}
}

func TestGMSessionRequiresBearerPrefix(t *testing.T) {
	s := New(nil, true)
	req := httptest.NewRequest("GET", "/gm/games", nil)
	if _, err := s.gmSession(req); err == nil || err.Error() != "gm_unauthorized" {
		t.Fatalf("expected gm_unauthorized, got %v", err)
	}
}
