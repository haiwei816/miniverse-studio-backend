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
	if strings.Contains(q, "FROM admins") && !strings.Contains(q, "gm_sessions") {
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
		cookies := w.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "mv_gm_session" {
				sessionCookie = c
			}
		}
		if sessionCookie == nil || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("expected secure HttpOnly SameSite=Strict mv_gm_session cookie, got %+v", sessionCookie)
		}
	})
}

func TestGMCookieSessionCSRFAndLogout(t *testing.T) {
	sessions := map[string]string{}
	f := &gmSessionTestFixture{sessions: sessions}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	mux := http.NewServeMux()
	s.gmRoutes(mux)

	sessions[hash("Bearer gm-tok-1")] = "admin"

	// GET /gm/me works with HttpOnly cookie without CSRF header.
	meReq := httptest.NewRequest("GET", "/gm/me", nil)
	meReq.AddCookie(&http.Cookie{Name: "mv_gm_session", Value: "gm-tok-1"})
	meRec := httptest.NewRecorder()
	mux.ServeHTTP(meRec, meReq)
	if meRec.Code != 200 || !strings.Contains(meRec.Body.String(), `"username":"admin"`) {
		t.Fatalf("GET /gm/me with cookie failed: status=%d body=%s", meRec.Code, meRec.Body.String())
	}

	// POST /gm/logout with cookie but missing CSRF header is rejected with 403.
	noCSRF := httptest.NewRequest("POST", "/gm/logout", nil)
	noCSRF.AddCookie(&http.Cookie{Name: "mv_gm_session", Value: "gm-tok-1"})
	noCSRFRec := httptest.NewRecorder()
	mux.ServeHTTP(noCSRFRec, noCSRF)
	if noCSRFRec.Code != 403 || !strings.Contains(noCSRFRec.Body.String(), "csrf_rejected") {
		t.Fatalf("expected 403 csrf_rejected, got status=%d body=%s", noCSRFRec.Code, noCSRFRec.Body.String())
	}

	// POST /gm/logout with CSRF header deletes session and clears cookie.
	logoutReq := httptest.NewRequest("POST", "/gm/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "mv_gm_session", Value: "gm-tok-1"})
	logoutReq.Header.Set("X-MiniVerse-CSRF", "1")
	logoutRec := httptest.NewRecorder()
	mux.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != 200 || len(sessions) != 0 {
		t.Fatalf("POST /gm/logout failed: status=%d sessions=%d body=%s", logoutRec.Code, len(sessions), logoutRec.Body.String())
	}

	// Old cookie is now 401.
	meAfter := httptest.NewRequest("GET", "/gm/me", nil)
	meAfter.AddCookie(&http.Cookie{Name: "mv_gm_session", Value: "gm-tok-1"})
	meAfterRec := httptest.NewRecorder()
	mux.ServeHTTP(meAfterRec, meAfter)
	if meAfterRec.Code != 401 {
		t.Fatalf("expected 401 after logout, got %d", meAfterRec.Code)
	}
}

type gmSessionTestFixture struct {
	sessions map[string]string
}
type gmSessionTestConn struct{ f *gmSessionTestFixture }
type gmSessionTestRows struct {
	cols []string
	rows [][]driver.Value
	at   int
}

func (f *gmSessionTestFixture) Connect(context.Context) (driver.Conn, error) {
	return &gmSessionTestConn{f}, nil
}
func (f *gmSessionTestFixture) Driver() driver.Driver        { return gmAuthDriver{} }
func (*gmSessionTestConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (*gmSessionTestConn) Close() error                        { return nil }
func (*gmSessionTestConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
func (c *gmSessionTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM gm_sessions") {
		h := args[0].Value.(string)
		if id, ok := c.f.sessions[h]; ok {
			return &gmSessionTestRows{cols: []string{"id", "role", "salt"}, rows: [][]driver.Value{{id, "admin", "sandbox-fixture-admin"}}}, nil
		}
		return &gmSessionTestRows{cols: []string{"id", "role", "salt"}}, nil
	}
	if strings.Contains(q, "FROM games g JOIN gm_access") {
		return &gmSessionTestRows{cols: []string{"id", "name"}, rows: [][]driver.Value{{"sky-island", "Sky Island"}}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (c *gmSessionTestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if q == "DELETE FROM gm_sessions WHERE token_hash=?" {
		delete(c.f.sessions, args[0].Value.(string))
		return driver.RowsAffected(1), nil
	}
	return nil, errors.New("unexpected exec: " + q)
}
func (r *gmSessionTestRows) Columns() []string { return r.cols }
func (*gmSessionTestRows) Close() error        { return nil }
func (r *gmSessionTestRows) Next(dest []driver.Value) error {
	if r.at >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.at])
	r.at++
	return nil
}
