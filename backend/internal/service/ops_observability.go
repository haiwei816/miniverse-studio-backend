package service

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"miniverse/backend/internal/database"
	"miniverse/backend/internal/httpserver"
	"miniverse/backend/internal/telemetry"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"strings"
	"sync"
	"time"
)

type opsEventTask struct {
	ev        telemetry.Event
	rawDetail []byte
}

func (s *Service) startOpsWriter() {
	if s.opsCh != nil {
		return
	}
	s.opsCh = make(chan opsEventTask, 2048)
	s.opsStopCh = make(chan struct{})
	s.opsDoneCh = make(chan struct{})
	go s.runOpsEventWriter()
}

func (s *Service) stopOpsWriter() {
	if s.opsCh == nil {
		return
	}
	s.opsStopOnce.Do(func() {
		close(s.opsStopCh)
		<-s.opsDoneCh
	})
}

func (s *Service) runOpsEventWriter() {
	defer close(s.opsDoneCh)
	const maxBatch = 64
	batch := make([]opsEventTask, 0, maxBatch)
	flush := func() {
		if len(batch) == 0 || s.DB == nil {
			batch = batch[:0]
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var b strings.Builder
		b.WriteString("INSERT INTO ops_events(request_id,game_id,player_id,category,method,path,status_code,error_code,latency_ms,detail,created_at) VALUES ")
		args := make([]any, 0, len(batch)*11)
		for i, t := range batch {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString("(?,?,?,?,?,?,?,?,?,?,?)")
			args = append(args, t.ev.RequestID, t.ev.GameID, t.ev.PlayerID, t.ev.Category, t.ev.Method, t.ev.Path, t.ev.StatusCode, t.ev.ErrorCode, t.ev.LatencyMS, t.rawDetail, t.ev.CreatedAt)
		}
		if _, err := s.DB.ExecContext(ctx, b.String(), args...); err != nil {
			slog.Warn("ops_events_batch_insert_failed", "count", len(batch), "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-s.opsStopCh:
			for {
				select {
				case t := <-s.opsCh:
					batch = append(batch, t)
					if len(batch) >= maxBatch {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case first := <-s.opsCh:
			batch = append(batch, first)
			// Coalesce burst arrivals briefly (or immediately flush when only 1 arrived) so single anomalies persist quickly without saturating DB connections during storms.
			drain:
			for len(batch) < maxBatch {
				select {
				case t := <-s.opsCh:
					batch = append(batch, t)
				default:
					break drain
				}
			}
			flush()
		}
	}
}

func classifyOpsCategory(status int, code string) string {
	switch {
	case status == 429 || code == "rate_limited" || code == "auth_rate_limited" || code == "companion_rate_limited":
		return "rate_limit"
	case code == "rulecore_unavailable" || code == "companion_model_unavailable" || code == "ops_agent_unavailable":
		return "dependency"
	case status == 409:
		return "conflict"
	case status == 422:
		return "validation"
	case status >= 500:
		return "server_error"
	default:
		return "request_error"
	}
}

func (s *Service) recordOpsEvent(r *http.Request, category string, status int, errorCode string, latency time.Duration, playerID string, detail map[string]any) {
	if r == nil {
		return
	}
	reqID := httpserver.RequestID(r.Context())
	gameID := r.PathValue("game")
	if meta, ok := r.Context().Value(reqMetaKey{}).(*reqMeta); ok && meta != nil {
		if gameID == "" {
			gameID = meta.gameID
		}
		if playerID == "" {
			playerID = meta.playerID
		}
	}
	if playerID == "" {
		playerID = r.PathValue("player")
	}
	now := time.Now().Unix()
	ms := latency.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	ev := telemetry.Event{
		RequestID:  reqID,
		GameID:     gameID,
		PlayerID:   playerID,
		Category:   category,
		Method:     r.Method,
		Path:       r.URL.Path,
		StatusCode: status,
		ErrorCode:  errorCode,
		LatencyMS:  ms,
		Detail:     detail,
		CreatedAt:  now,
	}
	if s.Telemetry != nil {
		s.Telemetry.RecordEvent(ev)
	}
	if !s.PersistOpsEvents || s.DB == nil {
		return
	}
	// Burst-sample 429 rate-limit anomalies (at most 1 persisted DB row per error_code+path per second)
	// so CC flood traffic cannot saturate MySQL connection pool.
	if category == "rate_limit" {
		sampleKey := errorCode + ":" + r.URL.Path
		s.opsRateMu.Lock()
		if s.opsRateLastLog == nil {
			s.opsRateLastLog = map[string]int64{}
		}
		last := s.opsRateLastLog[sampleKey]
		if now == last {
			s.opsRateMu.Unlock()
			if s.Telemetry != nil {
				s.Telemetry.RecordOpsEventDropped()
			}
			return
		}
		if len(s.opsRateLastLog) > 1024 {
			clear(s.opsRateLastLog)
		}
		s.opsRateLastLog[sampleKey] = now
		s.opsRateMu.Unlock()
	}

	var rawDetail []byte
	if detail != nil {
		rawDetail, _ = json.Marshal(detail)
	} else {
		rawDetail = []byte("{}")
	}
	if s.opsCh != nil {
		select {
		case s.opsCh <- opsEventTask{ev: ev, rawDetail: rawDetail}:
			return
		default:
			if s.Telemetry != nil {
				s.Telemetry.RecordOpsEventDropped()
			}
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = s.DB.ExecContext(ctx,
		"INSERT INTO ops_events(request_id,game_id,player_id,category,method,path,status_code,error_code,latency_ms,detail,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
		reqID, gameID, playerID, category, r.Method, r.URL.Path, status, errorCode, ms, rawDetail, now,
	)
}

func (s *Service) inspectServiceHealth(ctx context.Context) map[string]any {
	dbReady := false
	dbErr := ""
	if s.DB != nil {
		dbCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		err := database.Ready(dbCtx, s.DB)
		cancel()
		if err == nil {
			dbReady = true
		} else {
			dbErr = err.Error()
		}
	} else {
		dbErr = "database_nil"
	}

	ruleAddr := strings.TrimSpace(s.RuleCoreAddr)
	if ruleAddr == "" {
		ruleAddr = "rulecore:50051"
	}
	ruleReachable := false
	ruleProbeErr := ""
	probeStart := time.Now()
	if conn, err := net.DialTimeout("tcp", ruleAddr, 350*time.Millisecond); err == nil {
		ruleReachable = true
		_ = conn.Close()
	} else {
		ruleProbeErr = err.Error()
	}
	ruleProbeMS := time.Since(probeStart).Milliseconds()

	var snap telemetry.Snapshot
	if s.Telemetry != nil {
		snap = s.Telemetry.Snapshot(s.DB, database.ExpectedSchemaVersion, ruleAddr)
	}
	if snap.RuleCoreHealthy == nil {
		snap.RuleCoreHealthy = &ruleReachable
		snap.RuleCoreLastLatMS = ruleProbeMS
		if !ruleReachable {
			snap.RuleCoreLastError = ruleProbeErr
		}
	}

	return map[string]any{
		"db_ready":             dbReady,
		"db_error":             dbErr,
		"rulecore_addr":        ruleAddr,
		"rulecore_reachable":   ruleReachable,
		"rulecore_probe_ms":    ruleProbeMS,
		"rulecore_probe_error": ruleProbeErr,
		"sandbox":              s.Sandbox,
		"telemetry":            snap,
	}
}

func (s *Service) queryOpsEvents(ctx context.Context, gameID, playerID, requestID, errorCode, category string, since int64, limit int) ([]telemetry.Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if !s.PersistOpsEvents || s.DB == nil {
		out := []telemetry.Event{}
		if s.Telemetry != nil {
			for _, ev := range s.Telemetry.RecentEvents(gameID, playerID, requestID, errorCode, 200) {
				if category != "" && ev.Category != category {
					continue
				}
				if since > 0 && ev.CreatedAt < since {
					continue
				}
				out = append(out, ev)
				if len(out) >= limit {
					break
				}
			}
		}
		return out, nil
	}

	conds := []string{"1=1"}
	args := []any{}
	if gameID != "" {
		conds = append(conds, "game_id=?")
		args = append(args, gameID)
	}
	if playerID != "" {
		conds = append(conds, "player_id=?")
		args = append(args, playerID)
	}
	if requestID != "" {
		conds = append(conds, "request_id=?")
		args = append(args, requestID)
	}
	if errorCode != "" {
		conds = append(conds, "error_code=?")
		args = append(args, errorCode)
	}
	if category != "" {
		conds = append(conds, "category=?")
		args = append(args, category)
	}
	if since > 0 {
		conds = append(conds, "created_at>=?")
		args = append(args, since)
	}
	args = append(args, limit)

	query := "SELECT id,request_id,game_id,player_id,category,method,path,status_code,error_code,latency_ms,COALESCE(detail,JSON_OBJECT()),created_at FROM ops_events WHERE " + strings.Join(conds, " AND ") + " ORDER BY id DESC LIMIT ?"
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []telemetry.Event{}
	for rows.Next() {
		var ev telemetry.Event
		var rawDetail []byte
		if err = rows.Scan(&ev.ID, &ev.RequestID, &ev.GameID, &ev.PlayerID, &ev.Category, &ev.Method, &ev.Path, &ev.StatusCode, &ev.ErrorCode, &ev.LatencyMS, &rawDetail, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if len(rawDetail) > 0 {
			var parsed any
			if json.Unmarshal(rawDetail, &parsed) == nil {
				ev.Detail = parsed
			}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Service) traceRequestByID(ctx context.Context, game, requestID string) (map[string]any, error) {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 64 {
		return nil, bad(400, "invalid_request_id")
	}
	events, err := s.queryOpsEvents(ctx, game, "", requestID, "", "", 0, 50)
	if err != nil {
		return nil, err
	}
	audits := []any{}
	if s.DB != nil {
		rows, err := s.DB.QueryContext(ctx,
			"SELECT id,admin_id,action,reason,details,created_at FROM audits WHERE game_id=? AND request_id=? ORDER BY id DESC LIMIT 50",
			game, requestID,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, ts int64
			var admin, action, reason string
			var details json.RawMessage
			if err = rows.Scan(&id, &admin, &action, &reason, &details, &ts); err != nil {
				return nil, err
			}
			audits = append(audits, map[string]any{"id": id, "admin": admin, "action": action, "reason": reason, "details": details, "created_at": ts})
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"game_id":    game,
		"request_id": requestID,
		"ops_events": events,
		"audits":     audits,
	}, nil
}

func (s *Service) queryRowsGeneric(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		refs := make([]any, len(cols))
		for i := range vals {
			refs[i] = &vals[i]
		}
		if err = rows.Scan(refs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = vals[i]
			}
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func (s *Service) playerDiagnostics(ctx context.Context, game, player string) (map[string]any, error) {
	var name string
	if err := s.DB.QueryRowContext(ctx, "SELECT name FROM profiles WHERE game_id=? AND player_id=?", game, player).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, bad(404, "player_not_found")
		}
		return nil, err
	}
	out := map[string]any{"game_id": game, "player_id": player, "name": name}
	queries := []struct {
		key   string
		query string
	}{
		{"saves", "SELECT slot,revision,schema_version,checksum,deleted FROM saves WHERE game_id=? AND player_id=? ORDER BY slot"},
		{"save_history_recent", "SELECT slot,revision,schema_version,checksum,deleted FROM save_history WHERE game_id=? AND player_id=? ORDER BY revision DESC LIMIT 10"},
		{"activity_progress", "SELECT activity_id,version,bucket,progress FROM activity_progress WHERE game_id=? AND player_id=? ORDER BY activity_id,version DESC,bucket DESC LIMIT 50"},
		{"grants", "SELECT id,business_key,item,amount,created_at FROM grants WHERE game_id=? AND player_id=? ORDER BY created_at DESC,id LIMIT 20"},
		{"inventory", "SELECT item,amount FROM inventory WHERE game_id=? AND player_id=? ORDER BY item"},
		{"inventory_ledger", "SELECT id,item,delta,balance,reason,business_key,created_at FROM inventory_ledger WHERE game_id=? AND player_id=? ORDER BY id DESC LIMIT 25"},
		{"mail", "SELECT m.id,m.title,m.claimed,COALESCE(st.read_at,0)>0 AS is_read,COALESCE(st.deleted_at,0)>0 AS deleted,m.expires_at,g.item,g.amount FROM mail m JOIN grants g ON g.id=m.grant_id LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.player_id=? ORDER BY m.expires_at DESC,m.id LIMIT 25"},
		{"recent_events", "SELECT event_id,event_type,amount,source,created_at FROM events WHERE game_id=? AND player_id=? ORDER BY created_at DESC,event_id LIMIT 25"},
		{"scores", "SELECT board_id,season,score,reached_at FROM scores WHERE game_id=? AND player_id=? ORDER BY reached_at DESC LIMIT 10"},
		{"replays", "SELECT request_id,rule_version,seed,score,state_hash FROM replays WHERE game_id=? AND player_id=? ORDER BY request_id DESC LIMIT 10"},
	}
	if s.ConcurrentDiagnostics {
		results := make([][]map[string]any, len(queries))
		errs := make([]error, len(queries))
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for i, q := range queries {
			wg.Add(1)
			go func(idx int, query string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				results[idx], errs[idx] = s.queryRowsGeneric(ctx, query, game, player)
			}(i, q.query)
		}
		wg.Wait()
		for i, q := range queries {
			if errs[i] != nil {
				return nil, errs[i]
			}
			out[q.key] = results[i]
		}
	} else {
		for _, q := range queries {
			items, err := s.queryRowsGeneric(ctx, q.query, game, player)
			if err != nil {
				return nil, err
			}
			out[q.key] = items
		}
	}
	var activeSessions int64
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE player_id=? AND expires_at>?", player, time.Now().Unix()).Scan(&activeSessions); err != nil {
		return nil, err
	}
	out["active_sessions_count"] = activeSessions

	opsEvents, err := s.queryOpsEvents(ctx, game, player, "", "", "", 0, 20)
	if err != nil {
		return nil, err
	}
	out["recent_ops_events"] = opsEvents
	return out, nil
}

func (s *Service) opsObservabilityRoutes(m *http.ServeMux) {
	s.handle(m, "GET /gm/system/telemetry", func(r *http.Request) (any, error) {
		if _, e := s.gmSession(r); e != nil {
			return nil, e
		}
		return s.inspectServiceHealth(r.Context()), nil
	})
	m.HandleFunc("GET /gm/system/metrics", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if !s.authorizeMetricsScraper(r) {
			if _, e := s.gmSession(r); e != nil {
				status, code := 401, "gm_unauthorized"
				var p Problem
				if errors.As(e, &p) {
					status, code = p.Status, p.Code
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]string{"error": code})
				return
			}
		}
		var snap telemetry.Snapshot
		if s.Telemetry != nil {
			snap = s.Telemetry.Snapshot(s.DB, database.ExpectedSchemaVersion, s.RuleCoreAddr)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(telemetry.PrometheusText(snap)))
	})
	s.handle(m, "GET /gm/games/{game}/ops-events", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		limit := 50
		if raw := q.Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 200 {
				return nil, bad(400, "invalid_limit")
			}
			limit = n
		}
		var since int64
		if raw := q.Get("since"); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 {
				return nil, bad(400, "invalid_since")
			}
			since = n
		}
		return s.queryOpsEvents(r.Context(), a.Game, q.Get("player_id"), q.Get("request_id"), q.Get("error_code"), q.Get("category"), since, limit)
	})
	s.handle(m, "GET /gm/games/{game}/players/{player}/diagnostics", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		return s.playerDiagnostics(r.Context(), a.Game, r.PathValue("player"))
	})

	authorizePprof := func(w http.ResponseWriter, r *http.Request) bool {
		if s.authorizeMetricsScraper(r) {
			return true
		}
		gm, err := s.gmSession(r)
		if err != nil || gm.Role != "admin" {
			w.Header().Set("Content-Type", "application/json")
			if err == nil {
				w.WriteHeader(403)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
			} else {
				w.WriteHeader(401)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "gm_unauthorized"})
			}
			return false
		}
		return true
	}
	wrapPprof := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !authorizePprof(w, r) {
				return
			}
			h(w, r)
		}
	}
	m.HandleFunc("GET /gm/system/pprof/", wrapPprof(func(w http.ResponseWriter, r *http.Request) {
		// Rewrite prefix so net/http/pprof resolves /debug/pprof/... cleanly.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/debug/pprof/" + strings.TrimPrefix(r.URL.Path, "/gm/system/pprof/")
		pprof.Index(w, r2)
	}))
	m.HandleFunc("GET /gm/system/pprof/cmdline", wrapPprof(pprof.Cmdline))
	m.HandleFunc("GET /gm/system/pprof/profile", wrapPprof(pprof.Profile))
	m.HandleFunc("GET /gm/system/pprof/symbol", wrapPprof(pprof.Symbol))
	m.HandleFunc("GET /gm/system/pprof/trace", wrapPprof(pprof.Trace))
}

func (s *Service) authorizeMetricsScraper(r *http.Request) bool {
	if s.MetricsBearerToken == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	got := strings.TrimPrefix(auth, "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.MetricsBearerToken)) == 1
}
