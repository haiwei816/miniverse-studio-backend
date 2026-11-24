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

type boardLockFixture struct {
	boardQueries []string
}

type boardLockConn struct{ f *boardLockFixture }
type boardLockDriver struct{}
type boardLockTx struct{}
type boardLockRows struct {
	cols   []string
	values [][]driver.Value
	at     int
}

func (f *boardLockFixture) Connect(context.Context) (driver.Conn, error) {
	return &boardLockConn{f}, nil
}
func (*boardLockFixture) Driver() driver.Driver            { return boardLockDriver{} }
func (boardLockDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (*boardLockConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (*boardLockConn) Close() error              { return nil }
func (*boardLockConn) Begin() (driver.Tx, error) { return boardLockTx{}, nil }
func (boardLockTx) Commit() error                { return nil }
func (boardLockTx) Rollback() error              { return nil }
func (c *boardLockConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "FROM sessions"):
		return &boardLockRows{cols: []string{"player_id"}, values: [][]driver.Value{{"player-1"}}}, nil
	case strings.Contains(q, "FROM games"):
		return &boardLockRows{cols: []string{"active"}, values: [][]driver.Value{{true}}}, nil
	case strings.Contains(q, "FROM rule_boards"):
		return &boardLockRows{cols: []string{"c"}, values: [][]driver.Value{{int64(0)}}}, nil
	case strings.Contains(q, "FROM boards"):
		c.f.boardQueries = append(c.f.boardQueries, q)
		if strings.Contains(q, "SELECT mode,starts_at,ends_at,frozen") {
			return &boardLockRows{cols: []string{"mode", "starts_at", "ends_at", "frozen"}, values: [][]driver.Value{{"max", int64(1), int64(4102444800), false}}}, nil
		}
		if strings.Contains(q, "SELECT mode,settled,item,reward_amount,ends_at") {
			return &boardLockRows{cols: []string{"mode", "settled", "item", "reward_amount", "ends_at"}, values: [][]driver.Value{{"max", false, "wood", int64(5), int64(1)}}}, nil
		}
		return nil, errors.New("unexpected boards query: " + q)
	case strings.Contains(q, "SELECT score FROM scores"):
		return &boardLockRows{cols: []string{"score"}}, nil
	case strings.Contains(q, "SELECT player_id,score,reached_at FROM scores"):
		return &boardLockRows{cols: []string{"player_id", "score", "reached_at"}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (*boardLockConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT IGNORE INTO score_events("),
		strings.HasPrefix(q, "INSERT INTO scores("),
		strings.HasPrefix(q, "UPDATE boards SET frozen=TRUE,settled=TRUE"):
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
}
func (r *boardLockRows) Columns() []string { return r.cols }
func (*boardLockRows) Close() error        { return nil }
func (r *boardLockRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}

func TestBoardScoreUsesShareLockWhileSettleUsesUpdateLock(t *testing.T) {
	f := &boardLockFixture{}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	mux := http.NewServeMux()
	s.boardRoutes(mux)

	scoreReq := httptest.NewRequest("POST", "/v1/games/sky-island/boards/weekly/s1/scores", strings.NewReader(`{"request_id":"r1","score":42}`))
	scoreReq.SetPathValue("game", "sky-island")
	scoreReq.SetPathValue("board", "weekly")
	scoreReq.SetPathValue("season", "s1")
	scoreReq.Header.Set("Authorization", "Bearer tok")
	scoreRes := httptest.NewRecorder()
	mux.ServeHTTP(scoreRes, scoreReq)
	if scoreRes.Code != 200 {
		t.Fatalf("score status=%d body=%s", scoreRes.Code, scoreRes.Body.String())
	}
	if len(f.boardQueries) != 1 || !strings.Contains(f.boardQueries[0], "FOR SHARE") || strings.Contains(f.boardQueries[0], "FOR UPDATE") {
		t.Fatalf("score submission did not use FOR SHARE lock: %v", f.boardQueries)
	}

	settleReq := httptest.NewRequest("POST", "/v1/games/sky-island/boards/weekly/s1/settle", strings.NewReader(`{}`))
	settleReq.SetPathValue("game", "sky-island")
	settleReq.SetPathValue("board", "weekly")
	settleReq.SetPathValue("season", "s1")
	settleReq.Header.Set("Authorization", "Bearer tok")
	settleRes := httptest.NewRecorder()
	mux.ServeHTTP(settleRes, settleReq)
	if settleRes.Code != 200 {
		t.Fatalf("settle status=%d body=%s", settleRes.Code, settleRes.Body.String())
	}
	if len(f.boardQueries) != 2 || !strings.Contains(f.boardQueries[1], "FOR UPDATE") {
		t.Fatalf("settle did not use FOR UPDATE lock: %v", f.boardQueries)
	}
}
