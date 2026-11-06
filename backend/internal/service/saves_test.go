package service

import (
	"strings"
	"testing"
)

func TestBoundedCompression(t *testing.T) {
	v := `{"progress":42}`
	out, e := unpack(pack(v))
	if e != nil || out != v {
		t.Fatal("compression round trip")
	}
	if _, e = unpack(pack(strings.Repeat("x", (1<<20)+1))); e == nil {
		t.Fatal("decompression bomb accepted")
	}
	if _, e = unpack([]byte("invalid")); e == nil {
		t.Fatal("invalid gzip accepted")
	}
}
