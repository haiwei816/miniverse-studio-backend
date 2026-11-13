package main

import (
	"reflect"
	"testing"
)

func TestSplitSQL(t *testing.T) {
	sql := `
-- comment with ; semicolon
CREATE TABLE a (id INT);
INSERT INTO a VALUES ('hello;world');
/* block; comment */
INSERT INTO a VALUES ('it''s;ok');
`
	got := splitSQL(sql)
	want := []string{
		"-- comment with ; semicolon\nCREATE TABLE a (id INT)",
		"INSERT INTO a VALUES ('hello;world')",
		"/* block; comment */\nINSERT INTO a VALUES ('it''s;ok')",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
