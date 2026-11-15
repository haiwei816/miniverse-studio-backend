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

// Drive the real listing handlers with a mutable draft, so an editor can replace
// it after GET but before a stale submit/publish request reaches the server.
type listingReviewFixture struct {
	raw            []byte
	author         string
	submitted      bool
	listingQueries int
	mutations      int
	commits        int
	inTransaction  bool
}
type listingReviewConn struct{ f *listingReviewFixture }
type listingReviewDriver struct{}
type listingReviewTx struct{ f *listingReviewFixture }
type listingReviewRows struct{ values []driver.Value }

func (f *listingReviewFixture) Connect(context.Context) (driver.Conn, error) {
	return &listingReviewConn{f}, nil
}
func (*listingReviewFixture) Driver() driver.Driver { return listingReviewDriver{} }
func (listingReviewDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}
func (*listingReviewConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (*listingReviewConn) Close() error { return nil }
func (c *listingReviewConn) Begin() (driver.Tx, error) {
	c.f.inTransaction = true
	return &listingReviewTx{c.f}, nil
}
func (tx *listingReviewTx) Commit() error {
	tx.f.inTransaction = false
	tx.f.commits++
	return nil
}
func (tx *listingReviewTx) Rollback() error { tx.f.inTransaction = false; return nil }
func (c *listingReviewConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "FROM gm_sessions"):
		return &listingReviewRows{[]driver.Value{"reviewer", "admin", "test-salt"}}, nil
	case strings.Contains(q, "FROM gm_access"):
		return &listingReviewRows{[]driver.Value{int64(1)}}, nil
	case strings.Contains(q, "FROM game_listings l JOIN games"):
		c.f.listingQueries++
		return &listingReviewRows{[]driver.Value{"sky-island", "Published", "Old summary", "", "探索", "sky", "✦", "1.0", "/games/sky-island/", "context", c.f.raw, c.f.author, c.f.submitted, true}}, nil
	case strings.Contains(q, "FROM game_listings") && strings.Contains(q, "FOR UPDATE") && c.f.inTransaction:
		return &listingReviewRows{[]driver.Value{c.f.raw, c.f.author, c.f.submitted}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (c *listingReviewConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO audits") {
		return driver.RowsAffected(1), nil
	}
	if c.f.inTransaction && (strings.HasPrefix(q, "UPDATE game_listings") || strings.HasPrefix(q, "UPDATE games")) {
		c.f.mutations++
		return driver.RowsAffected(1), nil
	}
	return nil, errors.New("unexpected write: " + q)
}
func (r *listingReviewRows) Columns() []string { return make([]string, len(r.values)) }
func (*listingReviewRows) Close() error        { return nil }
func (r *listingReviewRows) Next(dest []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(dest, r.values)
	r.values = nil
	return nil
}

func TestListingActionsBindTheDraftActuallySeen(t *testing.T) {
	for _, action := range []string{"submit", "publish"} {
		t.Run(action, func(t *testing.T) {
			f := &listingReviewFixture{author: "writer", submitted: action == "publish"}
			g := platformGame{GameID: "sky-island", Name: "Demo", Summary: "Reviewed content", Category: "探索", Accent: "sky", Icon: "✦", Version: "1.0", EntryURL: "/games/sky-island/", CompanionLevel: "context"}
			f.raw, _ = json.Marshal(g)
			db := sql.OpenDB(f)
			defer db.Close()
			mux := http.NewServeMux()
			New(db, true).developerListingRoutes(mux)
			call := func(method, suffix, body string, status int) map[string]any {
				t.Helper()
				r := httptest.NewRequest(method, "/gm/games/sky-island/listing"+suffix, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer reviewer-session")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != status {
					t.Fatalf("%s %s status=%d body=%s", method, suffix, w.Code, w.Body)
				}
				var value map[string]any
				if e := json.Unmarshal(w.Body.Bytes(), &value); e != nil {
					t.Fatal(e)
				}
				return value
			}
			seen := call("GET", "", "", 200)
			if f.listingQueries != 1 || seen["draft_hash"] == "" || seen["draft_hash"] == nil {
				t.Fatalf("listing must expose one consistent snapshot and hash: %+v", seen)
			}
			g.Summary = "Replaced after the reviewer opened the page"
			f.raw, _ = json.Marshal(g)
			staleBody, _ := json.Marshal(map[string]any{"draft_hash": seen["draft_hash"]})
			failure := call("POST", "/"+action, string(staleBody), 409)
			if failure["error"] != "listing_draft_changed" || f.mutations != 0 || f.commits != 0 {
				t.Fatalf("stale review changed the listing: %+v fixture=%+v", failure, f)
			}
			call("POST", "/"+action, `{}`, 400)
			if f.mutations != 0 || f.commits != 0 {
				t.Fatal("missing version changed the listing")
			}
			latest := call("GET", "", "", 200)
			currentBody, _ := json.Marshal(map[string]any{"draft_hash": latest["draft_hash"]})
			if action == "publish" {
				f.author = "reviewer"
				call("POST", "/publish", string(currentBody), 403)
				if f.mutations != 0 || f.commits != 0 {
					t.Fatal("author published their own draft")
				}
				f.author = "writer"
			}
			call("POST", "/"+action, string(currentBody), 200)
			if f.mutations == 0 || f.commits != 1 {
				t.Fatal("fresh review did not commit")
			}
		})
	}
}
