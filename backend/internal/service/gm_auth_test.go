package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type gmAdminRecord struct {
	hash string
	salt string
	role string
}

type gmAuthFixture struct {
	admins   map[string]gmAdminRecord
	queryErr error
	queries  int
	inserts  int
}

type gmAuthConn struct{ f *gmAuthFixture }
type gmAuthDriver struct{}
type gmAuthRows struct {
	values []driver.Value
	done   bool
}

func (f *gmAuthFixture) Connect(context.Context) (driver.Conn, error) { return &gmAuthConn{f}, nil }
func (f *gmAuthFixture) Driver() driver.Driver                        { return gmAuthDriver{} }
func (gmAuthDriver) Open(string) (driver.Conn, error)                 { return nil, errors.New("use connector") }
func (*gmAuthConn) Prepare(string) (driver.Stmt, error)               { return nil, errors.New("unsupported") }
func (*gmAuthConn) Close() error                                      { return nil }
func (*gmAuthConn) Begin() (driver.Tx, error)                         { return nil, errors.New("unsupported") }
func (c *gmAuthConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.f.queries++
	if c.f.queryErr != nil {
		return nil, c.f.queryErr
	}
	if strings.Contains(q, "FROM admins") {
		id := args[0].Value.(string)
		if rec, ok := c.f.admins[id]; ok {
			return &gmAuthRows{values: []driver.Value{rec.hash, rec.salt, rec.role}}, nil
		}
		return &gmAuthRows{}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (c *gmAuthConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO gm_sessions(") {
		c.f.inserts++
		return driver.RowsAffected(1), nil
	}
	return nil, errors.New("unexpected exec: " + q)
}
func (*gmAuthRows) Columns() []string { return []string{"password_hash", "salt", "role"} }
func (*gmAuthRows) Close() error      { return nil }
func (r *gmAuthRows) Next(dest []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	copy(dest, r.values)
	r.done = true
	return nil
}

func TestGMAuthLimitsIPAndUsernameIndependentlyFromPlatform(t *testing.T) {
	now := time.Now()
	s := New(nil, true)
	for i := 0; i < 30; i++ {
		if !s.allowGMAuth("192.0.2.1", fmt.Sprintf("gm-%d", i), now) {
			t.Fatal("GM IP quota rejected early")
		}
	}
	if s.allowGMAuth("192.0.2.1", "gm-extra", now) {
		t.Fatal("new GM username bypassed IP limit")
	}
	if !s.allowPlatformAuth("192.0.2.1", "player-1", now) {
		t.Fatal("GM limit spilled over to platform limit")
	}
	for i := 0; i < 15; i++ {
		if !s.allowGMAuth(fmt.Sprintf("198.51.100.%d", i), "admin", now) {
			t.Fatal("GM username quota rejected early")
		}
	}
	if s.allowGMAuth("203.0.113.1", "admin", now) {
		t.Fatal("new IP bypassed GM username limit")
	}
	if !s.allowGMAuth("192.0.2.1", "admin", now.Add(time.Minute)) {
		t.Fatal("expired GM auth limits did not reset")
	}
}

func TestGMLoginRateLimitBeforeDBAndIgnoresUntrustedForwarding(t *testing.T) {
	f := &gmAuthFixture{admins: map[string]gmAdminRecord{}}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	now := time.Now()
	for i := 0; i < 30; i++ {
		s.allowGMAuth("127.0.0.1", fmt.Sprintf("old-%d", i), now)
	}
	mux := http.NewServeMux()
	s.gmRoutes(mux)

	r := httptest.NewRequest("POST", "/gm/login", strings.NewReader(`{"username":"admin","password":"sandbox-admin"}`))
	r.RemoteAddr = "127.0.0.1:4321"
	r.Header.Set("X-Forwarded-For", "203.0.113.99")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 429 || !strings.Contains(w.Body.String(), "auth_rate_limited") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if f.queries != 0 {
		t.Fatalf("rate-limited request queried DB %d times", f.queries)
	}
}

func TestGMLoginHardeningPaths(t *testing.T) {
	salt := "prod-salt"
	adminHash := passwordHash("secret-pass", salt)
	sandboxSalt := "sandbox-fixture-admin"
	sandboxHash := passwordHash("sandbox-admin", sandboxSalt)

	t.Run("missing user runs bounded dummy hash", func(t *testing.T) {
		f := &gmAuthFixture{admins: map[string]gmAdminRecord{}}
		db := sql.OpenDB(f)
		defer db.Close()
		s := New(db, true)
		for i := 0; i < 4; i++ {
			s.authHashes <- struct{}{}
		}
		defer func() {
			for i := 0; i < 4; i++ {
				<-s.authHashes
			}
		}()
		mux := http.NewServeMux()
		s.gmRoutes(mux)

		r := httptest.NewRequest("POST", "/gm/login", strings.NewReader(`{"username":"missing","password":"secret"}`))
		r.RemoteAddr = "127.0.0.1:1001"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "auth_busy") {
			t.Fatalf("missing user bypassed hash semaphore: status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("existing user busy returns 503", func(t *testing.T) {
		f := &gmAuthFixture{admins: map[string]gmAdminRecord{"admin": {adminHash, salt, "admin"}}}
		db := sql.OpenDB(f)
		defer db.Close()
		s := New(db, true)
		for i := 0; i < 4; i++ {
			s.authHashes <- struct{}{}
		}
		defer func() {
			for i := 0; i < 4; i++ {
				<-s.authHashes
			}
		}()
		mux := http.NewServeMux()
		s.gmRoutes(mux)

		r := httptest.NewRequest("POST", "/gm/login", strings.NewReader(`{"username":"admin","password":"secret-pass"}`))
		r.RemoteAddr = "127.0.0.1:1002"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "auth_busy") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("db error returns 500", func(t *testing.T) {
		f := &gmAuthFixture{queryErr: errors.New("mysql down")}
		db := sql.OpenDB(f)
		defer db.Close()
		s := New(db, true)
		mux := http.NewServeMux()
		s.gmRoutes(mux)

		r := httptest.NewRequest("POST", "/gm/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
		r.RemoteAddr = "127.0.0.1:1003"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 500 || !strings.Contains(w.Body.String(), "internal_error") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("production rejects sandbox admin with 403", func(t *testing.T) {
		f := &gmAuthFixture{admins: map[string]gmAdminRecord{"admin": {sandboxHash, sandboxSalt, "admin"}}}
		db := sql.OpenDB(f)
		defer db.Close()
		s := New(db, false)
		mux := http.NewServeMux()
		s.gmRoutes(mux)

		r := httptest.NewRequest("POST", "/gm/login", strings.NewReader(`{"username":"admin","password":"sandbox-admin"}`))
		r.RemoteAddr = "127.0.0.1:1004"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "sandbox_admin_disabled") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("valid login succeeds", func(t *testing.T) {
		f := &gmAuthFixture{admins: map[string]gmAdminRecord{"admin": {adminHash, salt, "admin"}}}
		db := sql.OpenDB(f)
		defer db.Close()
		s := New(db, false)
		mux := http.NewServeMux()
		s.gmRoutes(mux)

		r := httptest.NewRequest("POST", "/gm/login", strings.NewReader(`{"username":" admin ","password":"secret-pass"}`))
		r.RemoteAddr = "127.0.0.1:1005"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 || f.inserts != 1 || !strings.Contains(w.Body.String(), `"username":"admin"`) {
			t.Fatalf("status=%d inserts=%d body=%s", w.Code, f.inserts, w.Body.String())
		}
	})
}
