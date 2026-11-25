package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"miniverse/backend/internal/rulepb"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
)

type domainLoopsTicket struct {
	id         string
	game       string
	player     string
	board      string
	season     string
	version    string
	seed       int64
	expiresAt  int64
	consumedAt *int64
	requestID  string
}

type domainLoopsRequest struct {
	id        string
	game      string
	kind      string
	creator   string
	reviewer  string
	state     string
	reason    string
	payload   string
	createdAt int64
}

type domainLoopsBoard struct {
	id            string
	season        string
	mode          string
	starts        int64
	ends          int64
	frozen        bool
	settled       bool
	item          string
	reward        int64
	ruleVersion   string
	requireTicket bool
}

type domainLoopsBan struct {
	reason    string
	bannedBy  string
	bannedAt  int64
	expiresAt *int64
}

type domainLoopsFixture struct {
	mu           sync.Mutex
	sessions     map[string]string
	gameSessions map[string]string
	launchGrants map[string]string
	bans         map[string]domainLoopsBan
	tickets      map[string]*domainLoopsTicket
	replays      map[string]string
	requests     map[string]*domainLoopsRequest
	boards       map[string]*domainLoopsBoard
	scores       map[string]int64
	audits       []string
	grants       []string
}

func newDomainLoopsFixture() *domainLoopsFixture {
	return &domainLoopsFixture{
		sessions: map[string]string{
			"tok-p1": "player-1",
		},
		gameSessions: map[string]string{
			"gtok-p1": "player-1",
		},
		launchGrants: map[string]string{
			"lg-1": "player-1",
		},
		bans:     map[string]domainLoopsBan{},
		tickets:  map[string]*domainLoopsTicket{},
		replays:  map[string]string{},
		requests: map[string]*domainLoopsRequest{},
		boards: map[string]*domainLoopsBoard{
			"ranked/sandbox": {
				id:            "ranked",
				season:        "sandbox",
				mode:          "max",
				starts:        1,
				ends:          4102444800,
				item:          "gem",
				reward:        5,
				ruleVersion:   "card-v1",
				requireTicket: true,
			},
			"verified/sandbox": {
				id:            "verified",
				season:        "sandbox",
				mode:          "max",
				starts:        1,
				ends:          4102444800,
				item:          "gem",
				reward:        3,
				ruleVersion:   "card-v1",
				requireTicket: false,
			},
		},
		scores: map[string]int64{},
	}
}

type domainLoopsConn struct{ f *domainLoopsFixture }
type domainLoopsDriver struct{}
type domainLoopsTx struct{}
type domainLoopsRows struct {
	cols   []string
	values [][]driver.Value
	at     int
}

func (f *domainLoopsFixture) Connect(context.Context) (driver.Conn, error) {
	return &domainLoopsConn{f}, nil
}
func (*domainLoopsFixture) Driver() driver.Driver            { return domainLoopsDriver{} }
func (domainLoopsDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (*domainLoopsConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (*domainLoopsConn) Close() error                        { return nil }
func (*domainLoopsConn) Begin() (driver.Tx, error)           { return domainLoopsTx{}, nil }
func (domainLoopsTx) Commit() error                          { return nil }
func (domainLoopsTx) Rollback() error                        { return nil }

func (c *domainLoopsConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	switch {
	case strings.Contains(q, "FROM gm_sessions"):
		tokHash, _ := args[0].Value.(string)
		switch tokHash {
		case hash("Bearer gm-admin"):
			return &domainLoopsRows{cols: []string{"id", "role", "salt"}, values: [][]driver.Value{{"admin", "admin", "s"}}}, nil
		case hash("Bearer gm-operator"):
			return &domainLoopsRows{cols: []string{"id", "role", "salt"}, values: [][]driver.Value{{"operator", "operator", "s"}}}, nil
		case hash("Bearer gm-reviewer"):
			return &domainLoopsRows{cols: []string{"id", "role", "salt"}, values: [][]driver.Value{{"reviewer", "reviewer", "s"}}}, nil
		}
		return &domainLoopsRows{cols: []string{"id", "role", "salt"}}, nil
	case strings.Contains(q, "FROM gm_access"):
		return &domainLoopsRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM sessions"):
		tokHash, _ := args[0].Value.(string)
		for tok, pid := range c.f.sessions {
			if hash(tok) == tokHash {
				return &domainLoopsRows{cols: []string{"player_id"}, values: [][]driver.Value{{pid}}}, nil
			}
		}
		return &domainLoopsRows{cols: []string{"player_id"}}, nil
	case strings.Contains(q, "FROM game_sessions"):
		tokHash, _ := args[0].Value.(string)
		for tok, pid := range c.f.gameSessions {
			if hash(tok) == tokHash {
				return &domainLoopsRows{cols: []string{"player_id", "game_id"}, values: [][]driver.Value{{pid, "card-game"}}}, nil
			}
		}
		return &domainLoopsRows{cols: []string{"player_id", "game_id"}}, nil
	case strings.Contains(q, "FROM games"):
		return &domainLoopsRows{cols: []string{"active"}, values: [][]driver.Value{{true}}}, nil
	case strings.Contains(q, "FROM accounts WHERE secret_hash"):
		return &domainLoopsRows{cols: []string{"id"}, values: [][]driver.Value{{"player-1"}}}, nil
	case strings.Contains(q, "FROM profiles"):
		if strings.HasPrefix(q, "SELECT name FROM profiles") {
			return &domainLoopsRows{cols: []string{"name"}, values: [][]driver.Value{{"Player One"}}}, nil
		}
		return &domainLoopsRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM items"):
		return &domainLoopsRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM saves"):
		return &domainLoopsRows{cols: []string{"slot", "revision", "schema_version", "deleted"}}, nil
	case strings.Contains(q, "FROM grants"):
		return &domainLoopsRows{cols: []string{"id", "business_key", "item", "amount"}}, nil
	case strings.Contains(q, "FROM inventory "):
		return &domainLoopsRows{cols: []string{"item", "amount"}}, nil
	case strings.Contains(q, "FROM inventory_ledger"):
		return &domainLoopsRows{cols: []string{"id", "item", "delta", "balance", "reason", "business_key", "created_at"}}, nil
	case strings.Contains(q, "FROM mail "):
		return &domainLoopsRows{cols: []string{"id", "title", "claimed", "is_read", "deleted", "expires_at"}}, nil
	case strings.Contains(q, "FROM player_bans"):
		if strings.HasPrefix(q, "SELECT reason,banned_by") {
			g, _ := args[0].Value.(string)
			p, _ := args[1].Value.(string)
			if b, ok := c.f.bans[g+":"+p]; ok {
				var exp driver.Value
				if b.expiresAt != nil {
					exp = *b.expiresAt
				}
				return &domainLoopsRows{cols: []string{"reason", "banned_by", "banned_at", "expires_at"}, values: [][]driver.Value{{b.reason, b.bannedBy, b.bannedAt, exp}}}, nil
			}
			return &domainLoopsRows{cols: []string{"reason", "banned_by", "banned_at", "expires_at"}}, nil
		}
		p, _ := args[0].Value.(string)
		now := time.Now().Unix()
		var cnt int64
		for key, b := range c.f.bans {
			if strings.HasSuffix(key, ":"+p) && (b.expiresAt == nil || *b.expiresAt > now) {
				if len(args) == 3 {
					g, _ := args[1].Value.(string)
					if key == ":"+p || key == g+":"+p {
						cnt++
					}
				} else {
					cnt++
				}
			}
		}
		return &domainLoopsRows{cols: []string{"c"}, values: [][]driver.Value{{cnt}}}, nil
	case strings.Contains(q, "FROM rule_boards rb JOIN boards b"):
		board, _ := args[1].Value.(string)
		season, _ := args[2].Value.(string)
		b, ok := c.f.boards[board+"/"+season]
		if !ok || b.ruleVersion == "" {
			return &domainLoopsRows{cols: []string{"rule_version", "require_ticket", "starts_at", "ends_at", "frozen"}}, nil
		}
		if strings.Contains(q, "rb.require_ticket") {
			return &domainLoopsRows{
				cols:   []string{"rule_version", "require_ticket", "starts_at", "ends_at", "frozen"},
				values: [][]driver.Value{{b.ruleVersion, b.requireTicket, b.starts, b.ends, b.frozen}},
			}, nil
		}
		return &domainLoopsRows{
			cols:   []string{"rule_version", "starts_at", "ends_at", "frozen"},
			values: [][]driver.Value{{b.ruleVersion, b.starts, b.ends, b.frozen}},
		}, nil
	case strings.Contains(q, "FROM boards b LEFT JOIN rule_boards"):
		var vals [][]driver.Value
		for _, b := range c.f.boards {
			vals = append(vals, []driver.Value{b.id, b.season, b.mode, b.starts, b.ends, b.frozen, b.settled, b.item, b.reward, b.ruleVersion, b.requireTicket})
		}
		return &domainLoopsRows{cols: []string{"id", "season", "mode", "starts_at", "ends_at", "frozen", "settled", "item", "reward_amount", "rule_version", "require_ticket"}, values: vals}, nil
	case strings.Contains(q, "FROM boards"):
		board, _ := args[1].Value.(string)
		season, _ := args[2].Value.(string)
		b, ok := c.f.boards[board+"/"+season]
		if !ok {
			return &domainLoopsRows{cols: []string{"c"}}, nil
		}
		if strings.Contains(q, "SELECT mode,settled") {
			return &domainLoopsRows{cols: []string{"mode", "settled", "item", "reward_amount", "ends_at"}, values: [][]driver.Value{{b.mode, b.settled, b.item, b.reward, b.ends}}}, nil
		}
		if strings.Contains(q, "SELECT settled,ends_at") {
			return &domainLoopsRows{cols: []string{"settled", "ends_at"}, values: [][]driver.Value{{b.settled, b.ends}}}, nil
		}
		return &domainLoopsRows{cols: []string{"mode"}, values: [][]driver.Value{{b.mode}}}, nil
	case strings.Contains(q, "FROM replays"):
		reqID, _ := args[2].Value.(string)
		if ph, ok := c.f.replays[reqID]; ok {
			return &domainLoopsRows{cols: []string{"payload_hash"}, values: [][]driver.Value{{ph}}}, nil
		}
		return &domainLoopsRows{cols: []string{"payload_hash"}}, nil
	case strings.Contains(q, "FROM match_tickets"):
		id, _ := args[0].Value.(string)
		t, ok := c.f.tickets[id]
		if !ok {
			return &domainLoopsRows{cols: []string{"game_id", "player_id", "board_id", "season", "rule_version", "seed", "expires_at", "consumed_at"}}, nil
		}
		var cons driver.Value
		if t.consumedAt != nil {
			cons = *t.consumedAt
		}
		return &domainLoopsRows{
			cols:   []string{"game_id", "player_id", "board_id", "season", "rule_version", "seed", "expires_at", "consumed_at"},
			values: [][]driver.Value{{t.game, t.player, t.board, t.season, t.version, t.seed, t.expiresAt, cons}},
		}, nil
	case strings.Contains(q, "SELECT score FROM scores"):
		return &domainLoopsRows{cols: []string{"score"}}, nil
	case strings.Contains(q, "SELECT player_id,score,reached_at FROM scores"):
		var vals [][]driver.Value
		for p, sc := range c.f.scores {
			vals = append(vals, []driver.Value{p, sc, int64(1000)})
		}
		return &domainLoopsRows{cols: []string{"player_id", "score", "reached_at"}, values: vals}, nil
	case strings.Contains(q, "FROM gm_requests"):
		id, _ := args[1].Value.(string)
		r, ok := c.f.requests[id]
		if !ok {
			return &domainLoopsRows{cols: []string{"creator", "kind", "payload"}}, nil
		}
		return &domainLoopsRows{cols: []string{"creator", "state", "kind", "payload"}, values: [][]driver.Value{{r.creator, r.state, r.kind, []byte(r.payload)}}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}

func (c *domainLoopsConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	switch {
	case strings.HasPrefix(q, "INSERT INTO match_tickets"):
		c.f.tickets[args[0].Value.(string)] = &domainLoopsTicket{
			id:        args[0].Value.(string),
			game:      args[1].Value.(string),
			player:    args[2].Value.(string),
			board:     args[3].Value.(string),
			season:    args[4].Value.(string),
			version:   args[5].Value.(string),
			seed:      args[6].Value.(int64),
			expiresAt: args[8].Value.(int64),
		}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "UPDATE match_tickets SET consumed_at"):
		now := args[0].Value.(int64)
		id := args[2].Value.(string)
		if t, ok := c.f.tickets[id]; ok {
			t.consumedAt = &now
			t.requestID = args[1].Value.(string)
		}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO scores"):
		c.f.scores[args[3].Value.(string)] = args[4].Value.(int64)
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO replays"):
		c.f.replays[args[2].Value.(string)] = args[3].Value.(string)
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO sessions"):
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO player_bans"):
		g := args[0].Value.(string)
		p := args[1].Value.(string)
		var exp *int64
		if args[5].Value != nil {
			v := args[5].Value.(int64)
			exp = &v
		}
		c.f.bans[g+":"+p] = domainLoopsBan{reason: args[2].Value.(string), bannedBy: args[3].Value.(string), bannedAt: args[4].Value.(int64), expiresAt: exp}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "DELETE FROM player_bans"):
		g := args[0].Value.(string)
		p := args[1].Value.(string)
		if _, ok := c.f.bans[g+":"+p]; !ok {
			return driver.RowsAffected(0), nil
		}
		delete(c.f.bans, g+":"+p)
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "DELETE FROM game_sessions"):
		c.f.gameSessions = map[string]string{}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "DELETE FROM launch_grants"):
		c.f.launchGrants = map[string]string{}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "DELETE FROM sessions"):
		c.f.sessions = map[string]string{}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO gm_requests"):
		var payStr string
		switch v := args[5].Value.(type) {
		case []byte:
			payStr = string(v)
		case string:
			payStr = v
		}
		c.f.requests[args[1].Value.(string)] = &domainLoopsRequest{
			id:      args[1].Value.(string),
			game:    args[0].Value.(string),
			kind:    args[2].Value.(string),
			creator: args[3].Value.(string),
			state:   "pending",
			reason:  args[4].Value.(string),
			payload: payStr,
		}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "UPDATE gm_requests"):
		if r, ok := c.f.requests[args[3].Value.(string)]; ok {
			r.state = args[0].Value.(string)
			r.reviewer = args[1].Value.(string)
		}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO boards"):
		b := &domainLoopsBoard{
			id:      args[1].Value.(string),
			season:  args[2].Value.(string),
			mode:    args[3].Value.(string),
			starts:  args[4].Value.(int64),
			ends:    args[5].Value.(int64),
			frozen:  args[6].Value.(bool),
			item:    args[7].Value.(string),
			reward:  args[8].Value.(int64),
		}
		c.f.boards[b.id+"/"+b.season] = b
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO rule_boards"):
		if b, ok := c.f.boards[args[1].Value.(string)+"/"+args[2].Value.(string)]; ok {
			b.ruleVersion = args[3].Value.(string)
			b.requireTicket = args[4].Value.(bool)
		}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT IGNORE INTO grants"):
		c.f.grants = append(c.f.grants, args[3].Value.(string))
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO inventory"), strings.HasPrefix(q, "INSERT INTO inventory_ledger"), strings.HasPrefix(q, "INSERT INTO mail"), strings.HasPrefix(q, "INSERT IGNORE INTO mail"):
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "UPDATE boards SET frozen=TRUE,settled=TRUE"):
		if b, ok := c.f.boards[args[1].Value.(string)+"/"+args[2].Value.(string)]; ok {
			b.frozen = true
			b.settled = true
		}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(q, "INSERT INTO audits"):
		c.f.audits = append(c.f.audits, args[2].Value.(string))
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
}

func (r *domainLoopsRows) Columns() []string { return r.cols }
func (*domainLoopsRows) Close() error        { return nil }
func (r *domainLoopsRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}

type stubRuleServer struct {
	rulepb.UnimplementedRuleCoreServer
}

func (stubRuleServer) Replay(_ context.Context, req *rulepb.ReplayRequest) (*rulepb.ReplayResult, error) {
	if len(req.Actions) == 1 && req.Actions[0] == 9 {
		return &rulepb.ReplayResult{Valid: false, Error: "illegal_action"}, nil
	}
	return &rulepb.ReplayResult{Valid: true, Score: 36, StateHash: 8660768014306689972, Finished: true, Victory: true}, nil
}

func startStubRuleCore(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	rulepb.RegisterRuleCoreServer(srv, stubRuleServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestMatchTicketsAndReplayTrust(t *testing.T) {
	f := newDomainLoopsFixture()
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	s.RuleCoreAddr = startStubRuleCore(t)
	defer s.Close()
	mux := http.NewServeMux()
	s.Routes(mux)

	// Ranked board rejects missing ticket.
	reqNoTicket := httptest.NewRequest("POST", "/v1/games/card-game/replays/validate", strings.NewReader(`{"version":"card-v1","seed":123,"actions":[2,2],"request_id":"no-ticket","board":"ranked","season":"sandbox"}`))
	reqNoTicket.Header.Set("Authorization", "Bearer tok-p1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, reqNoTicket)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "match_ticket_required") {
		t.Fatalf("expected match_ticket_required, got %d %s", w.Code, w.Body.String())
	}

	// Start match issues ticket + random seed.
	startReq := httptest.NewRequest("POST", "/v1/games/card-game/matches/start", strings.NewReader(`{"board":"ranked","season":"sandbox","rule_version":"card-v1"}`))
	startReq.Header.Set("Authorization", "Bearer tok-p1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, startReq)
	if w.Code != 200 {
		t.Fatalf("matches/start failed: %d %s", w.Code, w.Body.String())
	}
	var ticket struct {
		MatchID string `json:"match_id"`
		Seed    uint32 `json:"seed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ticket); err != nil || ticket.MatchID == "" || ticket.Seed == 0 {
		t.Fatalf("unexpected ticket response: %s", w.Body.String())
	}

	// Wrong seed is rejected with 422 seed_mismatch without consuming ticket.
	wrongSeedBody, _ := json.Marshal(map[string]any{
		"version": "card-v1", "seed": ticket.Seed + 1, "actions": []int{2, 2}, "request_id": "bad-seed", "board": "ranked", "season": "sandbox", "match_id": ticket.MatchID,
	})
	w = httptest.NewRecorder()
	rWrong := httptest.NewRequest("POST", "/v1/games/card-game/replays/validate", strings.NewReader(string(wrongSeedBody)))
	rWrong.Header.Set("Authorization", "Bearer tok-p1")
	mux.ServeHTTP(w, rWrong)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "seed_mismatch") {
		t.Fatalf("expected seed_mismatch, got %d %s", w.Code, w.Body.String())
	}

	// Matching seed succeeds with server_seeded_replay trust.
	goodBody, _ := json.Marshal(map[string]any{
		"version": "card-v1", "seed": ticket.Seed, "actions": []int{2, 2}, "request_id": "good-1", "board": "ranked", "season": "sandbox", "match_id": ticket.MatchID,
	})
	w = httptest.NewRecorder()
	rGood := httptest.NewRequest("POST", "/v1/games/card-game/replays/validate", strings.NewReader(string(goodBody)))
	rGood.Header.Set("Authorization", "Bearer tok-p1")
	mux.ServeHTTP(w, rGood)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"server_seeded_replay"`) {
		t.Fatalf("expected server_seeded_replay, got %d %s", w.Code, w.Body.String())
	}

	// New request_id reusing consumed ticket returns 409 match_ticket_consumed.
	reuseBody, _ := json.Marshal(map[string]any{
		"version": "card-v1", "seed": ticket.Seed, "actions": []int{2, 2}, "request_id": "good-2", "board": "ranked", "season": "sandbox", "match_id": ticket.MatchID,
	})
	w = httptest.NewRecorder()
	rReuse := httptest.NewRequest("POST", "/v1/games/card-game/replays/validate", strings.NewReader(string(reuseBody)))
	rReuse.Header.Set("Authorization", "Bearer tok-p1")
	mux.ServeHTTP(w, rReuse)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "match_ticket_consumed") {
		t.Fatalf("expected match_ticket_consumed, got %d %s", w.Code, w.Body.String())
	}
}

func TestPlayerBanEnforcementAtomicKickAndMCPFreeze(t *testing.T) {
	f := newDomainLoopsFixture()
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	s.EnforceBans = true
	mux := http.NewServeMux()
	s.Routes(mux)

	// Operator cannot directly ban (requires approve perm).
	opReq := httptest.NewRequest("POST", "/gm/games/card-game/players/player-1/ban", strings.NewReader(`{"reason":"chargeback fraud"}`))
	opReq.Header.Set("Authorization", "Bearer gm-operator")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, opReq)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "permission_denied") {
		t.Fatalf("expected operator ban rejected, got %d %s", w.Code, w.Body.String())
	}

	// Admin bans player-1 and kicks sessions atomically.
	banReq := httptest.NewRequest("POST", "/gm/games/card-game/players/player-1/ban", strings.NewReader(`{"reason":"chargeback fraud"}`))
	banReq.Header.Set("Authorization", "Bearer gm-admin")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, banReq)
	if w.Code != 200 {
		t.Fatalf("admin ban failed: %d %s", w.Code, w.Body.String())
	}
	if len(f.sessions) != 0 || len(f.gameSessions) != 0 || len(f.launchGrants) != 0 {
		t.Fatalf("expected all sessions kicked atomically, remaining: s=%d gs=%d lg=%d", len(f.sessions), len(f.gameSessions), len(f.launchGrants))
	}

	// Guest relogin blocked with 403 account_suspended.
	reloginReq := httptest.NewRequest("POST", "/v1/auth/guest", strings.NewReader(`{"secret":"sec-1"}`))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, reloginReq)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "account_suspended") {
		t.Fatalf("expected account_suspended on relogin, got %d %s", w.Code, w.Body.String())
	}

	// Restore session manually to verify actor/scope check also enforces ban.
	f.mu.Lock()
	f.sessions["tok-p1"] = "player-1"
	f.mu.Unlock()
	meReq := httptest.NewRequest("GET", "/v1/platform/me", nil)
	meReq.Header.Set("Authorization", "Bearer tok-p1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, meReq)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "account_suspended") {
		t.Fatalf("expected account_suspended on actor check, got %d %s", w.Code, w.Body.String())
	}

	// Unban restores player access.
	unbanReq := httptest.NewRequest("POST", "/gm/games/card-game/players/player-1/unban", strings.NewReader(`{"reason":"appeal accepted"}`))
	unbanReq.Header.Set("Authorization", "Bearer gm-reviewer")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, unbanReq)
	if w.Code != 200 {
		t.Fatalf("unban failed: %d %s", w.Code, w.Body.String())
	}

	// MCP propose_player_freeze creates pending player_ban request, reviewer approves it.
	mcpReq := httptest.NewRequest("POST", "/gm/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"propose_player_freeze","arguments":{"game_id":"card-game","player_id":"player-1","reason":"anomaly spike"}}}`))
	mcpReq.Header.Set("Authorization", "Bearer gm-operator")
	mcpReq.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, mcpReq)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `requires_human_approval`) {
		t.Fatalf("unexpected mcp output: %d %s", w.Code, w.Body.String())
	}
	var freezeReqID string
	for id := range f.requests {
		freezeReqID = id
	}
	approveReq := httptest.NewRequest("POST", "/gm/games/card-game/requests/"+freezeReqID+"/approve", strings.NewReader(`{"reason":"verified anomaly"}`))
	approveReq.Header.Set("Authorization", "Bearer gm-reviewer")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, approveReq)
	if w.Code != 200 {
		t.Fatalf("approve player_ban failed: %d %s", w.Code, w.Body.String())
	}
	if _, banned := f.bans["card-game:player-1"]; !banned {
		t.Fatal("expected player-1 banned after dual approval")
	}
}

func TestGMBoardUpsertAndSeasonSettleDualApproval(t *testing.T) {
	f := newDomainLoopsFixture()
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	mux := http.NewServeMux()
	s.Routes(mux)

	// Operator creates board_upsert request with ended window so it can be settled.
	upReq := httptest.NewRequest("POST", "/gm/games/card-game/requests", strings.NewReader(`{"id":"req-b1","kind":"board_upsert","reason":"create closed cup","payload":{"id":"cup-1","season":"s1","mode":"max","starts_at":1,"ends_at":100,"frozen":false,"item":"gem","reward_amount":5,"rule_version":"card-v1","require_ticket":true}}`))
	upReq.Header.Set("Authorization", "Bearer gm-operator")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, upReq)
	if w.Code != 200 {
		t.Fatalf("create board_upsert failed: %d %s", w.Code, w.Body.String())
	}

	appReq := httptest.NewRequest("POST", "/gm/games/card-game/requests/req-b1/approve", strings.NewReader(`{"reason":"cup approved"}`))
	appReq.Header.Set("Authorization", "Bearer gm-reviewer")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, appReq)
	if w.Code != 200 {
		t.Fatalf("approve board_upsert failed: %d %s", w.Code, w.Body.String())
	}

	// Add score and run season_settle dual approval.
	f.mu.Lock()
	f.scores["player-1"] = 42
	f.mu.Unlock()
	settleCreate := httptest.NewRequest("POST", "/gm/games/card-game/requests", strings.NewReader(`{"id":"req-s1","kind":"season_settle","reason":"settle cup s1","payload":{"board":"cup-1","season":"s1"}}`))
	settleCreate.Header.Set("Authorization", "Bearer gm-operator")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, settleCreate)
	if w.Code != 200 {
		t.Fatalf("create season_settle failed: %d %s", w.Code, w.Body.String())
	}
	settleApp := httptest.NewRequest("POST", "/gm/games/card-game/requests/req-s1/approve", strings.NewReader(`{"reason":"payout verified"}`))
	settleApp.Header.Set("Authorization", "Bearer gm-reviewer")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, settleApp)
	if w.Code != 200 {
		t.Fatalf("approve season_settle failed: %d %s", w.Code, w.Body.String())
	}
	if !f.boards["cup-1/s1"].settled || len(f.grants) != 1 || f.grants[0] != "season:cup-1:s1" {
		t.Fatalf("expected board settled and top player rewarded, got settled=%v grants=%v", f.boards["cup-1/s1"].settled, f.grants)
	}
}
