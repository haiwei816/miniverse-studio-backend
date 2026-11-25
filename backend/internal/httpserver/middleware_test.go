package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingID(t *testing.T) {
	var ctxID string
	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestID(r.Context())
		w.WriteHeader(409)
	}))
	a := httptest.NewRecorder()
	b := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-ID", "untrusted")
	h.ServeHTTP(a, r)
	firstCtxID := ctxID
	h.ServeHTTP(b, r)
	if a.Code != 409 || a.Header().Get("X-Request-ID") == "untrusted" || a.Header().Get("X-Request-ID") == b.Header().Get("X-Request-ID") {
		t.Fatal("request ID isolation or status failed")
	}
	if firstCtxID == "" || firstCtxID != a.Header().Get("X-Request-ID") || ctxID != b.Header().Get("X-Request-ID") {
		t.Fatalf("context request ID mismatch: first=%q headerA=%q second=%q headerB=%q", firstCtxID, a.Header().Get("X-Request-ID"), ctxID, b.Header().Get("X-Request-ID"))
	}
}
