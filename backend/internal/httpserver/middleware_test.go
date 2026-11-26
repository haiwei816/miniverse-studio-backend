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

func TestTraceparentPropagation(t *testing.T) {
	var gotTraceID string
	h := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceID = TraceID(r.Context())
		w.WriteHeader(200)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if gotTraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || w.Header().Get("X-Trace-ID") != gotTraceID {
		t.Fatalf("expected trace id preserved, got ctx=%q header=%q", gotTraceID, w.Header().Get("X-Trace-ID"))
	}
}
