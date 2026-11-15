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

func TestPlatformListingOnlyAllowsPackagedSameOriginGames(t *testing.T) {
	g := platformGame{Name: "Demo", Category: "探索", Version: "1.0", EntryURL: "/games/demo/", Accent: "sky", Icon: "✦", CompanionLevel: "context"}
	if err := validatePlatformGame(g); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"https://evil.invalid/game/", "//evil.invalid/game/", "/games/../admin/", "/games/demo/?token=secret", "/games/demo/#fragment", "/games/demo/%2e%2e/", "/games/demo//"} {
		g.EntryURL = entry
		if validatePlatformGame(g) == nil {
			t.Fatalf("accepted unsafe entry %q", entry)
		}
	}
}

func TestCatalogPaginationValidation(t *testing.T) {
	for _, q := range []string{"limit=0", "limit=101", "limit=abc", "cursor=" + strings.Repeat("a", 65)} {
		if _, _, e := platformCatalogPage(httptest.NewRequest("GET", "/?"+q, nil)); e == nil {
			t.Fatalf("invalid pagination accepted: %s", q)
		}
	}
	if cursor, limit, e := platformCatalogPage(httptest.NewRequest("GET", "/?cursor=sky-island&limit=1", nil)); e != nil || cursor != "sky-island" || limit != 1 {
		t.Fatalf("valid pagination rejected: %s %d %v", cursor, limit, e)
	}
	if _, limit, e := platformCatalogPage(httptest.NewRequest("GET", "/", nil)); e != nil || limit != 24 {
		t.Fatalf("wrong default page size %d %v", limit, e)
	}
}

func TestListingTextLimitsCountUnicodeCharacters(t *testing.T) {
	g := platformGame{Name: "测试", Summary: strings.Repeat("界", 256), Description: strings.Repeat("界", 12000), Category: strings.Repeat("类", 32), Version: "1.0", EntryURL: "/games/demo/", Accent: "sky", Icon: "✦", CompanionLevel: "context"}
	if e := validatePlatformGame(g); e != nil {
		t.Fatalf("valid unicode listing rejected: %v", e)
	}
	g.Summary += "界"
	if e := validatePlatformGame(g); e == nil {
		t.Fatal("overlong unicode summary accepted")
	}
}

func TestListingEntryURLMatchesDatabaseLength(t *testing.T) {
	g := platformGame{Name: "Demo", Category: "探索", Version: "1.0", Accent: "sky", Icon: "✦", CompanionLevel: "context"}
	g.EntryURL = "/games/" + strings.Repeat("a", 248) + "/"
	if e := validatePlatformGame(g); e != nil {
		t.Fatalf("256-byte entry rejected: %v", e)
	}
	g.EntryURL = "/games/" + strings.Repeat("a", 249) + "/"
	if e := validatePlatformGame(g); e == nil || e.Error() != "invalid_listing" {
		t.Fatalf("257-byte entry accepted: %v", e)
	}
}

func TestListingDraftHashIgnoresDatabaseJSONFormatting(t *testing.T) {
	a, e := listingDraftHash([]byte(`{"name":"Demo","summary":"资料"}`))
	if e != nil {
		t.Fatal(e)
	}
	b, e := listingDraftHash([]byte(`{ "summary": "资料", "name": "Demo" }`))
	if e != nil || a != b || len(a) != 64 {
		t.Fatalf("formatting changed draft identity: %q %q %v", a, b, e)
	}
	c, e := listingDraftHash([]byte(`{"name":"Demo","summary":"新资料"}`))
	if e != nil || a == c {
		t.Fatalf("content change retained old identity: %v", e)
	}
	if empty, e := listingDraftHash(nil); e != nil || empty != "" {
		t.Fatalf("empty draft has identity: %q %v", empty, e)
	}
}

type gameScopeFixture struct {
	parentActive bool
	queries      []string
}
type gameScopeConn struct{ f *gameScopeFixture }
type gameScopeRows struct{ values []driver.Value }

func (f *gameScopeFixture) Connect(context.Context) (driver.Conn, error) {
	return &gameScopeConn{f}, nil
}
func (*gameScopeFixture) Driver() driver.Driver            { return sessionDriver{} }
func (*gameScopeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (*gameScopeConn) Close() error                        { return nil }
func (*gameScopeConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
func (c *gameScopeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.f.queries = append(c.f.queries, q)
	if !strings.Contains(q, "JOIN sessions s ON s.token_hash=gs.parent_hash") {
		return nil, errors.New("delegation must validate parent session")
	}
	if !strings.Contains(q, "cc.epoch=gs.context_epoch") {
		return nil, errors.New("delegation must validate current context epoch")
	}
	rows := &gameScopeRows{}
	if c.f.parentActive {
		rows.values = []driver.Value{"player-1", "sky-island"}
	}
	return rows, nil
}
func (*gameScopeRows) Columns() []string { return []string{"player_id", "game_id"} }
func (*gameScopeRows) Close() error      { return nil }
func (r *gameScopeRows) Next(dest []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(dest, r.values)
	r.values = nil
	return nil
}

func TestDelegatedGameSessionHonorsScopeAndParentRevocation(t *testing.T) {
	fixture := &gameScopeFixture{parentActive: true}
	db := sql.OpenDB(fixture)
	defer db.Close()
	s := New(db, true)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer g_token")
	if p, e := s.gameActor(r, "sky-island"); e != nil || p != "player-1" {
		t.Fatalf("valid delegation: %s %v", p, e)
	}
	if _, e := s.gameActor(r, "card-game"); e == nil || e.Error() != "game_scope_forbidden" {
		t.Fatalf("cross-game delegation accepted: %v", e)
	}
	fixture.parentActive = false
	if _, e := s.gameActor(r, "sky-island"); e == nil || e.Error() != "unauthorized" {
		t.Fatalf("revoked parent accepted: %v", e)
	}
}
