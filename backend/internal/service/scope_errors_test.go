package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type scopeFixture struct {
	sessionErr, gameErr                 error
	sessionFound, gameFound, gameActive bool
}
type scopeConn struct{ f *scopeFixture }

func (f *scopeFixture) Connect(context.Context) (driver.Conn, error) { return &scopeConn{f}, nil }
func (*scopeFixture) Driver() driver.Driver                          { return sessionDriver{} }
func (*scopeConn) Prepare(string) (driver.Stmt, error)               { return nil, errors.New("unsupported") }
func (*scopeConn) Close() error                                      { return nil }
func (*scopeConn) Begin() (driver.Tx, error)                         { return nil, errors.New("unsupported") }
func (c *scopeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM sessions") {
		if c.f.sessionErr != nil {
			return nil, c.f.sessionErr
		}
		rows := &sessionRows{}
		if c.f.sessionFound {
			rows.values = []driver.Value{"player"}
		}
		return rows, nil
	}
	if strings.Contains(q, "FROM games") {
		if c.f.gameErr != nil {
			return nil, c.f.gameErr
		}
		rows := &sessionRows{}
		if c.f.gameFound {
			rows.values = []driver.Value{c.f.gameActive}
		}
		return rows, nil
	}
	return nil, errors.New("unexpected query")
}

func TestScopeDatabaseErrorsAreNotAuthenticationFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture scopeFixture
		want    int
	}{
		{"missing session", scopeFixture{}, 401},
		{"session database failure", scopeFixture{sessionErr: errors.New("database unavailable")}, 500},
		{"missing game", scopeFixture{sessionFound: true}, 404},
		{"inactive game", scopeFixture{sessionFound: true, gameFound: true}, 404},
		{"game database failure", scopeFixture{sessionFound: true, gameErr: errors.New("database unavailable")}, 500},
		{"active game", scopeFixture{sessionFound: true, gameFound: true, gameActive: true}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := sql.OpenDB(&tc.fixture)
			defer db.Close()
			s := New(db, false)
			mux := http.NewServeMux()
			s.handle(mux, "GET /games/{game}", func(r *http.Request) (any, error) {
				_, _, err := s.scope(r)
				return map[string]bool{"ok": true}, err
			})
			req := httptest.NewRequest("GET", "/games/demo", nil)
			req.Header.Set("Authorization", "Bearer token")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
