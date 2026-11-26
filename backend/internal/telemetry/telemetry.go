// Package telemetry collects bounded in-process runtime metrics and anomaly events.
package telemetry

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxSamples    = 2048
	maxEvents     = 200
	maxRouteKeys  = 256
	windowSeconds = 300
)

type sample struct {
	ts        int64
	status    int
	latencyMS int64
}

type secBucket struct {
	sec        int64
	total      int
	errs       int
	serverErrs int
}

// Event captures a diagnostic/anomaly occurrence in memory alongside ops_events persistence.
type Event struct {
	ID         int64  `json:"id,omitempty"`
	RequestID  string `json:"request_id"`
	GameID     string `json:"game_id"`
	PlayerID   string `json:"player_id"`
	Category   string `json:"category"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	ErrorCode  string `json:"error_code"`
	LatencyMS  int64  `json:"latency_ms"`
	Detail     any    `json:"detail,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

// DBPoolStats mirrors sql.DBStats with JSON tags for telemetry output.
type DBPoolStats struct {
	MaxOpen         int   `json:"max_open"`
	Open            int   `json:"open"`
	InUse           int   `json:"in_use"`
	Idle            int   `json:"idle"`
	WaitCount       int64 `json:"wait_count"`
	WaitDurationMS  int64 `json:"wait_duration_ms"`
}

// RouteCounter summarizes traffic for a single registered route pattern.
type RouteCounter struct {
	Route  string `json:"route"`
	Total  int64  `json:"total"`
	Errors int64  `json:"errors"`
}

// Snapshot is a point-in-time stability view of the backend process.
type Snapshot struct {
	GeneratedAt         int64          `json:"generated_at"`
	UptimeSeconds       int64          `json:"uptime_seconds"`
	SchemaVersion       int            `json:"schema_version"`
	TotalRequests       int64          `json:"total_requests"`
	Status2xx           int64          `json:"status_2xx"`
	Status4xx           int64          `json:"status_4xx"`
	Status5xx           int64          `json:"status_5xx"`
	Window5mRequests    int            `json:"window_5m_requests"`
	Window5mErrors      int            `json:"window_5m_errors"`
	Window5mServerErrs  int            `json:"window_5m_server_errors"`
	Window5mErrorRate   float64        `json:"window_5m_error_rate"`
	LatencyP50MS        int64          `json:"latency_p50_ms"`
	LatencyP95MS        int64          `json:"latency_p95_ms"`
	LatencyP99MS        int64          `json:"latency_p99_ms"`
	DeadlockRetries      int64          `json:"deadlock_retries"`
	RateLimited          int64          `json:"rate_limited"`
	AuthRateLimited      int64          `json:"auth_rate_limited"`
	OpsEventsDropped     int64          `json:"ops_events_dropped"`
	CacheHits            int64          `json:"cache_hits"`
	CacheMisses          int64          `json:"cache_misses"`
	AIGatewayBlocked     int64          `json:"ai_gateway_blocked"`
	RuleCoreCircuitOpens int64          `json:"rulecore_circuit_opens"`
	OutboxDispatched     int64          `json:"outbox_dispatched"`
	OutboxDLQ            int64          `json:"outbox_dlq"`
	RuleCoreAddr         string         `json:"rulecore_addr"`
	RuleCoreCalls       int64          `json:"rulecore_calls"`
	RuleCoreErrors      int64          `json:"rulecore_errors"`
	RuleCoreHealthy     *bool          `json:"rulecore_healthy"`
	RuleCoreLastChecked int64          `json:"rulecore_last_checked"`
	RuleCoreLastLatMS   int64          `json:"rulecore_last_latency_ms"`
	RuleCoreLastError   string         `json:"rulecore_last_error,omitempty"`
	DBPool              DBPoolStats    `json:"db_pool"`
	TopRoutes           []RouteCounter `json:"top_routes"`
	RecentEventsCount   int            `json:"recent_events_count"`
}

// Recorder aggregates bounded runtime traffic, latency, stability counters, and recent events.
type Recorder struct {
	startedAt time.Time
	nowFn     func() time.Time

	totalRequests atomic.Int64
	status2xx     atomic.Int64
	status4xx     atomic.Int64
	status5xx     atomic.Int64

	deadlockRetries      atomic.Int64
	rateLimited          atomic.Int64
	authRateLimited      atomic.Int64
	opsEventsDropped     atomic.Int64
	cacheHits            atomic.Int64
	cacheMisses          atomic.Int64
	aiGatewayBlocked     atomic.Int64
	ruleCoreCircuitOpens atomic.Int64
	outboxDispatched     atomic.Int64
	outboxDLQ            atomic.Int64

	ruleCoreCalls  atomic.Int64
	ruleCoreErrors atomic.Int64

	mu              sync.Mutex
	secBuckets      [windowSeconds]secBucket
	samples         []sample
	samplePos       int
	sampleFull      bool
	events          []Event
	eventPos        int
	eventFull       bool
	routes          map[string]*RouteCounter
	ruleKnown       bool
	ruleHealthy     bool
	ruleLastChecked int64
	ruleLastLatMS   int64
	ruleLastError   string
}

// New initializes a bounded Recorder.
func New() *Recorder {
	now := time.Now()
	return &Recorder{
		startedAt: now,
		nowFn:     time.Now,
		samples:   make([]sample, maxSamples),
		events:    make([]Event, maxEvents),
		routes:    make(map[string]*RouteCounter),
	}
}

func (r *Recorder) now() time.Time {
	if r != nil && r.nowFn != nil {
		return r.nowFn()
	}
	return time.Now()
}

// RecordRequest tracks HTTP request counts, route breakdown, and bounded window latency samples.
func (r *Recorder) RecordRequest(route, method string, status int, latency time.Duration) {
	if r == nil {
		return
	}
	r.totalRequests.Add(1)
	switch {
	case status >= 500:
		r.status5xx.Add(1)
	case status >= 400:
		r.status4xx.Add(1)
	default:
		r.status2xx.Add(1)
	}

	ts := r.now().Unix()
	ms := latency.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	key := route
	if key == "" {
		key = method + " /unknown"
	}

	r.mu.Lock()
	bIdx := int(ts % windowSeconds)
	if bIdx < 0 {
		bIdx += windowSeconds
	}
	if r.secBuckets[bIdx].sec != ts {
		r.secBuckets[bIdx] = secBucket{sec: ts}
	}
	r.secBuckets[bIdx].total++
	if status >= 400 {
		r.secBuckets[bIdx].errs++
	}
	if status >= 500 {
		r.secBuckets[bIdx].serverErrs++
	}

	r.samples[r.samplePos] = sample{ts: ts, status: status, latencyMS: ms}
	r.samplePos = (r.samplePos + 1) % maxSamples
	if r.samplePos == 0 {
		r.sampleFull = true
	}

	rc, ok := r.routes[key]
	if !ok {
		if len(r.routes) >= maxRouteKeys {
			key = "other"
			rc, ok = r.routes[key]
		}
		if !ok {
			rc = &RouteCounter{Route: key}
			r.routes[key] = rc
		}
	}
	rc.Total++
	if status >= 400 {
		rc.Errors++
	}
	r.mu.Unlock()
}

// RecordEvent appends an anomaly/diagnostic event into the bounded in-memory ring buffer.
func (r *Recorder) RecordEvent(ev Event) {
	if r == nil {
		return
	}
	if ev.CreatedAt == 0 {
		ev.CreatedAt = r.now().Unix()
	}
	r.mu.Lock()
	r.events[r.eventPos] = ev
	r.eventPos = (r.eventPos + 1) % maxEvents
	if r.eventPos == 0 {
		r.eventFull = true
	}
	r.mu.Unlock()
}

func (r *Recorder) RecordDeadlockRetry() {
	if r != nil {
		r.deadlockRetries.Add(1)
	}
}

func (r *Recorder) RecordRateLimit() {
	if r != nil {
		r.rateLimited.Add(1)
	}
}

func (r *Recorder) RecordAuthRateLimit() {
	if r != nil {
		r.authRateLimited.Add(1)
	}
}

func (r *Recorder) RecordOpsEventDropped() {
	if r != nil {
		r.opsEventsDropped.Add(1)
	}
}

func (r *Recorder) RecordCacheHit() {
	if r != nil {
		r.cacheHits.Add(1)
	}
}

func (r *Recorder) RecordCacheMiss() {
	if r != nil {
		r.cacheMisses.Add(1)
	}
}

func (r *Recorder) RecordAIGatewayBlocked() {
	if r != nil {
		r.aiGatewayBlocked.Add(1)
	}
}

func (r *Recorder) RecordRuleCoreCircuitOpen() {
	if r != nil {
		r.ruleCoreCircuitOpens.Add(1)
	}
}

func (r *Recorder) RecordOutboxDispatched(n int64) {
	if r != nil && n > 0 {
		r.outboxDispatched.Add(n)
	}
}

func (r *Recorder) RecordOutboxDLQ() {
	if r != nil {
		r.outboxDLQ.Add(1)
	}
}

// RecordRuleCore records the result and latency of a RuleCore gRPC call or health probe.
func (r *Recorder) RecordRuleCore(ok bool, latency time.Duration, errCode string) {
	if r == nil {
		return
	}
	r.ruleCoreCalls.Add(1)
	if !ok {
		r.ruleCoreErrors.Add(1)
	}
	ms := latency.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	r.mu.Lock()
	r.ruleKnown = true
	r.ruleHealthy = ok
	r.ruleLastChecked = r.now().Unix()
	r.ruleLastLatMS = ms
	if ok {
		r.ruleLastError = ""
	} else {
		r.ruleLastError = errCode
	}
	r.mu.Unlock()
}

// RecentEvents returns matching events from newest to oldest.
func (r *Recorder) RecentEvents(gameID, playerID, requestID, errorCode string, limit int) []Event {
	if r == nil {
		return []Event{}
	}
	if limit <= 0 || limit > maxEvents {
		limit = 50
	}
	r.mu.Lock()
	count := r.eventPos
	start := 0
	if r.eventFull {
		count = maxEvents
		start = r.eventPos
	}
	ordered := make([]Event, 0, count)
	for i := 0; i < count; i++ {
		idx := (start + count - 1 - i) % maxEvents
		ordered = append(ordered, r.events[idx])
	}
	r.mu.Unlock()

	out := make([]Event, 0, min(limit, len(ordered)))
	for _, ev := range ordered {
		if gameID != "" && ev.GameID != gameID {
			continue
		}
		if playerID != "" && ev.PlayerID != playerID {
			continue
		}
		if requestID != "" && ev.RequestID != requestID {
			continue
		}
		if errorCode != "" && ev.ErrorCode != errorCode {
			continue
		}
		out = append(out, ev)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Snapshot builds a point-in-time summary combining ring samples, counters, and DB pool stats.
func (r *Recorder) Snapshot(db *sql.DB, schemaVersion int, ruleCoreAddr string) Snapshot {
	now := r.now()
	nowUnix := now.Unix()
	cutoff := nowUnix - windowSeconds

	snap := Snapshot{
		GeneratedAt:     nowUnix,
		UptimeSeconds:   max(int64(0), nowUnix-r.startedAt.Unix()),
		SchemaVersion:   schemaVersion,
		TotalRequests:   r.totalRequests.Load(),
		Status2xx:       r.status2xx.Load(),
		Status4xx:       r.status4xx.Load(),
		Status5xx:       r.status5xx.Load(),
		DeadlockRetries:      r.deadlockRetries.Load(),
		RateLimited:          r.rateLimited.Load(),
		AuthRateLimited:      r.authRateLimited.Load(),
		OpsEventsDropped:     r.opsEventsDropped.Load(),
		CacheHits:            r.cacheHits.Load(),
		CacheMisses:          r.cacheMisses.Load(),
		AIGatewayBlocked:     r.aiGatewayBlocked.Load(),
		RuleCoreCircuitOpens: r.ruleCoreCircuitOpens.Load(),
		OutboxDispatched:     r.outboxDispatched.Load(),
		OutboxDLQ:            r.outboxDLQ.Load(),
		RuleCoreAddr:         ruleCoreAddr,
		RuleCoreCalls:   r.ruleCoreCalls.Load(),
		RuleCoreErrors:  r.ruleCoreErrors.Load(),
	}

	if db != nil {
		st := db.Stats()
		snap.DBPool = DBPoolStats{
			MaxOpen:        st.MaxOpenConnections,
			Open:           st.OpenConnections,
			InUse:          st.InUse,
			Idle:           st.Idle,
			WaitCount:      st.WaitCount,
			WaitDurationMS: st.WaitDuration.Milliseconds(),
		}
	}

	r.mu.Lock()
	for i := 0; i < windowSeconds; i++ {
		b := r.secBuckets[i]
		if b.sec < cutoff || b.sec > nowUnix {
			continue
		}
		snap.Window5mRequests += b.total
		snap.Window5mErrors += b.errs
		snap.Window5mServerErrs += b.serverErrs
	}

	sampleCount := r.samplePos
	if r.sampleFull {
		sampleCount = maxSamples
	}
	latencies := make([]int64, 0, sampleCount)
	for i := 0; i < sampleCount; i++ {
		s := r.samples[i]
		if s.ts < cutoff {
			continue
		}
		latencies = append(latencies, s.latencyMS)
	}

	routes := make([]RouteCounter, 0, len(r.routes))
	for _, rc := range r.routes {
		routes = append(routes, *rc)
	}
	if r.ruleKnown {
		healthy := r.ruleHealthy
		snap.RuleCoreHealthy = &healthy
		snap.RuleCoreLastChecked = r.ruleLastChecked
		snap.RuleCoreLastLatMS = r.ruleLastLatMS
		snap.RuleCoreLastError = r.ruleLastError
	}
	if r.eventFull {
		snap.RecentEventsCount = maxEvents
	} else {
		snap.RecentEventsCount = r.eventPos
	}
	r.mu.Unlock()

	if snap.Window5mRequests > 0 {
		snap.Window5mErrorRate = float64(snap.Window5mErrors) / float64(snap.Window5mRequests)
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		snap.LatencyP50MS = percentile(latencies, 0.50)
		snap.LatencyP95MS = percentile(latencies, 0.95)
		snap.LatencyP99MS = percentile(latencies, 0.99)
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Total != routes[j].Total {
			return routes[i].Total > routes[j].Total
		}
		return routes[i].Route < routes[j].Route
	})
	if len(routes) > 10 {
		routes = routes[:10]
	}
	snap.TopRoutes = routes
	return snap
}

func percentile(sorted []int64, q float64) int64 {
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
	return sorted[idx]
}

// PrometheusText formats a Snapshot in Prometheus text exposition format.
func PrometheusText(snap Snapshot) string {
	var b strings.Builder
	writeGauge := func(name, help string, value any) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", name, help, name, name, value)
	}
	writeCounter := func(name, help string, value int64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, value)
	}

	writeCounter("miniverse_http_requests_total", "Total HTTP requests handled by API", snap.TotalRequests)
	writeCounter("miniverse_http_errors_total", "Total 4xx and 5xx HTTP responses", snap.Status4xx+snap.Status5xx)
	writeGauge("miniverse_http_5m_error_rate", "5-minute sliding window HTTP error ratio", fmt.Sprintf("%.6f", snap.Window5mErrorRate))
	writeGauge("miniverse_http_latency_p50_ms", "5-minute sliding window P50 latency in ms", snap.LatencyP50MS)
	writeGauge("miniverse_http_latency_p95_ms", "5-minute sliding window P95 latency in ms", snap.LatencyP95MS)
	writeGauge("miniverse_http_latency_p99_ms", "5-minute sliding window P99 latency in ms", snap.LatencyP99MS)
	writeGauge("miniverse_db_connections_open", "Current open MySQL connections", snap.DBPool.Open)
	writeGauge("miniverse_db_connections_in_use", "Current in-use MySQL connections", snap.DBPool.InUse)
	writeCounter("miniverse_deadlock_retries_total", "Total MySQL deadlock/lock-timeout retries", snap.DeadlockRetries)
	writeCounter("miniverse_rate_limited_total", "Total rate-limited HTTP requests", snap.RateLimited+snap.AuthRateLimited)
	writeCounter("miniverse_ops_events_dropped_total", "Total anomaly events dropped due to bounded async queue saturation", snap.OpsEventsDropped)
	writeCounter("miniverse_cache_hits_total", "Total L1/L2 cache hits", snap.CacheHits)
	writeCounter("miniverse_cache_misses_total", "Total L1/L2 cache misses", snap.CacheMisses)
	writeCounter("miniverse_ai_gateway_blocked_total", "Total AI requests/tool calls blocked or downgraded by 4-hook gateway", snap.AIGatewayBlocked)
	writeCounter("miniverse_rulecore_circuit_opens_total", "Total RuleCore gRPC fast-fail rejections due to open circuit breaker", snap.RuleCoreCircuitOpens)
	writeCounter("miniverse_outbox_dispatched_total", "Total transactional outbox events dispatched to Kafka", snap.OutboxDispatched)
	writeCounter("miniverse_outbox_dlq_total", "Total outbox/consumer messages routed to DLQ", snap.OutboxDLQ)
	writeCounter("miniverse_rulecore_calls_total", "Total RuleCore gRPC calls and probes", snap.RuleCoreCalls)
	writeCounter("miniverse_rulecore_errors_total", "Total failed RuleCore gRPC calls and probes", snap.RuleCoreErrors)
	writeGauge("miniverse_schema_version", "Expected database migration schema version", snap.SchemaVersion)
	return b.String()
}
