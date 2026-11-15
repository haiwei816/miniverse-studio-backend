package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type anomalyFixture struct {
	game, details string
	count         int
}
type anomalyConn struct{ f *anomalyFixture }
type anomalyRows struct {
	cols   []string
	values []driver.Value
}

func (f *anomalyFixture) Connect(context.Context) (driver.Conn, error) { return &anomalyConn{f}, nil }
func (*anomalyFixture) Driver() driver.Driver                          { return sessionDriver{} }
func (*anomalyConn) Prepare(string) (driver.Stmt, error)               { return nil, errors.New("unsupported") }
func (*anomalyConn) Close() error                                      { return nil }
func (*anomalyConn) Begin() (driver.Tx, error)                         { return nil, errors.New("unsupported") }
func (c *anomalyConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM game_sessions") {
		return &anomalyRows{cols: []string{"player_id", "game_id"}, values: []driver.Value{"player-1", "sky-island"}}, nil
	}
	if strings.Contains(q, "FROM games") {
		return &anomalyRows{cols: []string{"active"}, values: []driver.Value{true}}, nil
	}
	return nil, errors.New("unexpected query")
}
func (c *anomalyConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(q, "INSERT INTO audits") {
		return nil, errors.New("unexpected write")
	}
	c.f.game = args[1].Value.(string)
	c.f.details = string(args[4].Value.([]byte))
	c.f.count++
	return driver.RowsAffected(1), nil
}
func (r *anomalyRows) Columns() []string { return r.cols }
func (*anomalyRows) Close() error        { return nil }
func (r *anomalyRows) Next(dst []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(dst, r.values)
	r.values = nil
	return nil
}

func TestAnomalyAuditsAcceptValidGameSessionAndRejectWrongScope(t *testing.T) {
	f := &anomalyFixture{}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	r := httptest.NewRequest("POST", "/v1/games/sky-island/boards/demo/s1/scores", nil)
	r.SetPathValue("game", "sky-island")
	r.Header.Set("Authorization", "Bearer g_game")
	s.recordFailure(r, "abnormal_score")
	if f.count != 1 || f.game != "sky-island" || !strings.Contains(f.details, `"player_id":"player-1"`) {
		t.Fatalf("game anomaly missing: %+v", f)
	}
	r.SetPathValue("game", "card-game")
	s.recordFailure(r, "abnormal_score")
	if f.count != 1 {
		t.Fatal("invalid game scope produced an audit attributed to another game")
	}
}
