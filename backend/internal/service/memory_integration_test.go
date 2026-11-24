package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"miniverse/backend/internal/database"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"
)

type memorySample struct {
	Batch       int    `json:"batch"`
	HeapBytes   uint64 `json:"heap_bytes_after_gc"`
	HeapObjects uint64 `json:"heap_objects_after_gc"`
	Goroutines  int    `json:"goroutines"`
	OpenDB      int    `json:"db_open"`
	InUseDB     int    `json:"db_in_use"`
	FDs         int    `json:"file_descriptors"`
}

func TestMemorySteadyState(t *testing.T) {
	dsn := os.Getenv("MINIVERSE_MEMORY_DSN")
	if dsn == "" {
		t.Skip("requires explicit local integration database")
	}
	db, e := database.Open(dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	id, token := random(), random()
	if _, e = db.Exec("INSERT INTO accounts(id,secret_hash) VALUES(?,?)", id, hash(random())); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO sessions(token_hash,player_id,expires_at) VALUES(?,?,?)", hash(token), id, time.Now().Add(time.Hour).Unix()); e != nil {
		t.Fatal(e)
	}
	defer func() {
		for _, table := range []string{"replays", "profiles", "sessions"} {
			db.Exec("DELETE FROM "+table+" WHERE player_id=?", id)
		}
		db.Exec("DELETE FROM accounts WHERE id=?", id)
	}()
	s := New(db, true)
	mux := http.NewServeMux()
	s.Routes(mux)
	request := func(path, body string, index int) {
		r := httptest.NewRequest("GET", path, nil)
		if body != "" {
			r = httptest.NewRequest("POST", path, bytes.NewBufferString(body))
		}
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1000", index%64+1)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("request failed: %d %s", w.Code, w.Body.String())
		}
	}
	replay := `{"version":"card-v1","seed":123,"actions":[2,2,2,2,2,2],"request_id":"memory-probe"}`
	for i := 0; i < 30; i++ {
		request("/v1/games/sky-island/profile", "", i)
		request("/v1/games/card-game/replays/validate", replay, i)
	}
	dir := os.Getenv("MINIVERSE_MEMORY_OUTPUT")
	if dir == "" {
		dir = t.TempDir()
	}
	os.MkdirAll(dir, 0755)
	snapshot := func(batch int) memorySample {
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		stats := db.Stats()
		fds, _ := os.ReadDir("/proc/self/fd")
		return memorySample{batch, m.HeapAlloc, m.HeapObjects, runtime.NumGoroutine(), stats.OpenConnections, stats.InUse, len(fds)}
	}
	baseline := snapshot(0)
	samples := []memorySample{baseline}
	writeHeap := func(name string) {
		f, e := os.Create(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = pprof.WriteHeapProfile(f); e != nil {
			t.Fatal(e)
		}
	}
	writeHeap("memory-before.pprof")
	for batch := 1; batch <= 5; batch++ {
		for i := 0; i < 500; i++ {
			request("/v1/games/sky-island/profile", "", i)
			if i < 50 {
				request("/v1/games/card-game/replays/validate", replay, i)
			}
		}
		sample := snapshot(batch)
		samples = append(samples, sample)
		if sample.InUseDB != 0 {
			t.Fatal("database connection retained")
		}
	}
	writeHeap("memory-after.pprof")
	last := samples[len(samples)-1]
	raw, _ := json.MarshalIndent(map[string]any{"scope": "short local integration probe, not a long-duration leak proof", "profile_reads": 2530, "grpc_calls": 280, "samples": samples}, "", "  ")
	if e = os.WriteFile(filepath.Join(dir, "memory-results.json"), raw, 0644); e != nil {
		t.Fatal(e)
	}
	t.Log(string(raw))
	if last.HeapBytes > baseline.HeapBytes+4*1024*1024 {
		t.Error("retained heap growth exceeds short-run bound")
	}
	if last.Goroutines > baseline.Goroutines+12 {
		t.Error("goroutines did not return near baseline")
	}
	if last.FDs > baseline.FDs+12 {
		t.Error("file descriptors did not return near baseline")
	}
}
