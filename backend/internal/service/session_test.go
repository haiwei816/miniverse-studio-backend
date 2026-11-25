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
	"sync"
	"testing"
	"time"
)

// Small database/sql fixture: exercise real auth/logout handlers without MySQL.
// It intentionally supports only the session queries used by those handlers.
type sessionFixture struct {
	mu                    sync.Mutex
	rows                  map[string]sessionRecord
	contexts              map[string]companionContext
	failClear, failDelete bool
}
type sessionRecord struct {
	player string
	expiry int64
}
type sessionConn struct {
	fixture *sessionFixture
	tx      *sessionTx
}
type sessionTx struct {
	conn     *sessionConn
	rows     map[string]sessionRecord
	contexts map[string]companionContext
}
type sessionDriver struct{}
type sessionRows struct {
	values []driver.Value
	cols   []string
	done   bool
}

func (f *sessionFixture) Connect(context.Context) (driver.Conn, error) {
	return &sessionConn{fixture: f}, nil
}
func (f *sessionFixture) Driver() driver.Driver        { return sessionDriver{} }
func (sessionDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (*sessionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported prepare")
}
func (*sessionConn) Close() error { return nil }
func (c *sessionConn) Begin() (driver.Tx, error) {
	c.fixture.mu.Lock()
	tx := &sessionTx{conn: c, rows: map[string]sessionRecord{}, contexts: map[string]companionContext{}}
	for k, v := range c.fixture.rows {
		tx.rows[k] = v
	}
	for k, v := range c.fixture.contexts {
		tx.contexts[k] = v
	}
	c.tx = tx
	return tx, nil
}
func (tx *sessionTx) Commit() error {
	tx.conn.fixture.rows = tx.rows
	tx.conn.fixture.contexts = tx.contexts
	tx.conn.tx = nil
	tx.conn.fixture.mu.Unlock()
	return nil
}
func (tx *sessionTx) Rollback() error {
	tx.conn.tx = nil
	tx.conn.fixture.mu.Unlock()
	return nil
}
func (c *sessionConn) lock() func() {
	if c.tx != nil {
		return func() {}
	}
	c.fixture.mu.Lock()
	return c.fixture.mu.Unlock
}
func (c *sessionConn) records() map[string]sessionRecord {
	if c.tx != nil {
		return c.tx.rows
	}
	return c.fixture.rows
}
func (c *sessionConn) contexts() map[string]companionContext {
	if c.tx != nil {
		return c.tx.contexts
	}
	if c.fixture.contexts == nil {
		c.fixture.contexts = map[string]companionContext{}
	}
	return c.fixture.contexts
}
func (c *sessionConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	unlock := c.lock()
	defer unlock()
	rows := &sessionRows{}
	switch strings.TrimSuffix(q, " FOR UPDATE") {
	case "SELECT player_id FROM sessions WHERE token_hash=? AND expires_at>?":
		v, ok := c.records()[args[0].Value.(string)]
		if ok && v.expiry > args[1].Value.(int64) {
			rows.values = []driver.Value{v.player}
		}
	case "SELECT epoch FROM companion_contexts WHERE player_id=?":
		rows.cols = []string{"epoch"}
		if v, ok := c.contexts()[args[0].Value.(string)]; ok {
			rows.values = []driver.Value{v.Epoch}
		}
	case "SELECT game_id,epoch,scene_id,quest_stage,updated_at FROM companion_contexts WHERE player_id=?":
		rows.cols = []string{"game_id", "epoch", "scene_id", "quest_stage", "updated_at"}
		if v, ok := c.contexts()[args[0].Value.(string)]; ok {
			rows.values = []driver.Value{v.Game, v.Epoch, v.Scene, v.Quest, v.Updated}
		}
	default:
		return nil, errors.New("unexpected query: " + q)
	}
	return rows, nil
}
func (c *sessionConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	unlock := c.lock()
	defer unlock()
	if strings.HasPrefix(q, "INSERT INTO companion_contexts(") {
		if c.fixture.failClear {
			return nil, errors.New("context write failed")
		}
		key := args[0].Value.(string)
		previous := c.contexts()[key]
		c.contexts()[key] = companionContext{Game: args[1].Value.(string), Epoch: previous.Epoch + 1, Targets: []string{}, Updated: args[2].Value.(int64)}
		return driver.RowsAffected(1), nil
	}
	if q != "DELETE FROM sessions WHERE token_hash=?" {
		return nil, errors.New("unexpected exec: " + q)
	}
	if c.fixture.failDelete {
		return nil, errors.New("session delete failed")
	}
	key := args[0].Value.(string)
	_, found := c.records()[key]
	delete(c.records(), key)
	if found {
		return driver.RowsAffected(1), nil
	}
	return driver.RowsAffected(0), nil
}
func (r *sessionRows) Columns() []string {
	if r.cols != nil {
		return r.cols
	}
	return []string{"player_id"}
}
func (*sessionRows) Close() error { return nil }
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

func TestBothLogoutRoutesClearWorldAtomically(t *testing.T) {
	for _, path := range []string{"/v1/auth/logout", "/v1/platform/auth/logout"} {
		for _, failure := range []string{"", "clear", "delete"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				f := &sessionFixture{
					rows:      map[string]sessionRecord{},
					contexts:  map[string]companionContext{"player": {Game: "sky-island", Epoch: 7, Scene: "old-world"}},
					failClear: failure == "clear", failDelete: failure == "delete",
				}
				db := sql.OpenDB(f)
				defer db.Close()
				s := New(db, true)
				f.rows[s.sessionHash("current")] = sessionRecord{"player", time.Now().Add(time.Hour).Unix()}
				f.rows[s.sessionHash("other")] = sessionRecord{"player", time.Now().Add(time.Hour).Unix()}
				mux := http.NewServeMux()
				s.identityRoutes(mux)
				s.playerPlatformRoutes(mux)
				r := httptest.NewRequest("POST", path, nil)
				r.Header.Set("Authorization", "Bearer current")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				_, retained := f.rows[s.sessionHash("current")]
				world := f.contexts["player"]
				if failure == "" {
					if w.Code != 200 || retained || world.Epoch != 8 || world.Game != "" || world.Scene != "" {
						t.Fatalf("logout did not revoke and clear: status=%d retained=%v world=%+v body=%s", w.Code, retained, world, w.Body.String())
					}
				} else if w.Code != 500 || !retained || world.Epoch != 7 || world.Game != "sky-island" || world.Scene != "old-world" {
					t.Fatalf("failed logout partially committed: status=%d retained=%v world=%+v body=%s", w.Code, retained, world, w.Body.String())
				}
				if _, ok := f.rows[s.sessionHash("other")]; !ok {
					t.Fatal("logout deleted another platform session")
				}
			})
		}
	}
}

func TestPlayerCookieSessionAndCSRF(t *testing.T) {
	f := &sessionFixture{rows: map[string]sessionRecord{}}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	f.rows[s.sessionHash("cookie-tok")] = sessionRecord{"player-1", time.Now().Add(time.Hour).Unix()}

	mux := http.NewServeMux()
	s.identityRoutes(mux)
	s.handle(mux, "GET /private", func(r *http.Request) (any, error) {
		p, err := s.actor(r)
		return map[string]string{"player_id": p}, err
	})

	// GET works with HttpOnly cookie without CSRF header.
	getReq := httptest.NewRequest("GET", "/private", nil)
	getReq.AddCookie(&http.Cookie{Name: "mv_player_session", Value: "cookie-tok"})
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != 200 || !strings.Contains(getRec.Body.String(), `"player_id":"player-1"`) {
		t.Fatalf("GET with player cookie failed: status=%d body=%s", getRec.Code, getRec.Body.String())
	}

	// POST with cookie but without X-MiniVerse-CSRF is rejected.
	noCSRF := httptest.NewRequest("POST", "/v1/auth/logout", nil)
	noCSRF.AddCookie(&http.Cookie{Name: "mv_player_session", Value: "cookie-tok"})
	noCSRFRec := httptest.NewRecorder()
	mux.ServeHTTP(noCSRFRec, noCSRF)
	if noCSRFRec.Code != 403 || !strings.Contains(noCSRFRec.Body.String(), "csrf_rejected") {
		t.Fatalf("expected 403 csrf_rejected, got status=%d body=%s", noCSRFRec.Code, noCSRFRec.Body.String())
	}

	// POST with cookie and CSRF revokes session and clears cookie.
	logoutReq := httptest.NewRequest("POST", "/v1/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "mv_player_session", Value: "cookie-tok"})
	logoutReq.Header.Set("X-MiniVerse-CSRF", "1")
	logoutRec := httptest.NewRecorder()
	mux.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != 200 {
		t.Fatalf("POST logout with cookie+CSRF failed: status=%d body=%s", logoutRec.Code, logoutRec.Body.String())
	}
	if _, retained := f.rows[s.sessionHash("cookie-tok")]; retained {
		t.Fatal("cookie logout did not revoke session")
	}
}
