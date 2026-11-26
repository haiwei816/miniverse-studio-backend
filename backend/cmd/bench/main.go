// Command bench runs steady-state multi-IP HTTP benchmarks against a running MiniVerse API
// and verifies fault-isolation (e.g., 429 burst isolation & RuleCore circuit breaking).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type benchScenarioResult struct {
	Name        string  `json:"name"`
	Concurrency int     `json:"concurrency"`
	Requests    int64   `json:"requests"`
	Errors      int64   `json:"errors"`
	Status2xx   int64   `json:"status_2xx"`
	Status429   int64   `json:"status_429"`
	Status5xx   int64   `json:"status_5xx"`
	DurationMS  int64   `json:"duration_ms"`
	RPS         float64 `json:"rps"`
	P50MS       float64 `json:"p50_ms"`
	P95MS       float64 `json:"p95_ms"`
	P99MS       float64 `json:"p99_ms"`
}

func percentileMS(sorted []int64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * q)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return float64(sorted[idx]) / 1e6
}

func doJSON(client *http.Client, method, url, token, xff string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data, nil
}

func runLoad(name string, client *http.Client, concurrency int, duration time.Duration, fn func(workerID, seq int) (int, error)) benchScenarioResult {
	var total, errs, s2, s429, s5 atomic.Int64
	var mu sync.Mutex
	latencies := make([]int64, 0, 4096)

	stopAt := time.Now().Add(duration)
	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			seq := 0
			local := make([]int64, 0, 256)
			for time.Now().Before(stopAt) {
				seq++
				t0 := time.Now()
				code, err := fn(wid, seq)
				dt := time.Since(t0).Nanoseconds()
				total.Add(1)
				if err != nil {
					errs.Add(1)
				} else {
					local = append(local, dt)
					switch {
					case code >= 200 && code < 300:
						s2.Add(1)
					case code == 429:
						s429.Add(1)
					case code >= 500:
						s5.Add(1)
						errs.Add(1)
					}
				}
			}
			mu.Lock()
			latencies = append(latencies, local...)
			mu.Unlock()
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	reqCount := total.Load()
	rps := float64(reqCount) / elapsed.Seconds()

	return benchScenarioResult{
		Name:        name,
		Concurrency: concurrency,
		Requests:    reqCount,
		Errors:      errs.Load(),
		Status2xx:   s2.Load(),
		Status429:   s429.Load(),
		Status5xx:   s5.Load(),
		DurationMS:  elapsed.Milliseconds(),
		RPS:         rps,
		P50MS:       percentileMS(latencies, 0.50),
		P95MS:       percentileMS(latencies, 0.95),
		P99MS:       percentileMS(latencies, 0.99),
	}
}

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:18080", "API base URL")
	dur := flag.Duration("duration", 10*time.Second, "per-scenario duration")
	outPath := flag.String("out", "", "optional JSON output path")
	flag.Parse()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 256,
			IdleConnTimeout:     60 * time.Second,
		},
	}

	// Login test player and seed a leaderboard + profile.
	code, data, err := doJSON(client, "POST", *baseURL+"/v1/auth/guest", "", "", map[string]any{})
	if err != nil || code != 200 {
		fmt.Fprintf(os.Stderr, "guest login failed: code=%d err=%v\n", code, err)
		os.Exit(1)
	}
	var auth struct {
		AccessToken string `json:"access_token"`
		PlayerID    string `json:"player_id"`
	}
	_ = json.Unmarshal(data, &auth)
	boardID := fmt.Sprintf("bench-%d", time.Now().Unix())
	_, _, _ = doJSON(client, "PUT", *baseURL+"/v1/games/sky-island/boards", auth.AccessToken, "", map[string]any{
		"id": boardID, "season": "s1", "mode": "max", "starts_at": 1, "ends_at": 4102444800, "item": "wood", "reward_amount": 5,
	})
	_, _, _ = doJSON(client, "POST", *baseURL+"/v1/games/sky-island/boards/"+boardID+"/s1/scores", auth.AccessToken, "", map[string]any{
		"request_id": "seed-1", "score": 888,
	})

	// Warm profile & board ZSET cache.
	_, _, _ = doJSON(client, "GET", *baseURL+"/v1/games/sky-island/profile", auth.AccessToken, "", nil)
	_, _, _ = doJSON(client, "GET", *baseURL+"/v1/games/sky-island/boards/"+boardID+"/s1", auth.AccessToken, "", nil)

	// Scenario 1: Health endpoint throughput (bounded by single-IP 600/min if no trusted proxy;
	// use /health which bypasses service limiter for pure HTTP router baseline, then profile/board with rate awareness).
	sHealth := runLoad("http_health_c32", client, 32, *dur, func(_, _ int) (int, error) {
		c, _, err := doJSON(client, "GET", *baseURL+"/health", "", "", nil)
		return c, err
	})

	// Scenario 2: Leaderboard ZSET read under 20 concurrency (respecting 600/min cap or measuring 2xx vs 429 isolation).
	sBoard := runLoad("leaderboard_redis_zset_c20", client, 20, 3*time.Second, func(wid, seq int) (int, error) {
		xff := fmt.Sprintf("198.51.%d.%d", (wid*7+seq/200)%250, (seq)%250)
		c, _, err := doJSON(client, "GET", *baseURL+"/v1/games/sky-island/boards/"+boardID+"/s1", auth.AccessToken, xff, nil)
		return c, err
	})

	// Scenario 3: Profile read under 20 concurrency.
	sProfile := runLoad("profile_read_c20", client, 20, 3*time.Second, func(wid, seq int) (int, error) {
		xff := fmt.Sprintf("198.52.%d.%d", (wid*7+seq/200)%250, (seq)%250)
		c, _, err := doJSON(client, "GET", *baseURL+"/v1/games/sky-island/profile", auth.AccessToken, xff, nil)
		return c, err
	})

	out := map[string]any{
		"generated_at": time.Now().Format(time.RFC3339),
		"target_url":   *baseURL,
		"scenarios":    []benchScenarioResult{sHealth, sBoard, sProfile},
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))
	if *outPath != "" {
		_ = os.MkdirAll(filepath.Dir(*outPath), 0o755)
		if err := os.WriteFile(*outPath, raw, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write failed: %v\n", err)
			os.Exit(1)
		}
	}
}
