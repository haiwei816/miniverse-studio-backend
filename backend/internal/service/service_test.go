package service

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrictInput(t *testing.T) {
	for _, body := range []string{`{"name":"ok","admin":true}`, `{"name":"ok"} {"name":"trailing"}`, `{`} {
		var dst struct {
			Name string `json:"name"`
		}
		if decode(httptest.NewRequest("POST", "/", strings.NewReader(body)), &dst) == nil {
			t.Fatalf("accepted invalid body: %s", body)
		}
	}
}
func TestRandomCredential(t *testing.T) {
	a, b := random(), random()
	if len(a) != 48 || a == b || hash(a) == a {
		t.Fatal("invalid credential generation")
	}
}
