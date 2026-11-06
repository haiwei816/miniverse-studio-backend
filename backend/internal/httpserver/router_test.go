package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter(t *testing.T) {
	router := NewRouter()
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodPost, "/health", http.StatusMethodNotAllowed},
		{http.MethodGet, "/missing", http.StatusNotFound},
		{http.MethodGet, "/health/extra", http.StatusNotFound},
	} {
		t.Run(tt.method+tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if tt.status == http.StatusOK {
				if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
					t.Fatalf("Content-Type = %q", got)
				}
				if got := w.Body.String(); got != "{\"status\":\"ok\"}\n" {
					t.Fatalf("body = %q", got)
				}
			}
		})
	}
}
