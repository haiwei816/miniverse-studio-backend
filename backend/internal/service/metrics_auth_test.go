package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type metricsFixture struct{}
type metricsConn struct{}
type metricsDriver struct{}
type metricsRows struct {
	cols []string
	at   int
}

func (f *metricsFixture) Connect(context.Context) (driver.Conn, error) { return &metricsConn{}, nil }
func (*metricsFixture) Driver() driver.Driver                        { return metricsDriver{} }
func (metricsDriver) Open(string) (driver.Conn, error)               { return nil, errors.New("use connector") }
func (*metricsConn) Prepare(string) (driver.Stmt, error)             { return nil, errors.New("unsupported") }
func (*metricsConn) Close() error                                    { return nil }
func (*metricsConn) Begin() (driver.Tx, error)                       { return nil, errors.New("unsupported") }
func (*metricsConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM gm_sessions") {
		tokHash, _ := args[0].Value.(string)
		if tokHash == hash("Bearer gm-ok") {
			return &metricsRows{cols: []string{"id", "role", "salt"}, at: 0}, nil
		}
		return &metricsRows{cols: []string{"id", "role", "salt"}, at: 1}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (*metricsConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("unexpected exec")
}
func (r *metricsRows) Columns() []string { return r.cols }
func (*metricsRows) Close() error        { return nil }
func (r *metricsRows) Next(dest []driver.Value) error {
	if r.at > 0 {
		return io.EOF
	}
	dest[0] = "admin"
	dest[1] = "admin"
	dest[2] = "s"
	r.at++
	return nil
}

func TestMetricsEndpointAuth(t *testing.T) {
	db := sql.OpenDB(&metricsFixture{})
	defer db.Close()
	s := New(db, true)
	s.MetricsBearerToken = "prom-scrape-secret-1234"
	mux := http.NewServeMux()
	s.Routes(mux)

	// Dedicated scraper bearer token succeeds.
	r1 := httptest.NewRequest("GET", "/gm/system/metrics", nil)
	r1.Header.Set("Authorization", "Bearer prom-scrape-secret-1234")
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, r1)
	if w1.Code != 200 || !strings.Contains(w1.Body.String(), "miniverse_http_latency_p99_ms") {
		t.Fatalf("expected 200 prometheus output, got %d %s", w1.Code, w1.Body.String())
	}

	// Wrong token without GM session fails 401.
	r2 := httptest.NewRequest("GET", "/gm/system/metrics", nil)
	r2.Header.Set("Authorization", "Bearer wrong-token")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, r2)
	if w2.Code != 401 {
		t.Fatalf("expected 401 for wrong token, got %d", w2.Code)
	}

	// GM session token continues to work as fallback.
	r3 := httptest.NewRequest("GET", "/gm/system/metrics", nil)
	r3.Header.Set("Authorization", "Bearer gm-ok")
	w3 := httptest.NewRecorder()
	mux.ServeHTTP(w3, r3)
	if w3.Code != 200 {
		t.Fatalf("expected 200 for GM fallback, got %d %s", w3.Code, w3.Body.String())
	}
}
