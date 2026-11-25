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
)

type opsAgentFixture struct {
	role            string
	runsInserted    int
	steps           int
	audits          int
	requests        int
	configWrites    int
	lastPlainKey    string
	lastCipherKey   string
	lastKeyEnc      bool
}

type opsAgentConn struct{ f *opsAgentFixture }
type opsAgentDriver struct{}
type opsAgentTx struct{}
type opsAgentRows struct {
	cols   []string
	values [][]driver.Value
	at     int
}

func (f *opsAgentFixture) Connect(context.Context) (driver.Conn, error) { return &opsAgentConn{f}, nil }
func (*opsAgentFixture) Driver() driver.Driver                        { return opsAgentDriver{} }
func (opsAgentDriver) Open(string) (driver.Conn, error)               { return nil, errors.New("use connector") }
func (*opsAgentConn) Prepare(string) (driver.Stmt, error)             { return nil, errors.New("unsupported") }
func (*opsAgentConn) Close() error                                    { return nil }
func (*opsAgentConn) Begin() (driver.Tx, error)                       { return opsAgentTx{}, nil }
func (opsAgentTx) Commit() error                                      { return nil }
func (opsAgentTx) Rollback() error                                    { return nil }

func (c *opsAgentConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "FROM gm_sessions"):
		return &opsAgentRows{cols: []string{"id", "role", "salt"}, values: [][]driver.Value{{"actor-1", c.f.role, "sandbox-fixture-" + c.f.role}}}, nil
	case strings.Contains(q, "FROM gm_access"):
		return &opsAgentRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM schema_migrations"):
		return &opsAgentRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM profiles WHERE game_id=? AND player_id IN"):
		return &opsAgentRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "SELECT name FROM profiles"):
		return &opsAgentRows{cols: []string{"name"}, values: [][]driver.Value{{"PlayerOne"}}}, nil
	case strings.Contains(q, "FROM items"):
		return &opsAgentRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM gm_requests WHERE game_id=? AND id=?"):
		return &opsAgentRows{cols: []string{"creator", "kind", "payload"}}, nil
	case strings.Contains(q, "FROM activities"),
		strings.Contains(q, "FROM saves"),
		strings.Contains(q, "FROM save_history"),
		strings.Contains(q, "FROM activity_progress"),
		strings.Contains(q, "FROM grants"),
		strings.Contains(q, "FROM inventory"),
		strings.Contains(q, "FROM mail"),
		strings.Contains(q, "FROM events"),
		strings.Contains(q, "FROM scores"),
		strings.Contains(q, "FROM replays"):
		return &opsAgentRows{cols: []string{"c1"}}, nil
	case strings.Contains(q, "SELECT COUNT(*) FROM sessions"):
		return &opsAgentRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}

func (c *opsAgentConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "INSERT INTO ops_agent_runs("):
		c.f.runsInserted++
	case strings.HasPrefix(q, "INSERT INTO ops_agent_steps("):
		c.f.steps++
	case strings.HasPrefix(q, "INSERT INTO audits("):
		c.f.audits++
	case strings.HasPrefix(q, "INSERT INTO gm_requests("):
		c.f.requests++
	case strings.HasPrefix(q, "INSERT INTO ops_agent_config("):
		c.f.configWrites++
		if len(args) >= 5 {
			c.f.lastCipherKey, _ = args[3].Value.(string)
			c.f.lastKeyEnc, _ = args[4].Value.(bool)
		}
		c.f.lastPlainKey = ""
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
	return driver.RowsAffected(1), nil
}

func (r *opsAgentRows) Columns() []string { return r.cols }
func (*opsAgentRows) Close() error        { return nil }
func (r *opsAgentRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}

func TestOpsAgentDiagnoseRespectsRBACAndPersistsTrace(t *testing.T) {
	// support role cannot create proposals even when allow_propose=true
	f := &opsAgentFixture{role: "support"}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	mux := http.NewServeMux()
	s.opsAgentRoutes(mux)

	req := httptest.NewRequest("POST", "/gm/games/sky-island/ops-agent/diagnose", strings.NewReader(`{
		"question":"排查玩家奖励未到账并生成补发单",
		"player_id":"p1",
		"allow_propose":true,
		"proposal_item":"wood",
		"proposal_amount":5
	}`))
	req.SetPathValue("game", "sky-island")
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("diagnose status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["mode"] != "deterministic_playbook" || out["real_ai"] != false || out["proposal_request_id"] != "" {
		t.Fatalf("unexpected support diagnose output: %+v", out)
	}
	if f.runsInserted != 1 || f.steps < 2 || f.requests != 0 {
		t.Fatalf("unexpected persistence or proposal mutation for support: %+v", f)
	}

	// admin role with allow_propose=true creates a pending proposal
	fAdmin := &opsAgentFixture{role: "admin"}
	dbAdmin := sql.OpenDB(fAdmin)
	defer dbAdmin.Close()
	sAdmin := New(dbAdmin, true)
	muxAdmin := http.NewServeMux()
	sAdmin.opsAgentRoutes(muxAdmin)

	req2 := httptest.NewRequest("POST", "/gm/games/sky-island/ops-agent/diagnose", strings.NewReader(`{
		"question":"排查玩家奖励未到账并生成补发单",
		"player_id":"p1",
		"allow_propose":true,
		"proposal_item":"wood",
		"proposal_amount":5
	}`))
	req2.SetPathValue("game", "sky-island")
	req2.Header.Set("Authorization", "Bearer tok")
	rec2 := httptest.NewRecorder()
	muxAdmin.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("admin diagnose status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var out2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &out2); err != nil {
		t.Fatal(err)
	}
	if out2["proposal_request_id"] == "" || fAdmin.requests != 1 {
		t.Fatalf("expected proposal created for admin: out=%+v fixture=%+v", out2, fAdmin)
	}
}

func TestOpsAgentRuntimeConfigRoutes(t *testing.T) {
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test-secret-9999" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`))
	}))
	defer mockLLM.Close()

	// Non-admin can read config status but cannot modify or test it.
	fSupport := &opsAgentFixture{role: "support"}
	dbSupport := sql.OpenDB(fSupport)
	defer dbSupport.Close()
	sSupport := New(dbSupport, true)
	muxSupport := http.NewServeMux()
	sSupport.opsAgentRoutes(muxSupport)

	getReq := httptest.NewRequest("GET", "/gm/system/ops-agent/config", nil)
	getReq.Header.Set("Authorization", "Bearer tok")
	getRec := httptest.NewRecorder()
	muxSupport.ServeHTTP(getRec, getReq)
	if getRec.Code != 200 {
		t.Fatalf("support GET config status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	putSupport := httptest.NewRequest("PUT", "/gm/system/ops-agent/config", strings.NewReader(`{"url":"https://api.example.com/v1","model":"gpt-4o-mini"}`))
	putSupport.Header.Set("Authorization", "Bearer tok")
	putSupportRec := httptest.NewRecorder()
	muxSupport.ServeHTTP(putSupportRec, putSupport)
	if putSupportRec.Code != 403 {
		t.Fatalf("expected support PUT config 403, got %d", putSupportRec.Code)
	}

	// Admin configures custom OpenAI-compatible URL, key is masked and never echoed in plaintext.
	fAdmin := &opsAgentFixture{role: "admin"}
	dbAdmin := sql.OpenDB(fAdmin)
	defer dbAdmin.Close()
	sAdmin := New(dbAdmin, true)
	muxAdmin := http.NewServeMux()
	sAdmin.opsAgentRoutes(muxAdmin)

	putAdmin := httptest.NewRequest("PUT", "/gm/system/ops-agent/config", strings.NewReader(
		`{"url":"`+mockLLM.URL+`","model":"demo-model","api_key":"sk-test-secret-9999"}`,
	))
	putAdmin.Header.Set("Authorization", "Bearer tok")
	putAdminRec := httptest.NewRecorder()
	muxAdmin.ServeHTTP(putAdminRec, putAdmin)
	if putAdminRec.Code != 200 {
		t.Fatalf("admin PUT config status=%d body=%s", putAdminRec.Code, putAdminRec.Body.String())
	}
	if strings.Contains(putAdminRec.Body.String(), "sk-test-secret-9999") {
		t.Fatalf("plaintext API key leaked in response: %s", putAdminRec.Body.String())
	}
	var cfgOut map[string]any
	if err := json.Unmarshal(putAdminRec.Body.Bytes(), &cfgOut); err != nil {
		t.Fatal(err)
	}
	if cfgOut["configured"] != true || cfgOut["has_api_key"] != true || cfgOut["api_key_masked"] != "sk-****9999" {
		t.Fatalf("unexpected config response: %+v", cfgOut)
	}
	if !strings.HasSuffix(cfgOut["url"].(string), "/v1/chat/completions") {
		t.Fatalf("expected bare base URL normalized to /v1/chat/completions, got %v", cfgOut["url"])
	}

	// Test endpoint connectivity using saved secret (omitting api_key in test payload).
	testReq := httptest.NewRequest("POST", "/gm/system/ops-agent/test", strings.NewReader(`{}`))
	testReq.Header.Set("Authorization", "Bearer tok")
	testRec := httptest.NewRecorder()
	muxAdmin.ServeHTTP(testRec, testReq)
	if testRec.Code != 200 {
		t.Fatalf("admin POST test status=%d body=%s", testRec.Code, testRec.Body.String())
	}
	var testOut map[string]any
	if err := json.Unmarshal(testRec.Body.Bytes(), &testOut); err != nil {
		t.Fatal(err)
	}
	if testOut["ok"] != true || testOut["reply_preview"] != "OK" {
		t.Fatalf("expected test connection ok=true, got %+v", testOut)
	}

	// Clearing URL resets back to sandbox playbook mode.
	clearReq := httptest.NewRequest("PUT", "/gm/system/ops-agent/config", strings.NewReader(`{"url":"","clear_api_key":true}`))
	clearReq.Header.Set("Authorization", "Bearer tok")
	clearRec := httptest.NewRecorder()
	muxAdmin.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != 200 {
		t.Fatalf("admin clear config status=%d", clearRec.Code)
	}
	var cleared map[string]any
	_ = json.Unmarshal(clearRec.Body.Bytes(), &cleared)
	if cleared["configured"] != false || cleared["has_api_key"] != false {
		t.Fatalf("expected cleared config, got %+v", cleared)
	}

	// Anthropic Claude protocol: bare URL normalizes to /v1/messages and sends x-api-key + anthropic-version.
	mockClaude := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-ant-live-4321" || r.Header.Get("anthropic-version") != "2023-06-01" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-20250514"},{"id":"claude-3-5-haiku-20241022"}]}`))
			return
		}
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"OK"}],"usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer mockClaude.Close()

	putClaude := httptest.NewRequest("PUT", "/gm/system/ops-agent/config", strings.NewReader(
		`{"protocol":"anthropic","url":"`+mockClaude.URL+`","model":"claude-sonnet-4-20250514","api_key":"sk-ant-live-4321"}`,
	))
	putClaude.Header.Set("Authorization", "Bearer tok")
	putClaudeRec := httptest.NewRecorder()
	muxAdmin.ServeHTTP(putClaudeRec, putClaude)
	if putClaudeRec.Code != 200 {
		t.Fatalf("admin PUT claude config status=%d body=%s", putClaudeRec.Code, putClaudeRec.Body.String())
	}
	var claudeCfg map[string]any
	_ = json.Unmarshal(putClaudeRec.Body.Bytes(), &claudeCfg)
	if claudeCfg["protocol"] != "anthropic" || !strings.HasSuffix(claudeCfg["url"].(string), "/v1/messages") {
		t.Fatalf("unexpected claude config response: %+v", claudeCfg)
	}

	modelsReq := httptest.NewRequest("POST", "/gm/system/ops-agent/models", strings.NewReader(`{}`))
	modelsReq.Header.Set("Authorization", "Bearer tok")
	modelsRec := httptest.NewRecorder()
	muxAdmin.ServeHTTP(modelsRec, modelsReq)
	if modelsRec.Code != 200 {
		t.Fatalf("admin POST models status=%d body=%s", modelsRec.Code, modelsRec.Body.String())
	}
	var modelsOut struct {
		OK     bool     `json:"ok"`
		Models []string `json:"models"`
		Count  int      `json:"count"`
	}
	_ = json.Unmarshal(modelsRec.Body.Bytes(), &modelsOut)
	if !modelsOut.OK || modelsOut.Count != 2 || modelsOut.Models[0] != "claude-3-5-haiku-20241022" {
		t.Fatalf("unexpected models response: %+v", modelsOut)
	}

	testClaude := httptest.NewRequest("POST", "/gm/system/ops-agent/test", strings.NewReader(`{}`))
	testClaude.Header.Set("Authorization", "Bearer tok")
	testClaudeRec := httptest.NewRecorder()
	muxAdmin.ServeHTTP(testClaudeRec, testClaude)
	var claudeTestOut map[string]any
	_ = json.Unmarshal(testClaudeRec.Body.Bytes(), &claudeTestOut)
	if claudeTestOut["ok"] != true || claudeTestOut["reply_preview"] != "OK" {
		t.Fatalf("expected claude test ok=true, got %+v", claudeTestOut)
	}
}

func TestOpsAgentConfigEncryptionAndAudit(t *testing.T) {
	// Without secret key: never persists plaintext key, writes audit log, key_persisted=false.
	fNoKey := &opsAgentFixture{role: "admin"}
	dbNoKey := sql.OpenDB(fNoKey)
	defer dbNoKey.Close()
	sNoKey := New(dbNoKey, true)
	muxNoKey := http.NewServeMux()
	sNoKey.opsAgentRoutes(muxNoKey)

	req := httptest.NewRequest("PUT", "/gm/system/ops-agent/config", strings.NewReader(`{"url":"https://api.example.com/v1","model":"gpt-4o-mini","api_key":"sk-ephemeral-123456"}`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	muxNoKey.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT config status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["key_persisted"] != false || out["has_api_key"] != true {
		t.Fatalf("expected key_persisted=false when master key unset, got %+v", out)
	}
	if fNoKey.configWrites != 1 || fNoKey.audits != 1 || fNoKey.lastKeyEnc || fNoKey.lastCipherKey != "" {
		t.Fatalf("unexpected persistence state without master key: %+v", fNoKey)
	}

	// With 32-byte secret key: encrypts with AES-GCM, round-trips decrypt, writes audit log.
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	fEnc := &opsAgentFixture{role: "admin"}
	dbEnc := sql.OpenDB(fEnc)
	defer dbEnc.Close()
	sEnc := New(dbEnc, true)
	sEnc.OpsAgentSecretKey = masterKey
	muxEnc := http.NewServeMux()
	sEnc.opsAgentRoutes(muxEnc)

	req2 := httptest.NewRequest("PUT", "/gm/system/ops-agent/config", strings.NewReader(`{"url":"https://api.example.com/v1","model":"gpt-4o-mini","api_key":"sk-encrypted-secret-9876"}`))
	req2.Header.Set("Authorization", "Bearer tok")
	rec2 := httptest.NewRecorder()
	muxEnc.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("PUT config encrypted status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var out2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &out2)
	if out2["key_persisted"] != true || out2["has_api_key"] != true {
		t.Fatalf("expected key_persisted=true with master key, got %+v", out2)
	}
	if fEnc.configWrites != 1 || fEnc.audits != 1 || !fEnc.lastKeyEnc || !strings.HasPrefix(fEnc.lastCipherKey, "v1:") {
		t.Fatalf("expected encrypted ciphertext in DB, got %+v", fEnc)
	}
	if strings.Contains(fEnc.lastCipherKey, "sk-encrypted-secret-9876") {
		t.Fatalf("ciphertext leaked plaintext secret")
	}
	plain, err := decryptOpsSecret(masterKey, fEnc.lastCipherKey)
	if err != nil || plain != "sk-encrypted-secret-9876" {
		t.Fatalf("decryptOpsSecret failed: plain=%q err=%v", plain, err)
	}
}
