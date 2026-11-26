package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type requestIDKey struct{}
type traceParentKey struct{}

var traceparentRe = regexp.MustCompile(`^00-([a-f0-9]{32})-([a-f0-9]{16})-[a-f0-9]{2}$`)

// RequestID returns the server-generated request correlation ID stored on ctx.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// TraceParent returns the W3C traceparent header value stored on ctx.
func TraceParent(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	tp, _ := ctx.Value(traceParentKey{}).(string)
	return tp
}

// TraceID extracts the 32-char W3C trace ID from ctx when available.
func TraceID(ctx context.Context) string {
	tp := TraceParent(ctx)
	if m := traceparentRe.FindStringSubmatch(tp); len(m) == 3 {
		return m[1]
	}
	return ""
}

func resolveTraceparent(incoming string) string {
	trimmed := strings.ToLower(strings.TrimSpace(incoming))
	if m := traceparentRe.FindStringSubmatch(trimmed); len(m) == 3 &&
		m[1] != "00000000000000000000000000000000" &&
		m[2] != "0000000000000000" {
		var span [8]byte
		_, _ = rand.Read(span[:])
		return "00-" + m[1] + "-" + hex.EncodeToString(span[:]) + "-01"
	}
	var traceBytes [16]byte
	var spanBytes [8]byte
	_, _ = rand.Read(traceBytes[:])
	_, _ = rand.Read(spanBytes[:])
	return "00-" + hex.EncodeToString(traceBytes[:]) + "-" + hex.EncodeToString(spanBytes[:]) + "-01"
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(s int) { w.status = s; w.ResponseWriter.WriteHeader(s) }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		tp := resolveTraceparent(r.Header.Get("traceparent"))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("traceparent", tp)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		ctx = context.WithValue(ctx, traceParentKey{}, tp)
		if m := traceparentRe.FindStringSubmatch(tp); len(m) == 3 {
			w.Header().Set("X-Trace-ID", m[1])
		}
		r = r.WithContext(ctx)
		sw := &statusWriter{w, 200}
		start := time.Now()
		next.ServeHTTP(sw, r)
		slog.Info("http_request", "request_id", id, "trace_id", TraceID(ctx), "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", time.Since(start).Milliseconds())
	})
}
