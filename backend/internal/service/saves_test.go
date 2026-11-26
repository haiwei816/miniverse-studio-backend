package service

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"sync"
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

func TestSavePoolConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			payload := strings.Repeat(`{"slot":1,"gold":999},`, 50)
			for i := 0; i < 50; i++ {
				packed := pack(payload)
				got, err := unpack(packed)
				if err != nil || got != payload {
					t.Errorf("round-trip mismatch on g=%d i=%d: err=%v", id, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func BenchmarkSavePackUnpackPooled(b *testing.B) {
	payload := strings.Repeat(`{"building":"mill","level":12,"items":[1,2,3,4,5]},`, 40)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		compressed := pack(payload)
		out, err := unpack(compressed)
		if err != nil || len(out) != len(payload) {
			b.Fatal(err)
		}
	}
}

func BenchmarkSavePackUnpackUnpooled(b *testing.B) {
	payload := strings.Repeat(`{"building":"mill","level":12,"items":[1,2,3,4,5]},`, 40)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(payload))
		_ = zw.Close()
		zr, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
		if err != nil {
			b.Fatal(err)
		}
		raw, err := io.ReadAll(zr)
		_ = zr.Close()
		if err != nil || len(raw) != len(payload) {
			b.Fatal(err)
		}
	}
}
