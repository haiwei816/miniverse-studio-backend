package telemetry

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRecorderWindowMetricsAndBounds(t *testing.T) {
	r := New()
	base := time.Unix(1700000000, 0)
	now := base
	r.startedAt = base
	r.nowFn = func() time.Time { return now }

	// Old sample outside 5m window must not pollute current error rate or percentiles.
	r.RecordRequest("GET /old", "GET", 500, 999*time.Millisecond)
	now = base.Add(6 * time.Minute)

	for i := 1; i <= 20; i++ {
		status := 200
		if i == 20 {
			status = 503
		}
		r.RecordRequest("GET /v1/games/{game}/profile", "GET", status, time.Duration(i*10)*time.Millisecond)
	}
	r.RecordDeadlockRetry()
	r.RecordRateLimit()
	r.RecordAuthRateLimit()
	r.RecordRuleCore(false, 25*time.Millisecond, "rulecore_unavailable")

	snap := r.Snapshot(nil, 21, "rulecore:50051")
	if snap.TotalRequests != 21 || snap.Status2xx != 19 || snap.Status5xx != 2 {
		t.Fatalf("unexpected totals: %+v", snap)
	}
	if snap.Window5mRequests != 20 || snap.Window5mErrors != 1 || snap.Window5mServerErrs != 1 {
		t.Fatalf("unexpected 5m window counts: %+v", snap)
	}
	if snap.LatencyP50MS != 100 || snap.LatencyP95MS < 190 {
		t.Fatalf("unexpected percentiles: p50=%d p95=%d", snap.LatencyP50MS, snap.LatencyP95MS)
	}
	if snap.DeadlockRetries != 1 || snap.RateLimited != 1 || snap.AuthRateLimited != 1 {
		t.Fatalf("unexpected stability counters: %+v", snap)
	}
	if snap.RuleCoreHealthy == nil || *snap.RuleCoreHealthy || snap.RuleCoreErrors != 1 || snap.RuleCoreLastError != "rulecore_unavailable" {
		t.Fatalf("unexpected rulecore status: %+v", snap)
	}

	prom := PrometheusText(snap)
	for _, metric := range []string{
		"miniverse_http_requests_total 21",
		"miniverse_http_errors_total 2",
		"miniverse_http_latency_p95_ms ",
		"miniverse_deadlock_retries_total 1",
		"miniverse_rate_limited_total 2",
		"miniverse_rulecore_errors_total 1",
		"miniverse_schema_version 21",
	} {
		if !strings.Contains(prom, metric) {
			t.Fatalf("prometheus output missing %q:\n%s", metric, prom)
		}
	}
}

func TestRecorderRouteCapAndEventRingFiltering(t *testing.T) {
	r := New()
	for i := 0; i < maxRouteKeys+25; i++ {
		r.RecordRequest(fmt.Sprintf("GET /r/%d", i), "GET", 200, time.Millisecond)
	}
	if len(r.routes) > maxRouteKeys+1 {
		t.Fatalf("route map exceeded bound: %d", len(r.routes))
	}
	if r.routes["other"] == nil || r.routes["other"].Total != 25 {
		t.Fatalf("overflow routes not collapsed into 'other': %+v", r.routes["other"])
	}

	for i := 0; i < maxEvents+15; i++ {
		game := "sky-island"
		if i%2 == 0 {
			game = "card-game"
		}
		r.RecordEvent(Event{
			RequestID:  fmt.Sprintf("req-%d", i),
			GameID:     game,
			PlayerID:   fmt.Sprintf("p-%d", i%3),
			Category:   "conflict",
			StatusCode: 409,
			ErrorCode:  "revision_conflict",
		})
	}
	skyEvents := r.RecentEvents("sky-island", "", "", "revision_conflict", 10)
	if len(skyEvents) != 10 {
		t.Fatalf("expected 10 filtered events, got %d", len(skyEvents))
	}
	if skyEvents[0].RequestID != fmt.Sprintf("req-%d", maxEvents+14-1) {
		t.Fatalf("expected newest-first order, got %s", skyEvents[0].RequestID)
	}
	byReq := r.RecentEvents("", "", fmt.Sprintf("req-%d", maxEvents+10), "", 10)
	if len(byReq) != 1 || byReq[0].ErrorCode != "revision_conflict" {
		t.Fatalf("request filter failed: %+v", byReq)
	}
	if evicted := r.RecentEvents("", "", "req-0", "", 10); len(evicted) != 0 {
		t.Fatalf("expected oldest event evicted, got %+v", evicted)
	}
}

func TestRecorderHighQPSWindowNoTruncation(t *testing.T) {
	r := New()
	base := time.Unix(1700001000, 0)
	now := base
	r.startedAt = base
	r.nowFn = func() time.Time { return now }

	for i := 0; i < 5000; i++ {
		if i%100 == 0 {
			now = now.Add(time.Second)
		}
		status := 200
		if i%20 == 0 {
			status = 500
		}
		r.RecordRequest("GET /v1/games/{game}/profile", "GET", status, time.Duration((i%50)+1)*time.Millisecond)
	}
	snap := r.Snapshot(nil, 24, "rulecore:50051")
	if snap.Window5mRequests != 5000 || snap.Window5mErrors != 250 || snap.Window5mServerErrs != 250 {
		t.Fatalf("5m window truncated or inaccurate: %+v", snap)
	}
	if snap.Window5mErrorRate < 0.049 || snap.Window5mErrorRate > 0.051 {
		t.Fatalf("unexpected error rate: %f", snap.Window5mErrorRate)
	}
	if snap.LatencyP99MS < snap.LatencyP95MS {
		t.Fatalf("expected p99 >= p95, got p95=%d p99=%d", snap.LatencyP95MS, snap.LatencyP99MS)
	}
}
