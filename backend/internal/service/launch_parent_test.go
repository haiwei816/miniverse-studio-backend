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

type launchParentFixture struct {
	listingReviewFixture
	parentActive bool
	parentLocked bool
}
type launchParentConn struct{ f *launchParentFixture }

func (f *launchParentFixture) Connect(context.Context) (driver.Conn, error) {
	return &launchParentConn{f}, nil
}
func (*launchParentFixture) Driver() driver.Driver { return listingReviewDriver{} }
func (*launchParentConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (*launchParentConn) Close() error { return nil }
func (c *launchParentConn) Begin() (driver.Tx, error) {
	c.f.inTransaction = true
	return &listingReviewTx{&c.f.listingReviewFixture}, nil
}
func (c *launchParentConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "FROM sessions"):
		// The initial actor check succeeds, then the queued transaction sees
		// the session removed by logout when it obtains the session lock.
		if strings.Contains(q, "FOR UPDATE") {
			if !c.f.inTransaction {
				return nil, errors.New("parent lock outside transaction")
			}
			c.f.parentLocked = true
			if !c.f.parentActive {
				return &sessionRows{}, nil
			}
		}
		return &listingReviewRows{[]driver.Value{"player-1"}}, nil
	case strings.Contains(q, "FROM game_listings l JOIN games"):
		return &listingReviewRows{[]driver.Value{"sky-island", "Demo", "Summary", "", "探索", "sky", "✦", "1.0", "/games/sky-island/", "context"}}, nil
	case strings.Contains(q, "SELECT epoch FROM companion_contexts"):
		return &listingReviewRows{[]driver.Value{int64(1)}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (c *launchParentConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if !c.f.parentLocked || !c.f.parentActive || !c.f.inTransaction {
		return nil, errors.New("context written without a valid locked parent session")
	}
	if !strings.HasPrefix(q, "INSERT INTO companion_contexts") && !strings.HasPrefix(q, "INSERT INTO launch_grants") {
		return nil, errors.New("unexpected write: " + q)
	}
	c.f.mutations++
	return driver.RowsAffected(1), nil
}

func TestLaunchRechecksParentBeforeReopeningWorld(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout_won", true: "active_session"}[active], func(t *testing.T) {
			f := &launchParentFixture{parentActive: active}
			db := sql.OpenDB(f)
			defer db.Close()
			mux := http.NewServeMux()
			New(db, true).playerPlatformRoutes(mux)
			r := httptest.NewRequest("POST", "/v1/platform/games/sky-island/launch", strings.NewReader(`{"code_challenge":"`+strings.Repeat("a", 43)+`"}`))
			r.Header.Set("Authorization", "Bearer platform-session")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if !active {
				if w.Code != 401 || !strings.Contains(w.Body.String(), "unauthorized") || f.mutations != 0 || f.commits != 0 {
					t.Fatalf("logout was undone by queued launch: status=%d body=%s fixture=%+v", w.Code, w.Body, f)
				}
			} else if w.Code != 200 || f.mutations != 2 || f.commits != 1 {
				t.Fatalf("active launch rejected: status=%d body=%s fixture=%+v", w.Code, w.Body, f)
			}
		})
	}
}
