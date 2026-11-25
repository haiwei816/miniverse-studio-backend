package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStdioBridgeLoginAndForward(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gm/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"gm-tok-123"}`))
		case "/gm/mcp":
			if r.Header.Get("Authorization") != "Bearer gm-tok-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if strings.HasPrefix(req.Method, "notifications/") {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			resp, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]any{"method": req.Method},
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	token, err := resolveToken(context.Background(), srv.URL, srv.Client(), func(k string) string {
		return map[string]string{
			"MINIVERSE_GM_USER":     "admin",
			"MINIVERSE_GM_PASSWORD": "sandbox-admin",
		}[k]
	})
	if err != nil || token != "gm-tok-123" {
		t.Fatalf("resolveToken failed: token=%q err=%v", token, err)
	}

	b := &bridge{baseURL: srv.URL, token: token, client: srv.Client()}
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n",
	)
	var out bytes.Buffer
	if err := serveStdio(context.Background(), b, in, &out); err != nil {
		t.Fatalf("serveStdio failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"initialize"`) || !strings.Contains(lines[1], `"tools/list"`) {
		t.Fatalf("unexpected stdio output: %q", out.String())
	}
}
