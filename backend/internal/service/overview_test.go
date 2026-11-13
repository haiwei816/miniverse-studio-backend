package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The dashboard reads from many tables at once, so the fixture answers by the
// shape of each query instead of by position: rewording one SELECT keeps the
// other assertions meaningful.
type overviewFixture struct {
	mu      sync.Mutex
	queries int
}

type overviewConn struct{ f *overviewFixture }
type overviewDriver struct{}
type overviewRows struct {
	cols []string
	rows [][]driver.Value
	at   int
}

type overviewAnswer struct {
	contains string
	rows     [][]driver.Value
}

func answers() []overviewAnswer {
	return []overviewAnswer{
		{"FROM gm_sessions", [][]driver.Value{{"admin", "admin", "sandbox-fixture-admin"}}},
		{"FROM gm_access", [][]driver.Value{{int64(1)}}},
		{"FROM games", [][]driver.Value{{"天空岛物语", int64(4)}}},
		{"COUNT(DISTINCT s.player_id)", [][]driver.Value{{int64(2)}}},
		{"FROM profiles WHERE game_id=?", [][]driver.Value{{int64(12)}}},
		{"FROM saves", [][]driver.Value{{int64(9)}}},
		{"FROM mail m", [][]driver.Value{{int64(5)}}},
		{"FROM activities", [][]driver.Value{{int64(3)}}},
		{"FROM inventory", [][]driver.Value{{int64(7)}}},
		{"FROM gm_requests", [][]driver.Value{{int64(1)}}},
		{"DIV 86400", [][]driver.Value{{int64(1700000000), int64(4)}}},
		{"action IN ('operation_failed'", [][]driver.Value{{int64(0)}}},
		{"FROM audits", [][]driver.Value{{int64(11)}}},
		{"FROM grants", [][]driver.Value{{int64(6)}}},
	}
}

func (f *overviewFixture) Connect(context.Context) (driver.Conn, error) { return &overviewConn{f}, nil }
func (f *overviewFixture) Driver() driver.Driver                        { return overviewDriver{} }
func (overviewDriver) Open(string) (driver.Conn, error)                 { return nil, errors.New("use connector") }
func (*overviewConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported prepare")
}
func (*overviewConn) Close() error              { return nil }
func (*overviewConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported transaction") }
func (*overviewConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("unsupported exec")
}
func (c *overviewConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.f.mu.Lock()
	c.f.queries++
	c.f.mu.Unlock()
	for _, a := range answers() {
		if !strings.Contains(q, a.contains) {
			continue
		}
		cols := make([]string, len(a.rows[0]))
		for i := range cols {
			cols[i] = fmt.Sprint("c", i)
		}
		return &overviewRows{cols: cols, rows: a.rows}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (r *overviewRows) Columns() []string { return r.cols }
func (r *overviewRows) Close() error      { return nil }
func (r *overviewRows) Next(dst []driver.Value) error {
	if r.at >= len(r.rows) {
		return io.EOF
	}
	copy(dst, r.rows[r.at])
	r.at++
	return nil
}

func TestOverviewCountsAreServerSide(t *testing.T) {
	fixture := &overviewFixture{}
	db := sql.OpenDB(fixture)
	defer db.Close()
	s := New(db, true)
	m := http.NewServeMux()
	s.overviewRoutes(m)

	r := httptest.NewRequest("GET", "/gm/games/sky-island/overview", nil)
	r.SetPathValue("game", "sky-island")
	r.Header.Set("Authorization", "Bearer gm-token")
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var got struct {
		Game   map[string]any   `json:"game"`
		Role   string           `json:"role"`
		Totals map[string]int64 `json:"totals"`
		Series struct {
			Days   int           `json:"days"`
			Audits []overviewDay `json:"audits"`
			Grants []overviewDay `json:"grants"`
		} `json:"series"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if got.Game["name"] != "天空岛物语" || got.Game["config_version"] != float64(4) {
		t.Fatalf("game header: %+v", got.Game)
	}
	if got.Role != "admin" {
		t.Fatalf("role: %q", got.Role)
	}
	want := map[string]int64{"players": 12, "online": 2, "saves": 9, "mail_pending": 5, "activities_active": 3, "items": 7, "requests_pending": 1, "audits_24h": 11, "failures_24h": 0, "grants_24h": 6}
	for k, v := range want {
		if got.Totals[k] != v {
			t.Fatalf("totals[%s] = %d, want %d", k, got.Totals[k], v)
		}
	}
	if len(got.Series.Audits) != overviewWindow || len(got.Series.Grants) != overviewWindow {
		t.Fatalf("series length: audits %d grants %d", len(got.Series.Audits), len(got.Series.Grants))
	}
	// Days without rows must still appear, otherwise a quiet week breaks the axis.
	var filled int
	for _, d := range got.Series.Audits {
		if d.Count == 0 {
			filled++
		}
		if d.Date == "" || d.Day%86400 != 0 {
			t.Fatalf("series day %+v is not a UTC midnight with a label", d)
		}
	}
	if filled != overviewWindow {
		t.Fatalf("expected the fixture day to fall outside the window and all days zeroed, got %d zero days", filled)
	}
	if fixture.queries < 12 {
		t.Fatalf("expected one query per card, got %d", fixture.queries)
	}
}

func TestOverviewRequiresGameAccess(t *testing.T) {
	fixture := &overviewFixture{}
	db := sql.OpenDB(fixture)
	defer db.Close()
	s := New(db, true)
	m := http.NewServeMux()
	s.overviewRoutes(m)

	r := httptest.NewRequest("GET", "/gm/games/card-game/overview", nil)
	r.SetPathValue("game", "card-game")
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d, want 401 without a GM token", w.Code)
	}
}
