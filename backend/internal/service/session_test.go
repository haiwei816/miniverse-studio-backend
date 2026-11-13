package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Small database/sql fixture: exercise real auth/logout handlers without MySQL.
// It intentionally supports only the session queries used by those handlers.
type sessionFixture struct {
	mu   sync.Mutex
	rows map[string]sessionRecord
}
type sessionRecord struct {
	player string
	expiry int64
}
type sessionConn struct{ fixture *sessionFixture }
type sessionDriver struct{}
type sessionRows struct {
	values []driver.Value
	done   bool
}

func (f *sessionFixture) Connect(context.Context) (driver.Conn, error) { return &sessionConn{f}, nil }
func (f *sessionFixture) Driver() driver.Driver                        { return sessionDriver{} }
func (sessionDriver) Open(string) (driver.Conn, error)                 { return nil, errors.New("use connector") }
func (*sessionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported prepare")
}
func (*sessionConn) Close() error              { return nil }
func (*sessionConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported transaction") }
func (c *sessionConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if q != "SELECT player_id FROM sessions WHERE token_hash=? AND expires_at>?" {
		return nil, errors.New("unexpected query: " + q)
	}
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()
	v, ok := c.fixture.rows[args[0].Value.(string)]
	rows := &sessionRows{}
	if ok && v.expiry > args[1].Value.(int64) {
		rows.values = []driver.Value{v.player}
	}
	return rows, nil
}
func (c *sessionConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if q != "DELETE FROM sessions WHERE token_hash=?" {
		return nil, errors.New("unexpected exec: " + q)
	}
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()
	key := args[0].Value.(string)
	_, found := c.fixture.rows[key]
	delete(c.fixture.rows, key)
	if found {
		return driver.RowsAffected(1), nil
	}
	return driver.RowsAffected(0), nil
}
func (*sessionRows) Columns() []string { return []string{"player_id"} }
func (*sessionRows) Close() error      { return nil }
func (r *sessionRows) Next(dst []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	copy(dst, r.values)
	r.done = true
	return nil
}

func TestSessionLogoutRevokesOnlyCurrentEnvironmentSession(t *testing.T) {
	for _, sandbox := range []bool{true, false} {
		name := "production"
		if sandbox {
			name = "sandbox"
		}
		t.Run(name, func(t *testing.T) {
			fixture := &sessionFixture{rows: map[string]sessionRecord{}}
			db := sql.OpenDB(fixture)
			defer db.Close()
			s := New(db, sandbox)
			otherEnv := New(nil, !sandbox)
			future := time.Now().Add(time.Hour).Unix()
			fixture.rows[s.sessionHash("current")] = sessionRecord{"player", future}
			fixture.rows[s.sessionHash("other-session")] = sessionRecord{"player", future}
			fixture.rows[otherEnv.sessionHash("current")] = sessionRecord{"other-env-player", future}
			fixture.rows[s.sessionHash("expired")] = sessionRecord{"player", 1}
			m := http.NewServeMux()
			s.identityRoutes(m)
			m.HandleFunc("GET /private", func(w http.ResponseWriter, r *http.Request) {
				if _, err := s.actor(r); err != nil {
					w.WriteHeader(401)
				}
			})
			check := func(method, path, token string, want int) {
				t.Helper()
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				m.ServeHTTP(w, r)
				if w.Code != want {
					t.Fatalf("%s %s: %d, want %d", method, path, w.Code, want)
				}
			}
			check("GET", "/private", "current", 200)
			check("GET", "/private", "expired", 401)
			check("GET", "/private", "invalid", 401)
			check("POST", "/v1/auth/logout", "current", 200)
			check("GET", "/private", "current", 401)
			check("POST", "/v1/auth/logout", "current", 401)
			check("GET", "/private", "other-session", 200)
			if _, ok := fixture.rows[otherEnv.sessionHash("current")]; !ok {
				t.Fatal("deleted other environment session")
			}
			fixture.rows[otherEnv.sessionHash("foreign-only")] = sessionRecord{"foreign", future}
			check("GET", "/private", "foreign-only", 401)
		})
	}
}
