package service

import (
	"bytes"
	"database/sql"
	_ "embed"
	"encoding/json"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed activity.schema.json
var activitySchema string
var compiledActivitySchema = func() *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	if e := c.AddResource("activity.json", strings.NewReader(activitySchema)); e != nil {
		panic(e)
	}
	v, e := c.Compile("activity.json")
	if e != nil {
		panic(e)
	}
	return v
}()

func schemaCheck(raw []byte) error {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(&value); e != nil {
		return bad(400, "invalid_json_schema_input")
	}
	if e := compiledActivitySchema.Validate(value); e != nil {
		return bad(400, "activity_schema_invalid")
	}
	return nil
}

type SimulationEvent struct {
	Amount int64 `json:"amount"`
	Time   int64 `json:"time"`
}

func simulate(a Activity, events []SimulationEvent) map[string]any {
	progress := map[string]int64{}
	rewards := map[string]bool{}
	for _, event := range events {
		if event.Time < a.Starts || event.Time >= a.Ends || event.Amount < 1 || event.Amount > 100000 || !a.ClientAllowed {
			continue
		}
		bucket := bucketFor(a, time.Unix(event.Time, 0))
		n := event.Amount
		if a.Kind == "signin" {
			n = 1
		}
		progress[bucket] = min(a.Target, progress[bucket]+n)
		if progress[bucket] >= a.Target {
			rewards[bucket] = true
		}
	}
	return map[string]any{"progress": progress, "reward_count": len(rewards), "reward_total": int64(len(rewards)) * a.Amount, "trust": "client_simulation"}
}
func (s *Service) plannerRoutes(m *http.ServeMux) {
	base := "/gm/games/{game}/planner"
	s.handle(m, "GET "+base+"/catalog", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(), "SELECT id,name FROM items WHERE game_id=? ORDER BY id", a.Game)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		items := []any{}
		for rows.Next() {
			var id, n string
			if e = rows.Scan(&id, &n); e != nil {
				return nil, e
			}
			items = append(items, map[string]string{"id": id, "name": n})
		}
		return map[string]any{"items": items, "kinds": []string{"cumulative", "daily", "signin", "milestone"}, "events": []string{"building_upgraded", "match_completed", "visit"}, "schema": json.RawMessage(activitySchema), "provider": "deterministic_mock", "real_ai": "unverified"}, rows.Err()
	})
	s.handle(m, "POST "+base+"/validate", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		var b struct {
			Config json.RawMessage   `json:"config"`
			Events []SimulationEvent `json:"events"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.Events) > 1000 {
			return nil, bad(400, "too_many_simulation_events")
		}
		if e = schemaCheck(b.Config); e != nil {
			return nil, e
		}
		if e = s.validatePayload(r, a.Game, "activity", b.Config); e != nil {
			return nil, e
		}
		v, e := readActivity(b.Config)
		if e != nil {
			return nil, e
		}
		return map[string]any{"valid": true, "simulation": simulate(v, b.Events)}, nil
	})
	s.handle(m, "POST "+base+"/generate", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "write")
		if e != nil {
			return nil, e
		}
		if !s.Sandbox {
			return nil, bad(503, "real_ai_provider_not_configured")
		}
		var b struct {
			Brief string `json:"brief"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.Brief) < 2 || len(b.Brief) > 2000 {
			return nil, bad(400, "invalid_brief")
		}
		start := time.Now()
		var item string
		if e = s.DB.QueryRowContext(r.Context(), "SELECT id FROM items WHERE game_id=? ORDER BY id LIMIT 1", a.Game).Scan(&item); e != nil {
			return nil, bad(400, "game_has_no_items")
		}
		target, reward := int64(3), int64(5)
		numbers := regexp.MustCompile(`[0-9]+`).FindAllString(b.Brief, -1)
		if len(numbers) > 0 {
			target, _ = strconv.ParseInt(numbers[0], 10, 64)
			target = max(1, min(target, 1000))
		}
		if len(numbers) > 1 {
			reward, _ = strconv.ParseInt(numbers[1], 10, 64)
			reward = max(1, min(reward, 1000000))
		}
		kind, event := "cumulative", "visit"
		if strings.Contains(b.Brief, "每日") {
			kind = "daily"
		}
		if strings.Contains(b.Brief, "签到") {
			kind = "signin"
			target = 1
		}
		if strings.Contains(b.Brief, "建筑") {
			event = "building_upgraded"
		}
		if strings.Contains(b.Brief, "对局") || strings.Contains(b.Brief, "卡牌") {
			event = "match_completed"
		}
		id := random()
		v := Activity{ID: "ai-" + id[:12], Version: 1, Kind: kind, Event: event, Target: target, Starts: time.Now().Unix() - 1, Ends: time.Now().Add(7 * 24 * time.Hour).Unix(), ClientAllowed: true, Item: item, Amount: reward}
		attempts := 1
		candidate := v
		if strings.Contains(b.Brief, "[simulate-invalid]") {
			candidate.Target = 0
		}
		raw, _ := json.Marshal(candidate)
		firstPass := schemaCheck(raw) == nil && s.validatePayload(r, a.Game, "activity", raw) == nil
		if !firstPass {
			attempts = 2
			raw, _ = json.Marshal(v)
		}
		if e = schemaCheck(raw); e != nil {
			return nil, e
		}
		if e = s.validatePayload(r, a.Game, "activity", raw); e != nil {
			return nil, e
		}
		sim := simulate(v, []SimulationEvent{{Amount: min(v.Target, 100000), Time: time.Now().Unix()}})
		latency := time.Since(start).Milliseconds()
		_, e = s.DB.ExecContext(r.Context(), "INSERT INTO planner_runs VALUES(?,?,?,'deterministic_mock',?,TRUE,?,?,0,?)", id, a.Game, a.ID, raw, attempts, latency, time.Now().Unix())
		return map[string]any{"run_id": id, "config": json.RawMessage(raw), "valid": true, "simulation": sim, "attempts": attempts, "first_pass": firstPass, "latency_ms": latency, "cost_usd": 0, "provider": "deterministic_mock", "review_status": "draft", "real_ai": "unverified"}, e
	})
	s.handle(m, "GET "+base+"/runs", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		var total, valid, repaired int
		var latency sql.NullFloat64
		e = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*),COALESCE(SUM(valid),0),COALESCE(SUM(attempts>1),0),AVG(latency_ms) FROM planner_runs WHERE game_id=?", a.Game).Scan(&total, &valid, &repaired, &latency)
		return map[string]any{"total": total, "valid": valid, "repaired": repaired, "average_latency_ms": latency.Float64, "cost_usd": 0, "provider": "deterministic_mock", "real_ai": "unverified"}, e
	})
}
