package service

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"miniverse/backend/internal/aigateway"
	"miniverse/backend/internal/httpserver"
	"miniverse/backend/internal/opsagent"
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
		if e = s.validatePayload(r.Context(), a.Game, "activity", b.Config); e != nil {
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
		var b struct {
			Brief string `json:"brief"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if len(b.Brief) < 2 || len(b.Brief) > 2000 {
			return nil, bad(400, "invalid_brief")
		}
		proto, llmURL, llmKey, llmModel := s.opsAgentConfig()
		if llmURL == "" && !s.Sandbox {
			return nil, bad(503, "real_ai_provider_not_configured")
		}
		start := time.Now()
		var item string
		if e = s.DB.QueryRowContext(r.Context(), "SELECT id FROM items WHERE game_id=? ORDER BY id LIMIT 1", a.Game).Scan(&item); e != nil {
			return nil, bad(400, "game_has_no_items")
		}
		id := random()
		if llmURL != "" && llmModel != "" {
			return s.generatePlannerWithWorkflow(r.Context(), a, id, item, b.Brief, start, opsagent.ModelConfig{
				Protocol: proto,
				URL:      llmURL,
				APIKey:   llmKey,
				Model:    llmModel,
				Sandbox:  s.Sandbox,
			})
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
		v := Activity{ID: "ai-" + id[:12], Version: 1, Kind: kind, Event: event, Target: target, Starts: time.Now().Unix() - 1, Ends: time.Now().Add(7 * 24 * time.Hour).Unix(), ClientAllowed: true, Item: item, Amount: reward}
		attempts := 1
		candidate := v
		if strings.Contains(b.Brief, "[simulate-invalid]") {
			candidate.Target = 0
		}
		raw, _ := json.Marshal(candidate)
		firstPass := schemaCheck(raw) == nil && s.validatePayload(r.Context(), a.Game, "activity", raw) == nil
		if !firstPass {
			attempts = 2
			raw, _ = json.Marshal(v)
		}
		if e = schemaCheck(raw); e != nil {
			return nil, e
		}
		if e = s.validatePayload(r.Context(), a.Game, "activity", raw); e != nil {
			return nil, e
		}
		sim := simulate(v, []SimulationEvent{{Amount: min(v.Target, 100000), Time: time.Now().Unix()}})
		latency := time.Since(start).Milliseconds()
		_, e = s.DB.ExecContext(r.Context(), "INSERT INTO planner_runs(id,game_id,admin_id,provider,config,valid,attempts,latency_ms,cost_usd,created_at) VALUES(?,?,?,'deterministic_mock',?,TRUE,?,?,0,?)", id, a.Game, a.ID, raw, attempts, latency, time.Now().Unix())
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

func (s *Service) generatePlannerWithWorkflow(ctx context.Context, a GM, runID, fallbackItem, brief string, start time.Time, cfg opsagent.ModelConfig) (map[string]any, error) {
	gw := aigateway.NewSession(httpserver.TraceID(ctx), "planner", a.Game, a.ID, s, brief)
	maskedBrief, blocked := gw.HookLLMInput(ctx, brief)
	if blocked != "" {
		return nil, bad(400, blocked)
	}
	now := time.Now()
	sysPrompt := fmt.Sprintf(
		"你是游戏活动策划配置生成 Workflow。仅输出严格符合 Activity JSON 的纯 JSON 对象（无 markdown 代码块）。"+
			"可用道具：%s，活动类型：cumulative/daily/signin/milestone，事件：building_upgraded/match_completed/visit。"+
			"时间窗口参考 starts_at=%d, ends_at=%d, id=\"ai-%s\", version=1, client_allowed=true。",
		fallbackItem, now.Unix()-1, now.Add(7*24*time.Hour).Unix(), runID[:12],
	)
	var history []opsagent.SimpleTextMessage
	userPrompt := "请根据以下策划需求生成活动 JSON：" + maskedBrief

	var raw []byte
	var act Activity
	firstPass := false
	attempts := 0
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		attempts = attempt
		reply, _, _, callErr := opsagent.CallSimpleText(ctx, cfg, sysPrompt, history, userPrompt, 700)
		if callErr != nil {
			return nil, bad(502, "planner_llm_unavailable")
		}
		cleaned := strings.TrimSpace(reply)
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
		raw = []byte(cleaned)
		if err := schemaCheck(raw); err != nil {
			lastErr = err
		} else if err := s.validatePayload(ctx, a.Game, "activity", raw); err != nil {
			lastErr = err
		} else {
			parsed, rerr := readActivity(raw)
			if rerr != nil {
				lastErr = rerr
			} else {
				act = parsed
				lastErr = nil
				if attempt == 1 {
					firstPass = true
				}
				break
			}
		}
		// Feed exact deterministic validation error back to LLM for self-repair.
		history = append(history,
			opsagent.SimpleTextMessage{Role: "user", Content: userPrompt},
			opsagent.SimpleTextMessage{Role: "assistant", Content: cleaned},
		)
		userPrompt = fmt.Sprintf("你生成的 JSON 未通过校验（错误：%v），请严格修正后重新仅输出 JSON 对象。", lastErr)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	sim := simulate(act, []SimulationEvent{{Amount: min(act.Target, 100000), Time: now.Unix()}})
	latency := time.Since(start).Milliseconds()
	_, e := s.DB.ExecContext(ctx, "INSERT INTO planner_runs(id,game_id,admin_id,provider,config,valid,attempts,latency_ms,cost_usd,created_at) VALUES(?,?,?,'llm_workflow',?,TRUE,?,?,0,?)", runID, a.Game, a.ID, raw, attempts, latency, now.Unix())
	return map[string]any{
		"run_id":        runID,
		"config":        json.RawMessage(raw),
		"valid":         true,
		"simulation":    sim,
		"attempts":      attempts,
		"first_pass":    firstPass,
		"latency_ms":    latency,
		"cost_usd":      0,
		"provider":      "llm_workflow",
		"review_status": "draft",
		"real_ai":       "gateway_verified",
	}, e
}
