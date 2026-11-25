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

type batchQueryFixture struct {
	progressQueries int
	profileQueries  int
}

type batchQueryConn struct{ f *batchQueryFixture }
type batchQueryDriver struct{}
type batchQueryRows struct {
	cols   []string
	values [][]driver.Value
	at     int
}

func (f *batchQueryFixture) Connect(context.Context) (driver.Conn, error) {
	return &batchQueryConn{f}, nil
}
func (*batchQueryFixture) Driver() driver.Driver            { return batchQueryDriver{} }
func (batchQueryDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (*batchQueryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (*batchQueryConn) Close() error              { return nil }
func (*batchQueryConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }
func (c *batchQueryConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	today := time.Now().UTC().Format("2006-01-02")
	switch {
	case strings.Contains(q, "FROM sessions"):
		return &batchQueryRows{cols: []string{"player_id"}, values: [][]driver.Value{{"player-1"}}}, nil
	case strings.Contains(q, "FROM games"):
		return &batchQueryRows{cols: []string{"active"}, values: [][]driver.Value{{true}}}, nil
	case strings.Contains(q, "FROM activities"):
		return &batchQueryRows{
			cols: []string{"config"},
			values: [][]driver.Value{
				{[]byte(`{"id":"act-1","version":1,"kind":"cumulative","event_type":"visit","target":5,"starts_at":1,"ends_at":4102444800,"client_allowed":true,"item":"wood","reward_amount":2}`)},
				{[]byte(`{"id":"act-2","version":2,"kind":"daily","event_type":"visit","target":3,"starts_at":1,"ends_at":4102444800,"client_allowed":true,"item":"wood","reward_amount":4}`)},
				{[]byte(`{"id":"act-3","version":1,"kind":"signin","event_type":"visit","target":1,"starts_at":1,"ends_at":4102444800,"client_allowed":true,"item":"wood","reward_amount":1}`)},
			},
		}, nil
	case strings.Contains(q, "FROM activity_progress"):
		c.f.progressQueries++
		return &batchQueryRows{
			cols: []string{"activity_id", "version", "bucket", "progress"},
			values: [][]driver.Value{
				{"act-1", int64(1), "all", int64(5)},
				{"act-2", int64(2), today, int64(2)},
			},
		}, nil
	case strings.Contains(q, "FROM items"):
		return &batchQueryRows{cols: []string{"c"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.Contains(q, "FROM profiles"):
		c.f.profileQueries++
		// Expect game_id plus 5 deduplicated players = 6 args.
		return &batchQueryRows{cols: []string{"c"}, values: [][]driver.Value{{int64(len(args) - 1)}}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (r *batchQueryRows) Columns() []string { return r.cols }
func (*batchQueryRows) Close() error        { return nil }
func (r *batchQueryRows) Next(dest []driver.Value) error {
	if r.at >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.at])
	r.at++
	return nil
}

func TestActivitiesListBatchesProgressQuery(t *testing.T) {
	f := &batchQueryFixture{}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)
	mux := http.NewServeMux()
	s.activitiesRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/games/sky-island/activities", nil)
	req.SetPathValue("game", "sky-island")
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if f.progressQueries != 1 {
		t.Fatalf("expected 1 batched progress query for 3 activities, got %d", f.progressQueries)
	}
	var got []struct {
		Config   Activity `json:"config"`
		Progress int64    `json:"progress"`
		Complete bool     `json:"complete"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !got[0].Complete || got[0].Progress != 5 || got[1].Complete || got[1].Progress != 2 || got[2].Progress != 0 {
		t.Fatalf("unexpected activities output: %+v", got)
	}
}

func TestValidatePayloadRewardBatchesProfileLookup(t *testing.T) {
	f := &batchQueryFixture{}
	db := sql.OpenDB(f)
	defer db.Close()
	s := New(db, true)

	raw, _ := json.Marshal(RewardRequest{
		Players: []string{"p1", "p2", "p3", "p4", "p5", "p2"},
		Item:    "wood",
		Amount:  10,
	})
	req := httptest.NewRequest("POST", "/gm/games/sky-island/requests", nil)
	if err := s.validatePayload(req.Context(), "sky-island", "reward", raw); err != nil {
		t.Fatalf("validatePayload failed: %v", err)
	}
	if f.profileQueries != 1 {
		t.Fatalf("expected 1 batched profiles query, got %d", f.profileQueries)
	}
	emptyPlayerRaw, _ := json.Marshal(RewardRequest{
		Players: []string{"p1", ""},
		Item:    "wood",
		Amount:  10,
	})
	if err := s.validatePayload(req.Context(), "sky-island", "reward", emptyPlayerRaw); err == nil || err.Error() != "unknown_player" {
		t.Fatalf("expected unknown_player for empty player ID, got %v", err)
	}
}
