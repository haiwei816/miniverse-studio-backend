package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mcpFixture struct {
	role             string
	allowedGames     map[string]bool
	requestsCreated  int
	lastCreatedKind  string
	lastCreatedState string
	auditsCreated    int
	mailExpired      bool
}

type mcpConn struct{ f *mcpFixture }
type mcpDriver struct{}
type mcpTx struct{}
type mcpRows struct {
	cols   []string
	values [][]driver.Value
	at     int
}

func (f *mcpFixture) Connect(context.Context) (driver.Conn, error) { return &mcpConn{f}, nil }
func (*mcpFixture) Driver() driver.Driver                        { return mcpDriver{} }
func (mcpDriver) Open(string) (driver.Conn, error)               { return nil, errors.New("use connector") }
func (*mcpConn) Prepare(string) (driver.Stmt, error)             { return nil, errors.New("unsupported") }
func (*mcpConn) Close() error                                    { return nil }
func (*mcpConn) Begin() (driver.Tx, error)                       { return mcpTx{}, nil }
func (mcpTx) Commit() error                                      { return nil }
func (mcpTx) Rollback() error                                    { return nil }

func (c *mcpConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "FROM gm_sessions"):
		return &mcpRows{cols: []string{"id", "role", "salt"}, values: [][]driver.Value{{"actor-1", c.f.role, "sandbox-fixture-" + c.f.role}}}, nil
	case strings.Contains(q, "FROM gm_access"):
		game := args[1].Value.(string)
		if c.f.allowedGames[game] {
			return &mcpRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
		}
		return &mcpRows{cols: []string{"c"}, values: [][]driver.Value{{int64(0)}}}, nil
	case strings.Contains(q, "FROM items"):
		return &mcpRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM profiles WHERE game_id=? AND player_id IN"):
		return &mcpRows{cols: []string{"c"}, values: [][]driver.Value{{int64(len(args) - 1)}}}, nil
	case strings.Contains(q, "SELECT name FROM profiles"):
		return &mcpRows{cols: []string{"name"}, values: [][]driver.Value{{"Hero"}}}, nil
	case strings.Contains(q, "FROM activities WHERE game_id=?"):
		cfg := `{"id":"build-challenge","version":1,"kind":"cumulative","event_type":"building_upgraded","target":3,"starts_at":1,"ends_at":4102444800,"client_allowed":true,"item":"wood","reward_amount":5}`
		return &mcpRows{cols: []string{"id", "version", "config", "active"}, values: [][]driver.Value{{"build-challenge", int64(1), []byte(cfg), true}}}, nil
	case strings.Contains(q, "SELECT progress FROM activity_progress"):
		return &mcpRows{cols: []string{"progress"}, values: [][]driver.Value{{int64(3)}}}, nil
	case strings.Contains(q, "SELECT COUNT(*) FROM events"):
		return &mcpRows{cols: []string{"c"}, values: [][]driver.Value{{int64(3)}}}, nil
	case strings.Contains(q, "SELECT id FROM grants WHERE game_id=?"):
		return &mcpRows{cols: []string{"id"}, values: [][]driver.Value{{"grant-1"}}}, nil
	case strings.Contains(q, "FROM mail m JOIN grants g"):
		exp := time.Now().Add(time.Hour).Unix()
		if c.f.mailExpired {
			exp = 100
		}
		return &mcpRows{
			cols:   []string{"id", "title", "claimed", "deleted", "expires_at", "business_key", "item", "amount"},
			values: [][]driver.Value{{"mail-1", "奖励到账", int64(0), int64(0), exp, "activity:build-challenge:1:all", "wood", int64(5)}},
		}, nil
	case strings.Contains(q, "FROM inventory WHERE game_id=?"):
		return &mcpRows{cols: []string{"item", "amount"}, values: [][]driver.Value{{"wood", int64(0)}}}, nil
	case strings.Contains(q, "FROM inventory_ledger"):
		return &mcpRows{cols: []string{"id", "item", "delta", "balance", "reason", "business_key", "created_at"}}, nil
	case strings.Contains(q, "FROM gm_requests WHERE game_id=? AND id=?"):
		return &mcpRows{cols: []string{"creator", "kind", "payload"}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}

func (c *mcpConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT INTO gm_requests("):
		c.f.requestsCreated++
		c.f.lastCreatedKind = args[2].Value.(string)
		c.f.lastCreatedState = "pending"
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO audits("):
		c.f.auditsCreated++
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
}

func (r *mcpRows) Columns() []string { return r.cols }
func (*mcpRows) Close() error        { return nil }
func (r *mcpRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}

func callMCPHTTP(mux http.Handler, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/gm/mcp", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestMCPRBACAndCrossGameIsolation(t *testing.T) {
	f := &mcpFixture{role: "support", allowedGames: map[string]bool{"sky-island": true}}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	mux := http.NewServeMux()
	s.mcpRoutes(mux)

	if rec := callMCPHTTP(mux, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); rec.Code != 401 {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	rec := callMCPHTTP(mux, "tok", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "propose_reward_compensation") || !strings.Contains(rec.Body.String(), "explain_reward_issue") {
		t.Fatalf("unexpected tools/list for support: %d %s", rec.Code, rec.Body.String())
	}

	rec = callMCPHTTP(mux, "tok", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"propose_reward_compensation","arguments":{"game_id":"sky-island","player_id":"p1","item":"wood","amount":5,"reason":"test"}}}`)
	if !strings.Contains(rec.Body.String(), `"code":-32602`) || f.requestsCreated != 0 {
		t.Fatalf("support invoked write proposal: %s", rec.Body.String())
	}

	rec = callMCPHTTP(mux, "tok", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"explain_reward_issue","arguments":{"game_id":"card-game","player_id":"p1"}}}`)
	if !strings.Contains(rec.Body.String(), `"isError":true`) || !strings.Contains(rec.Body.String(), "game_forbidden") {
		t.Fatalf("cross-game call not blocked: %s", rec.Body.String())
	}
}

func TestMCPExplainRewardIssueAndControlledProposal(t *testing.T) {
	f := &mcpFixture{role: "operator", allowedGames: map[string]bool{"sky-island": true}, mailExpired: true}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	mux := http.NewServeMux()
	s.mcpRoutes(mux)

	rec := callMCPHTTP(mux, "tok", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"explain_reward_issue","arguments":{"game_id":"sky-island","player_id":"p1"}}}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "reward_mail_expired") {
		t.Fatalf("explain_reward_issue missed expired mail: %s", rec.Body.String())
	}

	rec = callMCPHTTP(mux, "tok", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"propose_reward_compensation","arguments":{"game_id":"sky-island","player_id":"p1","item":"wood","amount":5,"reason":"expired mail compensation"}}}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"pending"`) || !strings.Contains(rec.Body.String(), `"requires_human_approval":true`) {
		t.Fatalf("proposal failed: %s", rec.Body.String())
	}
	if f.requestsCreated != 1 || f.lastCreatedKind != "reward" || f.auditsCreated != 1 {
		t.Fatalf("unexpected mutation counts: %+v", f)
	}
	var rpc struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rpc); err != nil {
		t.Fatal(err)
	}
	if rpc.Result.StructuredContent["state"] != "pending" {
		t.Fatalf("expected pending state, got %+v", rpc.Result.StructuredContent)
	}
}
