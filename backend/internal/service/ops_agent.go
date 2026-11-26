package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"miniverse/backend/internal/aigateway"
	"miniverse/backend/internal/httpserver"
	"miniverse/backend/internal/mcp"
	"miniverse/backend/internal/opsagent"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type mcpServiceAdapter struct {
	srv *mcp.Server
}

func (a mcpServiceAdapter) ListTools(ctx context.Context) ([]mcp.ToolSpec, error) {
	return a.srv.ListTools(ctx), nil
}

func (a mcpServiceAdapter) CallTool(ctx context.Context, name string, args json.RawMessage) (string, any, bool, error) {
	text, structured, isErr, rpcErr := a.srv.CallTool(ctx, name, args)
	if rpcErr != nil {
		return "", nil, true, errors.New(rpcErr.Message)
	}
	return text, structured, isErr, nil
}

func (s *Service) opsAgentConfig() (string, string, string, string) {
	s.opsAgentMu.RLock()
	defer s.opsAgentMu.RUnlock()
	u := strings.TrimSpace(s.OpsAgentURL)
	proto, err := opsagent.NormalizeProtocol(s.OpsAgentProtocol, u)
	if err != nil {
		proto = opsagent.ProtocolOpenAI
	}
	return proto, u, strings.TrimSpace(s.OpsAgentAPIKey), strings.TrimSpace(s.OpsAgentModel)
}

func (s *Service) setOpsAgentConfig(protocol, url, apiKey, model string) {
	s.opsAgentMu.Lock()
	defer s.opsAgentMu.Unlock()
	s.OpsAgentProtocol = strings.TrimSpace(protocol)
	s.OpsAgentURL = strings.TrimSpace(url)
	s.OpsAgentAPIKey = strings.TrimSpace(apiKey)
	s.OpsAgentModel = strings.TrimSpace(model)
}

func encryptOpsSecret(key []byte, plain string) (string, error) {
	if len(key) != 32 || plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptOpsSecret(key []byte, encoded string) (string, error) {
	if len(key) != 32 || !strings.HasPrefix(encoded, "v1:") {
		return "", errors.New("cannot decrypt secret")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Service) loadPersistedOpsAgentConfig() {
	if s.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var proto, u, model, cipherText string
	var keyEncrypted bool
	err := s.DB.QueryRowContext(ctx, "SELECT protocol,url,model,COALESCE(api_key_ciphertext,''),key_encrypted FROM ops_agent_config WHERE id=1").Scan(&proto, &u, &model, &cipherText, &keyEncrypted)
	if err != nil {
		return
	}
	var key string
	if keyEncrypted && cipherText != "" && len(s.OpsAgentSecretKey) == 32 {
		decrypted, derr := decryptOpsSecret(s.OpsAgentSecretKey, cipherText)
		if derr != nil {
			slog.Warn("ops_agent_secret_decrypt_failed")
		} else {
			key = decrypted
		}
	}
	if strings.TrimSpace(u) != "" || strings.TrimSpace(model) != "" || key != "" {
		if key == "" {
			_, _, envKey, _ := s.opsAgentConfig()
			key = envKey
		}
		s.setOpsAgentConfig(proto, u, key, model)
	}
}

func (s *Service) savePersistedOpsAgentConfigTx(ctx context.Context, tx *sql.Tx, adminID, proto, u, apiKey, model string) (bool, error) {
	trimmedKey := strings.TrimSpace(apiKey)
	cipherText := ""
	keyEncrypted := false
	if len(s.OpsAgentSecretKey) == 32 && trimmedKey != "" {
		enc, err := encryptOpsSecret(s.OpsAgentSecretKey, trimmedKey)
		if err != nil {
			return false, err
		}
		cipherText = enc
		keyEncrypted = true
	}
	if tx == nil {
		return keyEncrypted, nil
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO ops_agent_config(id,protocol,url,model,api_key,api_key_ciphertext,key_encrypted,updated_by,updated_at)
		 VALUES(1,?,?,?,'',?,?,?,?)
		 ON DUPLICATE KEY UPDATE protocol=VALUES(protocol),url=VALUES(url),model=VALUES(model),api_key='',api_key_ciphertext=VALUES(api_key_ciphertext),key_encrypted=VALUES(key_encrypted),updated_by=VALUES(updated_by),updated_at=VALUES(updated_at)`,
		strings.TrimSpace(proto), strings.TrimSpace(u), strings.TrimSpace(model), cipherText, keyEncrypted, adminID, time.Now().Unix(),
	)
	return keyEncrypted, err
}

func maskSecret(secret string) string {
	n := len(secret)
	if n == 0 {
		return ""
	}
	if n <= 8 {
		return "****"
	}
	return secret[:3] + "****" + secret[n-4:]
}

func (s *Service) opsAgentConfigResponse(gm GM) map[string]any {
	proto, u, key, model := s.opsAgentConfig()
	return map[string]any{
		"protocol":       proto,
		"url":            u,
		"model":          model,
		"has_api_key":    key != "",
		"key_persisted":  len(s.OpsAgentSecretKey) == 32 && key != "",
		"api_key_masked": maskSecret(key),
		"configured":     u != "" && model != "",
		"sandbox":        s.Sandbox,
		"can_edit":       gm.Role == "admin",
	}
}

func (s *Service) persistOpsAgentConfigWithAudit(r *http.Request, gm GM, proto, normalizedURL, apiKey, model string) error {
	if s.DB == nil {
		return nil
	}
	return s.transaction(r.Context(), func(tx *sql.Tx) error {
		keyPersisted, err := s.savePersistedOpsAgentConfigTx(r.Context(), tx, gm.ID, proto, normalizedURL, apiKey, model)
		if err != nil {
			return err
		}
		auditGM := GM{ID: gm.ID, Role: gm.Role, Game: "*"}
		return audit(r, tx, auditGM, "ops_agent_config_updated", "更新智能排障模型配置", map[string]any{
			"protocol":      proto,
			"url":           normalizedURL,
			"model":         model,
			"has_api_key":   strings.TrimSpace(apiKey) != "",
			"key_persisted": keyPersisted,
		})
	})
}

// RecordAIGatewayAudit implements aigateway.AuditSink, updating telemetry and persisting gateway decisions.
func (s *Service) RecordAIGatewayAudit(ctx context.Context, rec aigateway.AuditRecord) {
	if s.Telemetry != nil && (rec.Decision == "block" || rec.Decision == "downgrade") {
		s.Telemetry.RecordAIGatewayBlocked()
	}
	if !s.PersistOpsEvents || s.DB == nil {
		return
	}
	detailRaw := []byte("{}")
	if rec.Detail != nil {
		if b, err := json.Marshal(rec.Detail); err == nil {
			detailRaw = b
		}
	}
	wCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = s.DB.ExecContext(wCtx,
		"INSERT INTO ai_gateway_audits(trace_id,source,game_id,actor_id,hook_stage,decision,reason_code,pii_redacted_count,detail,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
		rec.TraceID, rec.Source, rec.GameID, rec.ActorID, rec.HookStage, rec.Decision, rec.ReasonCode, rec.PIIRedactedCount, detailRaw, rec.CreatedAt,
	)
}

func (s *Service) persistOpsAgentRun(ctx context.Context, r *http.Request, a GM, question string, res opsagent.RunResult) (string, int64, error) {
	runID := "run-" + random()[:20]
	now := time.Now().Unix()
	e := s.transaction(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx,
			"INSERT INTO ops_agent_runs(id,game_id,admin_id,question,mode,model,status,summary,root_cause,recommendation,proposal_request_id,steps_count,prompt_tokens,completion_tokens,duration_ms,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			runID, a.Game, a.ID, question, res.Mode, res.Model, res.Status, res.Summary, res.RootCause, res.Recommendation, res.ProposalRequestID, len(res.Steps), res.PromptTokens, res.CompletionTokens, res.DurationMS, now,
		); e != nil {
			return e
		}
		for _, st := range res.Steps {
			if _, e := tx.ExecContext(ctx,
				"INSERT INTO ops_agent_steps(run_id,step_index,tool_name,arguments_json,result_summary,is_error,duration_ms,created_at) VALUES(?,?,?,?,?,?,?,?)",
				runID, st.StepIndex, st.ToolName, st.ArgumentsJSON, st.ResultSummary, st.IsError, st.DurationMS, now,
			); e != nil {
				return e
			}
		}
		reason, _ := sanitizeAIReason("[AI-OPS]", question)
		if reason == "" {
			reason = "[AI-OPS] diagnose"
		}
		return audit(r, tx, a, "ops_agent_diagnose", reason, map[string]any{
			"run_id":              runID,
			"mode":                res.Mode,
			"steps_count":         len(res.Steps),
			"proposal_request_id": res.ProposalRequestID,
		})
	})
	return runID, now, e
}

func formatOpsAgentRunResponse(runID, gameID, adminID, question string, now int64, res opsagent.RunResult) map[string]any {
	return map[string]any{
		"run_id":              runID,
		"game_id":             gameID,
		"admin_id":            adminID,
		"question":            question,
		"mode":                res.Mode,
		"model":               res.Model,
		"real_ai":             res.Mode == "llm",
		"status":              res.Status,
		"summary":             res.Summary,
		"root_cause":          res.RootCause,
		"recommendation":      res.Recommendation,
		"evidence_ids":        res.EvidenceIDs,
		"proposal_request_id": res.ProposalRequestID,
		"steps_count":         len(res.Steps),
		"steps":               res.Steps,
		"prompt_tokens":       res.PromptTokens,
		"completion_tokens":   res.CompletionTokens,
		"duration_ms":         res.DurationMS,
		"created_at":          now,
	}
}

func (s *Service) opsAgentRoutes(m *http.ServeMux) {
	s.handle(m, "GET /gm/system/ops-agent/config", func(r *http.Request) (any, error) {
		gm, err := s.gmSession(r)
		if err != nil {
			return nil, err
		}
		return s.opsAgentConfigResponse(gm), nil
	})

	s.handle(m, "PUT /gm/system/ops-agent/config", func(r *http.Request) (any, error) {
		gm, err := s.gmSession(r)
		if err != nil {
			return nil, err
		}
		if gm.Role != "admin" {
			return nil, bad(403, "forbidden")
		}
		var b struct {
			Protocol    string `json:"protocol"`
			URL         string `json:"url"`
			APIKey      string `json:"api_key"`
			Model       string `json:"model"`
			ClearAPIKey bool   `json:"clear_api_key"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		_, _, curKey, _ := s.opsAgentConfig()
		nextURL := strings.TrimSpace(b.URL)
		nextModel := strings.TrimSpace(b.Model)
		nextKey := strings.TrimSpace(b.APIKey)
		if b.ClearAPIKey {
			nextKey = ""
		} else if nextKey == "" {
			nextKey = curKey
		}
		if nextURL == "" {
			if err := s.persistOpsAgentConfigWithAudit(r, gm, "", "", "", ""); err != nil {
				return nil, err
			}
			s.setOpsAgentConfig("", "", "", "")
			return s.opsAgentConfigResponse(gm), nil
		}
		normalized, proto, verr := opsagent.ValidateModelConfig(opsagent.ModelConfig{
			Protocol: b.Protocol,
			URL:      nextURL,
			APIKey:   nextKey,
			Model:    nextModel,
			Sandbox:  s.Sandbox,
		})
		if verr != nil {
			var ae *opsagent.Error
			if errors.As(verr, &ae) {
				return nil, bad(ae.Status, ae.Code)
			}
			return nil, bad(400, "invalid_ops_agent_url")
		}
		if err := s.persistOpsAgentConfigWithAudit(r, gm, proto, normalized, nextKey, nextModel); err != nil {
			return nil, err
		}
		s.setOpsAgentConfig(proto, normalized, nextKey, nextModel)
		return s.opsAgentConfigResponse(gm), nil
	})

	s.handleWithTimeout(m, "POST /gm/system/ops-agent/test", 20*time.Second, func(r *http.Request) (any, error) {
		gm, err := s.gmSession(r)
		if err != nil {
			return nil, err
		}
		if gm.Role != "admin" {
			return nil, bad(403, "forbidden")
		}
		var b struct {
			Protocol string `json:"protocol"`
			URL      string `json:"url"`
			APIKey   string `json:"api_key"`
			Model    string `json:"model"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		curProto, curURL, curKey, curModel := s.opsAgentConfig()
		testProto := strings.TrimSpace(b.Protocol)
		testURL := strings.TrimSpace(b.URL)
		if testURL == "" {
			testURL = curURL
			if testProto == "" {
				testProto = curProto
			}
		}
		testKey := strings.TrimSpace(b.APIKey)
		if testKey == "" {
			testKey = curKey
		}
		testModel := strings.TrimSpace(b.Model)
		if testModel == "" {
			testModel = curModel
		}
		reply, latencyMS, terr := opsagent.TestConnection(r.Context(), opsagent.ModelConfig{
			Protocol: testProto,
			URL:      testURL,
			APIKey:   testKey,
			Model:    testModel,
			Sandbox:  s.Sandbox,
		})
		if terr != nil {
			code, detail := terr.Error(), ""
			var ae *opsagent.Error
			if errors.As(terr, &ae) {
				code, detail = ae.Code, ae.Detail
			}
			return map[string]any{
				"ok":         false,
				"latency_ms": latencyMS,
				"error":      code,
				"detail":     detail,
			}, nil
		}
		return map[string]any{
			"ok":            true,
			"latency_ms":    latencyMS,
			"reply_preview": reply,
		}, nil
	})

	s.handleWithTimeout(m, "POST /gm/system/ops-agent/models", 20*time.Second, func(r *http.Request) (any, error) {
		gm, err := s.gmSession(r)
		if err != nil {
			return nil, err
		}
		if gm.Role != "admin" {
			return nil, bad(403, "forbidden")
		}
		var b struct {
			Protocol string `json:"protocol"`
			URL      string `json:"url"`
			APIKey   string `json:"api_key"`
			Model    string `json:"model"`
			Save     bool   `json:"save"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		curProto, curURL, curKey, curModel := s.opsAgentConfig()
		targetProto := strings.TrimSpace(b.Protocol)
		targetURL := strings.TrimSpace(b.URL)
		if targetURL == "" {
			targetURL = curURL
			if targetProto == "" {
				targetProto = curProto
			}
		}
		targetKey := strings.TrimSpace(b.APIKey)
		if targetKey == "" {
			targetKey = curKey
		}
		models, latencyMS, ferr := opsagent.FetchModels(r.Context(), opsagent.ModelConfig{
			Protocol: targetProto,
			URL:      targetURL,
			APIKey:   targetKey,
			Sandbox:  s.Sandbox,
		})
		if ferr != nil {
			code, detail := ferr.Error(), ""
			var ae *opsagent.Error
			if errors.As(ferr, &ae) {
				code, detail = ae.Code, ae.Detail
			}
			return map[string]any{
				"ok":         false,
				"models":     []string{},
				"count":      0,
				"latency_ms": latencyMS,
				"error":      code,
				"detail":     detail,
			}, nil
		}
		saved := false
		if b.Save && targetURL != "" {
			chosenModel := strings.TrimSpace(b.Model)
			if chosenModel == "" {
				chosenModel = curModel
			}
			if chosenModel == "" && len(models) > 0 {
				chosenModel = models[0]
			}
			if chosenModel != "" {
				if normalized, proto, verr := opsagent.ValidateModelConfig(opsagent.ModelConfig{
					Protocol: targetProto,
					URL:      targetURL,
					APIKey:   targetKey,
					Model:    chosenModel,
					Sandbox:  s.Sandbox,
				}); verr == nil {
					if err := s.persistOpsAgentConfigWithAudit(r, gm, proto, normalized, targetKey, chosenModel); err == nil {
						s.setOpsAgentConfig(proto, normalized, targetKey, chosenModel)
						saved = true
					}
				}
			}
		}
		return map[string]any{
			"ok":         true,
			"models":     models,
			"count":      len(models),
			"latency_ms": latencyMS,
			"saved":      saved,
			"config":     s.opsAgentConfigResponse(gm),
		}, nil
	})

	base := "/gm/games/{game}/ops-agent"
	s.handleWithTimeout(m, "POST "+base+"/diagnose", 45*time.Second, func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		if s.opsAgentActive != nil {
			select {
			case s.opsAgentActive <- struct{}{}:
				defer func() { <-s.opsAgentActive }()
			default:
				return nil, bad(503, "ops_agent_busy")
			}
		}
		var b struct {
			Question       string `json:"question"`
			PlayerID       string `json:"player_id"`
			RequestID      string `json:"request_id"`
			ActivityID     string `json:"activity_id"`
			AllowPropose   bool   `json:"allow_propose"`
			ProposalItem   string `json:"proposal_item"`
			ProposalAmount int64  `json:"proposal_amount"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		b.Question = strings.TrimSpace(b.Question)
		if utf8.RuneCountInString(b.Question) < 2 || utf8.RuneCountInString(b.Question) > 1000 {
			return nil, bad(400, "invalid_question")
		}

		mcpSrv := s.newMCPServer()
		agentCtx := withMCPGM(r.Context(), a)
		agentProto, agentURL, agentKey, agentModel := s.opsAgentConfig()
		gw := aigateway.NewSession(httpserver.TraceID(r.Context()), "opsagent", a.Game, a.ID, s, b.RequestID, b.PlayerID, b.ActivityID, b.Question)
		res, err := opsagent.Run(agentCtx, opsagent.ModelConfig{
			Protocol: agentProto,
			URL:      agentURL,
			APIKey:   agentKey,
			Model:    agentModel,
			Sandbox:  s.Sandbox,
		}, mcpServiceAdapter{srv: mcpSrv}, opsagent.RunRequest{
			GameID:         a.Game,
			AdminID:        a.ID,
			Question:       b.Question,
			PlayerID:       strings.TrimSpace(b.PlayerID),
			RequestID:      strings.TrimSpace(b.RequestID),
			ActivityID:     strings.TrimSpace(b.ActivityID),
			AllowPropose:   b.AllowPropose,
			ProposalItem:   strings.TrimSpace(b.ProposalItem),
			ProposalAmount: b.ProposalAmount,
			Gateway:        gw,
		})
		if err != nil {
			var ae *opsagent.Error
			if errors.As(err, &ae) {
				return nil, bad(ae.Status, ae.Code)
			}
			return nil, err
		}

		runID, now, e := s.persistOpsAgentRun(r.Context(), r, a, b.Question, res)
		if e != nil {
			return nil, e
		}
		return formatOpsAgentRunResponse(runID, a.Game, a.ID, b.Question, now, res), nil
	})

	// SSE streaming endpoint pushing real-time step/hook progress and canceling upstream calls on client disconnect.
	m.HandleFunc("POST "+base+"/diagnose/stream", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		r = r.WithContext(ctx)

		a, err := s.gm(r, "read")
		if err != nil {
			status, code := 401, "gm_unauthorized"
			var p Problem
			if errors.As(err, &p) {
				status, code = p.Status, p.Code
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}
		if s.opsAgentActive != nil {
			select {
			case s.opsAgentActive <- struct{}{}:
				defer func() { <-s.opsAgentActive }()
			default:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(503)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "ops_agent_busy"})
				return
			}
		}
		var b struct {
			Question       string `json:"question"`
			PlayerID       string `json:"player_id"`
			RequestID      string `json:"request_id"`
			ActivityID     string `json:"activity_id"`
			AllowPropose   bool   `json:"allow_propose"`
			ProposalItem   string `json:"proposal_item"`
			ProposalAmount int64  `json:"proposal_amount"`
		}
		if err := decode(r, &b); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		b.Question = strings.TrimSpace(b.Question)
		if utf8.RuneCountInString(b.Question) < 2 || utf8.RuneCountInString(b.Question) > 1000 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_question"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		writeSSE := func(evName string, payload any) {
			raw, _ := json.Marshal(payload)
			_, _ = w.Write([]byte("event: " + evName + "\ndata: " + string(raw) + "\n\n"))
			_ = rc.Flush()
		}

		mcpSrv := s.newMCPServer()
		agentCtx := withMCPGM(r.Context(), a)
		agentProto, agentURL, agentKey, agentModel := s.opsAgentConfig()
		gw := aigateway.NewSession(httpserver.TraceID(r.Context()), "opsagent_sse", a.Game, a.ID, s, b.RequestID, b.PlayerID, b.ActivityID, b.Question)
		res, runErr := opsagent.Run(agentCtx, opsagent.ModelConfig{
			Protocol: agentProto,
			URL:      agentURL,
			APIKey:   agentKey,
			Model:    agentModel,
			Sandbox:  s.Sandbox,
		}, mcpServiceAdapter{srv: mcpSrv}, opsagent.RunRequest{
			GameID:         a.Game,
			AdminID:        a.ID,
			Question:       b.Question,
			PlayerID:       strings.TrimSpace(b.PlayerID),
			RequestID:      strings.TrimSpace(b.RequestID),
			ActivityID:     strings.TrimSpace(b.ActivityID),
			AllowPropose:   b.AllowPropose,
			ProposalItem:   strings.TrimSpace(b.ProposalItem),
			ProposalAmount: b.ProposalAmount,
			Gateway:        gw,
			EventSink: func(ev opsagent.StreamEvent) {
				writeSSE(ev.Type, ev)
			},
		})
		if runErr != nil {
			writeSSE("error", map[string]string{"error": runErr.Error()})
			return
		}
		runID, now, err := s.persistOpsAgentRun(r.Context(), r, a, b.Question, res)
		if err != nil {
			writeSSE("error", map[string]string{"error": err.Error()})
			return
		}
		writeSSE("final", formatOpsAgentRunResponse(runID, a.Game, a.ID, b.Question, now, res))
	})

	s.handle(m, "GET "+base+"/runs", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(),
			"SELECT id,admin_id,question,mode,model,status,summary,root_cause,recommendation,proposal_request_id,steps_count,prompt_tokens,completion_tokens,duration_ms,created_at FROM ops_agent_runs WHERE game_id=? ORDER BY created_at DESC,id DESC LIMIT 50",
			a.Game,
		)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, admin, question, mode, model, status, summary, rootCause, rec, proposalID string
			var stepsCount, promptTok, compTok int
			var dur, ts int64
			if e = rows.Scan(&id, &admin, &question, &mode, &model, &status, &summary, &rootCause, &rec, &proposalID, &stepsCount, &promptTok, &compTok, &dur, &ts); e != nil {
				return nil, e
			}
			out = append(out, map[string]any{
				"run_id":              id,
				"admin_id":            admin,
				"question":            question,
				"mode":                mode,
				"model":               model,
				"real_ai":             mode == "llm",
				"status":              status,
				"summary":             summary,
				"root_cause":          rootCause,
				"recommendation":      rec,
				"proposal_request_id": proposalID,
				"steps_count":         stepsCount,
				"prompt_tokens":       promptTok,
				"completion_tokens":   compTok,
				"duration_ms":         dur,
				"created_at":          ts,
			})
		}
		return out, rows.Err()
	})

	s.handle(m, "GET "+base+"/runs/{run}", func(r *http.Request) (any, error) {
		a, e := s.gm(r, "read")
		if e != nil {
			return nil, e
		}
		runID := r.PathValue("run")
		var id, admin, question, mode, model, status, summary, rootCause, rec, proposalID string
		var stepsCount, promptTok, compTok int
		var dur, ts int64
		e = s.DB.QueryRowContext(r.Context(),
			"SELECT id,admin_id,question,mode,model,status,summary,root_cause,recommendation,proposal_request_id,steps_count,prompt_tokens,completion_tokens,duration_ms,created_at FROM ops_agent_runs WHERE game_id=? AND id=?",
			a.Game, runID,
		).Scan(&id, &admin, &question, &mode, &model, &status, &summary, &rootCause, &rec, &proposalID, &stepsCount, &promptTok, &compTok, &dur, &ts)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, bad(404, "run_not_found")
		}
		if e != nil {
			return nil, e
		}
		rows, e := s.DB.QueryContext(r.Context(),
			"SELECT step_index,tool_name,arguments_json,result_summary,is_error,duration_ms FROM ops_agent_steps WHERE run_id=? ORDER BY step_index ASC,id ASC",
			runID,
		)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		steps := []opsagent.StepTrace{}
		for rows.Next() {
			var st opsagent.StepTrace
			if e = rows.Scan(&st.StepIndex, &st.ToolName, &st.ArgumentsJSON, &st.ResultSummary, &st.IsError, &st.DurationMS); e != nil {
				return nil, e
			}
			steps = append(steps, st)
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		return map[string]any{
			"run_id":              id,
			"game_id":             a.Game,
			"admin_id":            admin,
			"question":            question,
			"mode":                mode,
			"model":               model,
			"real_ai":             mode == "llm",
			"status":              status,
			"summary":             summary,
			"root_cause":          rootCause,
			"recommendation":      rec,
			"proposal_request_id": proposalID,
			"steps_count":         stepsCount,
			"steps":               steps,
			"prompt_tokens":       promptTok,
			"completion_tokens":   compTok,
			"duration_ms":         dur,
			"created_at":          ts,
		}, nil
	})
}
