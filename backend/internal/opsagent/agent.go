// Package opsagent executes multi-turn AI diagnostic workflows over MCP tools.
package opsagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"miniverse/backend/internal/aigateway"
	"miniverse/backend/internal/mcp"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxSteps           = 6
	maxStepResultChars = 6000
)

// Error encodes an HTTP-mappable error returned by the Ops Agent runner.
type Error struct {
	Status int
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code }

func rewriteLoopbackForDocker(rawURL string) (string, bool) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "127.0.0.1" && host != "localhost" {
		return "", false
	}
	if port := u.Port(); port != "" {
		u.Host = "host.docker.internal:" + port
	} else {
		u.Host = "host.docker.internal"
	}
	return u.String(), true
}

func doUpstreamRequest(ctx context.Context, client *http.Client, sandbox bool, targetURL string, build func(u string) (*http.Request, error)) (*http.Response, error) {
	req, err := build(targetURL)
	if err != nil {
		return nil, &Error{Status: 503, Code: "ops_agent_configuration_invalid", Detail: err.Error()}
	}
	res, err := client.Do(req)
	if err == nil {
		return res, nil
	}
	if errors.Is(err, ErrSSRFBlocked) {
		return nil, &Error{Status: 400, Code: "ops_agent_ssrf_blocked", Detail: "restricted target address"}
	}
	if sandbox {
		if altURL, ok := rewriteLoopbackForDocker(targetURL); ok {
			if altReq, berr := build(altURL); berr == nil {
				if altRes, aerr := client.Do(altReq); aerr == nil {
					return altRes, nil
				} else if errors.Is(aerr, ErrSSRFBlocked) {
					return nil, &Error{Status: 400, Code: "ops_agent_ssrf_blocked", Detail: "restricted target address"}
				} else {
					return nil, &Error{Status: 502, Code: "ops_agent_unavailable", Detail: truncateRunes(aerr.Error(), 220)}
				}
			}
		}
	}
	return nil, &Error{Status: 502, Code: "ops_agent_unavailable", Detail: truncateRunes(err.Error(), 220)}
}

func upstreamStatusDetail(res *http.Response) string {
	snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	msg := strings.Join(strings.Fields(string(snippet)), " ")
	if msg == "" {
		return fmt.Sprintf("HTTP %d", res.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", res.StatusCode, truncateRunes(msg, 180))
}

// MCPClient abstracts in-process or remote MCP tool discovery and invocation.
type MCPClient interface {
	ListTools(ctx context.Context) ([]mcp.ToolSpec, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (text string, structured any, isError bool, err error)
}

type ModelConfig struct {
	Protocol string
	URL      string
	APIKey   string
	Model    string
	Sandbox  bool
}

// StreamEvent reports real-time progress for SSE diagnostic streaming.
type StreamEvent struct {
	Type    string `json:"type"`
	Step    int    `json:"step,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Status  string `json:"status,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

type RunRequest struct {
	GameID         string
	AdminID        string
	Question       string
	PlayerID       string
	RequestID      string
	ActivityID     string
	AllowPropose   bool
	ProposalItem   string
	ProposalAmount int64
	Gateway        *aigateway.Session
	EventSink      func(StreamEvent)
}

type StepTrace struct {
	StepIndex     int    `json:"step_index"`
	ToolName      string `json:"tool_name"`
	ArgumentsJSON string `json:"arguments_json"`
	ResultSummary string `json:"result_summary"`
	Structured    any    `json:"-"`
	IsError       bool   `json:"is_error"`
	DurationMS    int64  `json:"duration_ms"`
}

type RunResult struct {
	Mode              string      `json:"mode"`
	Model             string      `json:"model"`
	Status            string      `json:"status"`
	Summary           string      `json:"summary"`
	RootCause         string      `json:"root_cause"`
	Recommendation    string      `json:"recommendation"`
	EvidenceIDs       []string    `json:"evidence_ids,omitempty"`
	ProposalRequestID string      `json:"proposal_request_id"`
	Steps             []StepTrace `json:"steps"`
	PromptTokens      int         `json:"prompt_tokens"`
	CompletionTokens  int         `json:"completion_tokens"`
	DurationMS        int64       `json:"duration_ms"`
}

var hexRequestIDRe = regexp.MustCompile(`\b[a-f0-9]{24}\b`)

const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
	anthropicVersion  = "2023-06-01"
)

// NormalizeProtocol resolves empty/"auto" based on endpoint URL, or validates explicit openai/anthropic values.
func NormalizeProtocol(raw, endpoint string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		lower := strings.ToLower(strings.TrimSpace(endpoint))
		if strings.Contains(lower, "anthropic.com") || strings.Contains(lower, "/messages") {
			return ProtocolAnthropic, nil
		}
		return ProtocolOpenAI, nil
	case ProtocolOpenAI:
		return ProtocolOpenAI, nil
	case ProtocolAnthropic, "claude":
		return ProtocolAnthropic, nil
	default:
		return "", &Error{Status: 400, Code: "invalid_ops_agent_protocol"}
	}
}

// NormalizeEndpointURL appends standard OpenAI chat-completions suffixes when given a bare base URL.
func NormalizeEndpointURL(raw string) string {
	return NormalizeEndpointURLForProtocol(raw, ProtocolOpenAI)
}

// NormalizeEndpointURLForProtocol appends the default path suffix for the selected LLM protocol when given a bare base URL.
func NormalizeEndpointURLForProtocol(raw, protocol string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	cleanPath := strings.TrimRight(u.Path, "/")
	if protocol == ProtocolAnthropic {
		switch {
		case cleanPath == "":
			u.Path = "/v1/messages"
		case strings.HasSuffix(cleanPath, "/v1"):
			u.Path = cleanPath + "/messages"
		}
		return u.String()
	}
	switch {
	case cleanPath == "":
		u.Path = "/v1/chat/completions"
	case strings.HasSuffix(cleanPath, "/v1"):
		u.Path = cleanPath + "/chat/completions"
	}
	return u.String()
}

// ValidateModelConfig checks whether cfg points to a valid external LLM endpoint and returns the normalized URL & protocol.
func ValidateModelConfig(cfg ModelConfig) (string, string, error) {
	proto, err := NormalizeProtocol(cfg.Protocol, cfg.URL)
	if err != nil {
		return "", "", err
	}
	endpoint := NormalizeEndpointURLForProtocol(cfg.URL, proto)
	if endpoint == "" {
		return "", "", &Error{Status: 400, Code: "invalid_ops_agent_url"}
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(cfg.Sandbox && u.Scheme == "http")) {
		return "", "", &Error{Status: 400, Code: "invalid_ops_agent_url"}
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return "", "", &Error{Status: 400, Code: "ops_agent_model_required"}
	}
	return endpoint, proto, nil
}

// DeriveModelsURL computes the /models listing endpoint from a chat/messages endpoint or base URL.
func DeriveModelsURL(raw, protocol string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	cleanPath := strings.TrimRight(u.Path, "/")
	switch {
	case cleanPath == "":
		u.Path = "/v1/models"
	case strings.HasSuffix(cleanPath, "/models"):
		u.Path = cleanPath
	case strings.HasSuffix(cleanPath, "/chat/completions"):
		u.Path = strings.TrimSuffix(cleanPath, "/chat/completions") + "/models"
	case strings.HasSuffix(cleanPath, "/messages"):
		u.Path = strings.TrimSuffix(cleanPath, "/messages") + "/models"
	case strings.HasSuffix(cleanPath, "/v1"):
		u.Path = cleanPath + "/models"
	default:
		u.Path = cleanPath + "/models"
	}
	_ = protocol
	return u.String()
}

// FetchModels queries the upstream OpenAI or Anthropic /models endpoint and returns sorted model IDs.
func FetchModels(ctx context.Context, cfg ModelConfig) ([]string, int64, error) {
	proto, err := NormalizeProtocol(cfg.Protocol, cfg.URL)
	if err != nil {
		return nil, 0, err
	}
	modelsURL := DeriveModelsURL(cfg.URL, proto)
	u, err := url.Parse(modelsURL)
	if err != nil || modelsURL == "" || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(cfg.Sandbox && u.Scheme == "http")) {
		return nil, 0, &Error{Status: 400, Code: "invalid_ops_agent_url"}
	}

	started := time.Now()
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	httpClient := NewSafeHTTPClient(12*time.Second, cfg.Sandbox)
	res, err := doUpstreamRequest(reqCtx, httpClient, cfg.Sandbox, modelsURL, func(target string) (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Accept", "application/json")
		if proto == ProtocolAnthropic {
			req.Header.Set("anthropic-version", anthropicVersion)
			if strings.TrimSpace(cfg.APIKey) != "" {
				req.Header.Set("x-api-key", strings.TrimSpace(cfg.APIKey))
			}
		} else if strings.TrimSpace(cfg.APIKey) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
		}
		return req, nil
	})
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		return nil, elapsed, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, elapsed, &Error{Status: 502, Code: "ops_agent_unavailable", Detail: upstreamStatusDetail(res)}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, elapsed, &Error{Status: 502, Code: "ops_agent_invalid_response"}
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, elapsed, &Error{Status: 502, Code: "ops_agent_invalid_response"}
	}

	seen := make(map[string]struct{})
	out := make([]string, 0, len(parsed.Data)+len(parsed.Models))
	addModel := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, m := range parsed.Data {
		addModel(m.ID)
	}
	for _, m := range parsed.Models {
		if m.ID != "" {
			addModel(m.ID)
		} else {
			addModel(m.Name)
		}
	}
	sort.Strings(out)
	if len(out) > 200 {
		out = out[:200]
	}
	return out, elapsed, nil
}

// TestConnection performs a minimal chat/messages call to verify endpoint, key, protocol, and model reachability.
func TestConnection(ctx context.Context, cfg ModelConfig) (string, int64, error) {
	endpoint, proto, err := ValidateModelConfig(cfg)
	if err != nil {
		return "", 0, err
	}
	cfg.URL = endpoint
	cfg.Protocol = proto
	started := time.Now()
	httpClient := NewSafeHTTPClient(12*time.Second, cfg.Sandbox)
	if proto == ProtocolAnthropic {
		text, _, _, err := callAnthropicMessages(ctx, httpClient, cfg, "", []anthropicMessage{
			{Role: "user", Content: "Reply with OK."},
		}, nil)
		elapsed := time.Since(started).Milliseconds()
		if err != nil {
			return "", elapsed, err
		}
		return truncateRunes(strings.TrimSpace(text), 160), elapsed, nil
	}
	msg, _, err := callOpenAIChat(ctx, httpClient, cfg, []chatMessage{
		{Role: "user", Content: "Reply with OK."},
	}, nil)
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		return "", elapsed, err
	}
	return truncateRunes(strings.TrimSpace(msg.Content), 160), elapsed, nil
}

func Run(ctx context.Context, cfg ModelConfig, client MCPClient, req RunRequest) (RunResult, error) {
	started := time.Now()
	endpoint := strings.TrimSpace(cfg.URL)
	if endpoint == "" {
		if !cfg.Sandbox {
			return RunResult{}, &Error{Status: 503, Code: "ops_agent_not_configured"}
		}
		res, err := runPlaybook(ctx, client, req)
		res.DurationMS = time.Since(started).Milliseconds()
		return res, err
	}
	normalized, proto, err := ValidateModelConfig(cfg)
	if err != nil {
		return RunResult{}, &Error{Status: 503, Code: "ops_agent_configuration_invalid"}
	}
	cfg.URL = normalized
	cfg.Protocol = proto

	// Hook 1: LLMInputHook — prompt injection check & PII masking before outbound LLM call.
	if req.Gateway != nil {
		maskedQ, blockedReason := req.Gateway.HookLLMInput(ctx, req.Question)
		if blockedReason != "" {
			emitStream(req.EventSink, StreamEvent{Type: "hook_blocked", Status: blockedReason, Detail: "LLMInputHook blocked potential prompt injection"})
			return RunResult{
				Mode:           "llm_gateway_blocked",
				Model:          cfg.Model,
				Status:         blockedReason,
				Summary:        "请求被 AI 安全网关 LLMInput 钩子拦截。",
				RootCause:      "检测到越权指令或 Prompt 注入特征（" + blockedReason + "）。",
				Recommendation: "请移除绕过审批或系统提示词覆盖语句后重试。",
				Steps:          []StepTrace{},
				DurationMS:     time.Since(started).Milliseconds(),
			}, nil
		}
		req.Question = maskedQ
	}

	var res RunResult
	if proto == ProtocolAnthropic {
		res, err = runAnthropicLLM(ctx, cfg, client, req)
	} else {
		res, err = runLLM(ctx, cfg, client, req)
	}
	if err == nil && req.Gateway != nil {
		// Hook 4: LLMOutputHook — groundedness verification + PII restoration.
		sum, rc, rec, downgraded, reason := req.Gateway.HookLLMOutput(ctx, res.Summary, res.RootCause, res.Recommendation, res.EvidenceIDs)
		if downgraded {
			emitStream(req.EventSink, StreamEvent{Type: "hook_downgrade", Status: reason, Detail: "LLMOutputHook downgraded ungrounded response to deterministic playbook"})
			pb, pbErr := runPlaybook(ctx, client, req)
			if pbErr == nil {
				pb.Status = "hallucination_downgraded"
				pb.PromptTokens += res.PromptTokens
				pb.CompletionTokens += res.CompletionTokens
				pb.Summary = "[网关降级:" + reason + "] " + pb.Summary
				pb.DurationMS = time.Since(started).Milliseconds()
				return pb, nil
			}
			res.Status = "hallucination_downgraded"
		} else {
			res.Summary, res.RootCause, res.Recommendation = sum, rc, rec
		}
	}
	res.DurationMS = time.Since(started).Milliseconds()
	return res, err
}

func emitStream(sink func(StreamEvent), ev StreamEvent) {
	if sink != nil {
		sink(ev)
	}
}

// SimpleTextMessage represents a single turn in ChatCompletions or Anthropic Messages.
type SimpleTextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CallSimpleText invokes OpenAI or Anthropic text completion through the shared SSRF-safe HTTP client.
func CallSimpleText(ctx context.Context, cfg ModelConfig, systemPrompt string, history []SimpleTextMessage, userPrompt string, maxTokens int) (string, int, int, error) {
	normalized, proto, err := ValidateModelConfig(cfg)
	if err != nil {
		return "", 0, 0, err
	}
	cfg.URL = normalized
	cfg.Protocol = proto
	if maxTokens <= 0 {
		maxTokens = 800
	}
	httpClient := NewSafeHTTPClient(15*time.Second, cfg.Sandbox)
	if proto == ProtocolAnthropic {
		msgs := make([]anthropicMessage, 0, len(history)+1)
		for _, h := range history {
			msgs = append(msgs, anthropicMessage{Role: h.Role, Content: h.Content})
		}
		msgs = append(msgs, anthropicMessage{Role: "user", Content: userPrompt})
		txt, _, usage, callErr := callAnthropicMessages(ctx, httpClient, cfg, systemPrompt, msgs, nil)
		return txt, usage.PromptTokens, usage.CompletionTokens, callErr
	}
	msgs := make([]chatMessage, 0, len(history)+2)
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: systemPrompt})
	}
	for _, h := range history {
		msgs = append(msgs, chatMessage{Role: h.Role, Content: h.Content})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: userPrompt})
	msg, usage, callErr := callOpenAIChat(ctx, httpClient, cfg, msgs, nil)
	return msg.Content, usage.PromptTokens, usage.CompletionTokens, callErr
}

func executeToolWithGateway(ctx context.Context, client MCPClient, allowedMap map[string]mcp.ToolSpec, gw *aigateway.Session, sink func(StreamEvent), stepNum int, toolName, argsText string) (truncatedOut string, structured any, isErr bool, durMS int64, loopBroken bool) {
	tStart := time.Now()
	emitStream(sink, StreamEvent{Type: "step_start", Step: stepNum, Tool: toolName, Detail: argsText})

	spec, ok := allowedMap[toolName]
	var outText string
	if !ok {
		isErr = true
		outText = `{"error":"tool_not_allowed"}`
	} else if gw != nil {
		// Hook 2: ToolPreInvokeHook — schema check, OPA policy, duplicate loop breaker.
		if allowed, reason := gw.HookToolPreInvoke(ctx, toolName, spec.InputSchema, json.RawMessage(argsText)); !allowed {
			isErr = true
			loopBroken = reason == "loop_circuit_broken"
			errJSON, _ := json.Marshal(map[string]string{"error": reason, "hook": "tool_pre_invoke"})
			outText = string(errJSON)
			emitStream(sink, StreamEvent{Type: "hook_blocked", Step: stepNum, Tool: toolName, Status: reason})
		}
	}

	if !isErr {
		var callErr error
		outText, structured, isErr, callErr = client.CallTool(ctx, toolName, json.RawMessage(argsText))
		if callErr != nil {
			isErr = true
			errJSON, _ := json.Marshal(map[string]string{"error": callErr.Error()})
			outText = string(errJSON)
		}
	}

	// Hook 3: ToolPostInvokeHook — PII masking & valid JSON compaction.
	if gw != nil {
		truncatedOut = gw.HookToolPostInvoke(ctx, toolName, outText, maxStepResultChars)
	} else {
		truncatedOut = truncateRunes(outText, maxStepResultChars)
	}
	durMS = time.Since(tStart).Milliseconds()
	emitStream(sink, StreamEvent{Type: "step_done", Step: stepNum, Tool: toolName, Status: map[bool]string{true: "error", false: "ok"}[isErr]})
	return truncatedOut, structured, isErr, durMS, loopBroken
}

func filterAllowedTools(all []mcp.ToolSpec, allowPropose bool) ([]mcp.ToolSpec, map[string]mcp.ToolSpec) {
	allowedList := make([]mcp.ToolSpec, 0, len(all))
	allowedMap := make(map[string]mcp.ToolSpec, len(all))
	for _, t := range all {
		if !t.ReadOnly && !allowPropose {
			continue
		}
		allowedList = append(allowedList, t)
		allowedMap[t.Name] = t
	}
	return allowedList, allowedMap
}

func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= maxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	b.WriteString("...(truncated)")
	return b.String()
}

func extractProposalID(toolName string, structured any, isError bool) string {
	if isError || !strings.HasPrefix(toolName, "propose_") {
		return ""
	}
	if m, ok := structured.(map[string]any); ok {
		if id, ok := m["request_id"].(string); ok {
			return id
		}
	}
	return ""
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

func runLLM(ctx context.Context, cfg ModelConfig, client MCPClient, req RunRequest) (RunResult, error) {
	allTools, err := client.ListTools(ctx)
	if err != nil {
		return RunResult{}, err
	}
	allowedList, allowedMap := filterAllowedTools(allTools, req.AllowPropose)

	openAITools := make([]map[string]any, 0, len(allowedList))
	for _, t := range allowedList {
		openAITools = append(openAITools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}

	reqCtxJSON, _ := json.Marshal(map[string]any{
		"game_id":         req.GameID,
		"player_id":       req.PlayerID,
		"request_id":      req.RequestID,
		"activity_id":     req.ActivityID,
		"allow_propose":   req.AllowPropose,
		"proposal_item":   req.ProposalItem,
		"proposal_amount": req.ProposalAmount,
	})
	systemPrompt := "你是 MiniVerse 游戏服务端智能运维与故障诊断 Agent。仅可基于 MCP 工具返回的真实观测数据得出结论，禁止编造日志、库存或健康状态。" +
		"若 allow_propose 为 true 且确实需要补偿或止损，只能调用 propose_* 工具创建 pending 审批单，并明确告知需要人类 reviewer 审核；绝不能声称变更已生效。" +
		"请优先使用 search_runbook 与 inspect_player_summary 做渐进式排查以降低上下文开销。" +
		"最后请只输出 JSON 对象（无 markdown 代码块），包含字段：summary、root_cause、recommendation、evidence_ids（字符串数组，列出引用的真实 request_id 或 error_code）。"

	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("上下文：%s\n问题：%s", string(reqCtxJSON), req.Question)},
	}

	res := RunResult{Mode: "llm", Model: cfg.Model, Status: "completed", Steps: []StepTrace{}}
	httpClient := NewSafeHTTPClient(15*time.Second, cfg.Sandbox)

	stepNum := 0
	for round := 0; round < MaxSteps; round++ {
		respMsg, usage, err := callOpenAIChat(ctx, httpClient, cfg, messages, openAITools)
		if err != nil {
			return res, err
		}
		res.PromptTokens += usage.PromptTokens
		res.CompletionTokens += usage.CompletionTokens

		if len(respMsg.ToolCalls) == 0 {
			parseFinalAnswer(respMsg.Content, &res)
			return res, nil
		}

		messages = append(messages, respMsg)
		loopBroken := false
		for _, tc := range respMsg.ToolCalls {
			if stepNum >= MaxSteps {
				break
			}
			stepNum++
			argsText := strings.TrimSpace(tc.Function.Arguments)
			if argsText == "" {
				argsText = "{}"
			}
			truncatedOut, structured, isErr, durMS, broke := executeToolWithGateway(ctx, client, allowedMap, req.Gateway, req.EventSink, stepNum, tc.Function.Name, argsText)
			res.Steps = append(res.Steps, StepTrace{
				StepIndex:     stepNum,
				ToolName:      tc.Function.Name,
				ArgumentsJSON: argsText,
				ResultSummary: truncatedOut,
				Structured:    structured,
				IsError:       isErr,
				DurationMS:    durMS,
			})
			if pid := extractProposalID(tc.Function.Name, structured, isErr); pid != "" {
				res.ProposalRequestID = pid
			}
			messages = append(messages, chatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    truncatedOut,
			})
			if broke {
				loopBroken = true
				break
			}
		}
		if loopBroken {
			res.Status = "loop_circuit_broken"
			res.Summary = "AI 安全网关检测到重复无效工具调用死循环，已主动熔断。"
			res.RootCause = "Agent 连续使用相同参数重复调用同一 MCP 工具，触发 ToolPreInvokeHook 死循环保护。"
			res.Recommendation = "请核对已采集的 MCP 步骤证据，或补充更明确的 player_id / request_id。"
			return res, nil
		}
		if stepNum >= MaxSteps {
			break
		}
	}

	// Ask for final summary without tools when step budget is exhausted.
	finalMsg, usage, err := callOpenAIChat(ctx, httpClient, cfg, messages, nil)
	if err == nil && strings.TrimSpace(finalMsg.Content) != "" {
		res.PromptTokens += usage.PromptTokens
		res.CompletionTokens += usage.CompletionTokens
		parseFinalAnswer(finalMsg.Content, &res)
	} else {
		res.Summary = "已达到最大工具调用步数上限，请查看已采集的 MCP 证据链。"
		res.RootCause = "诊断在达到步数限制前已完成部分信息采集。"
		res.Recommendation = "可根据上述步骤结果缩小范围（补充 player_id 或 request_id）再次排查。"
	}
	res.Status = "max_steps_reached"
	return res, nil
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func callOpenAIChat(ctx context.Context, client *http.Client, cfg ModelConfig, messages []chatMessage, tools []map[string]any) (chatMessage, chatUsage, error) {
	body := map[string]any{
		"model":       cfg.Model,
		"messages":    messages,
		"max_tokens":  900,
		"temperature": 0.1,
		"stream":      false,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	payload, _ := json.Marshal(body)
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := doUpstreamRequest(reqCtx, client, cfg.Sandbox, cfg.URL, func(target string) (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(reqCtx, http.MethodPost, target, bytes.NewReader(payload))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
		return req, nil
	})
	if err != nil {
		return chatMessage{}, chatUsage{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return chatMessage{}, chatUsage{}, &Error{Status: 502, Code: "ops_agent_unavailable", Detail: upstreamStatusDetail(res)}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return chatMessage{}, chatUsage{}, &Error{Status: 502, Code: "ops_agent_invalid_response"}
	}
	var out struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
		Usage chatUsage `json:"usage"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.Choices) == 0 {
		return chatMessage{}, chatUsage{}, &Error{Status: 502, Code: "ops_agent_invalid_response"}
	}
	return out.Choices[0].Message, out.Usage, nil
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func runAnthropicLLM(ctx context.Context, cfg ModelConfig, client MCPClient, req RunRequest) (RunResult, error) {
	allTools, err := client.ListTools(ctx)
	if err != nil {
		return RunResult{}, err
	}
	allowedList, allowedMap := filterAllowedTools(allTools, req.AllowPropose)

	claudeTools := make([]map[string]any, 0, len(allowedList))
	for _, t := range allowedList {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		claudeTools = append(claudeTools, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": schema,
		})
	}

	reqCtxJSON, _ := json.Marshal(map[string]any{
		"game_id":         req.GameID,
		"player_id":       req.PlayerID,
		"request_id":      req.RequestID,
		"activity_id":     req.ActivityID,
		"allow_propose":   req.AllowPropose,
		"proposal_item":   req.ProposalItem,
		"proposal_amount": req.ProposalAmount,
	})
	systemPrompt := "你是 MiniVerse 游戏服务端智能运维与故障诊断 Agent。仅可基于 MCP 工具返回的真实观测数据得出结论，禁止编造日志、库存或健康状态。" +
		"若 allow_propose 为 true 且确实需要补偿或止损，只能调用 propose_* 工具创建 pending 审批单，并明确告知需要人类 reviewer 审核；绝不能声称变更已生效。" +
		"请优先使用 search_runbook 与 inspect_player_summary 做渐进式排查以降低上下文开销。" +
		"最后请只输出 JSON 对象（无 markdown 代码块），包含字段：summary、root_cause、recommendation、evidence_ids（字符串数组，列出引用的真实 request_id 或 error_code）。"

	messages := []anthropicMessage{
		{Role: "user", Content: fmt.Sprintf("上下文：%s\n问题：%s", string(reqCtxJSON), req.Question)},
	}

	res := RunResult{Mode: "llm", Model: cfg.Model, Status: "completed", Steps: []StepTrace{}}
	httpClient := NewSafeHTTPClient(15*time.Second, cfg.Sandbox)

	stepNum := 0
	for turn := 0; turn < MaxSteps; turn++ {
		text, blocks, usage, err := callAnthropicMessages(ctx, httpClient, cfg, systemPrompt, messages, claudeTools)
		if err != nil {
			return res, err
		}
		res.PromptTokens += usage.PromptTokens
		res.CompletionTokens += usage.CompletionTokens

		var toolUses []anthropicContentBlock
		for _, blk := range blocks {
			if blk.Type == "tool_use" {
				toolUses = append(toolUses, blk)
			}
		}
		if len(toolUses) == 0 {
			parseFinalAnswer(text, &res)
			return res, nil
		}

		// Preserve assistant blocks as-is so tool_use IDs match subsequent tool_result blocks.
		messages = append(messages, anthropicMessage{
			Role:    "assistant",
			Content: blocks,
		})

		toolResults := make([]map[string]any, 0, len(toolUses))
		loopBroken := false
		for _, tu := range toolUses {
			if stepNum >= MaxSteps {
				break
			}
			stepNum++
			rawArgs := tu.Input
			if len(bytes.TrimSpace(rawArgs)) == 0 {
				rawArgs = json.RawMessage("{}")
			}
			truncatedOut, structured, isErr, dur, broke := executeToolWithGateway(ctx, client, allowedMap, req.Gateway, req.EventSink, stepNum, tu.Name, string(rawArgs))
			res.Steps = append(res.Steps, StepTrace{
				StepIndex:     stepNum,
				ToolName:      tu.Name,
				ArgumentsJSON: string(rawArgs),
				ResultSummary: truncatedOut,
				Structured:    structured,
				IsError:       isErr,
				DurationMS:    dur,
			})
			if pid := extractProposalID(tu.Name, structured, isErr); pid != "" {
				res.ProposalRequestID = pid
			}
			toolResults = append(toolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": tu.ID,
				"content":     truncatedOut,
				"is_error":    isErr,
			})
			if broke {
				loopBroken = true
				break
			}
		}
		messages = append(messages, anthropicMessage{
			Role:    "user",
			Content: toolResults,
		})
		if loopBroken {
			res.Status = "loop_circuit_broken"
			res.Summary = "AI 安全网关检测到重复无效工具调用死循环，已主动熔断。"
			res.RootCause = "Agent 连续使用相同参数重复调用同一 MCP 工具，触发 ToolPreInvokeHook 死循环保护。"
			res.Recommendation = "请核对已采集的 MCP 步骤证据，或补充更明确的 player_id / request_id。"
			return res, nil
		}
		if stepNum >= MaxSteps {
			break
		}
	}

	// Ask for final summary without tools when step budget is exhausted.
	finalText, _, usage, err := callAnthropicMessages(ctx, httpClient, cfg, systemPrompt, messages, nil)
	if err == nil && strings.TrimSpace(finalText) != "" {
		res.PromptTokens += usage.PromptTokens
		res.CompletionTokens += usage.CompletionTokens
		parseFinalAnswer(finalText, &res)
	} else {
		res.Summary = "已达到最大工具调用步数上限，请查看已采集的 MCP 证据链。"
		res.RootCause = "诊断在达到步数限制前已完成部分信息采集。"
		res.Recommendation = "可根据上述步骤结果缩小范围（补充 player_id 或 request_id）再次排查。"
	}
	res.Status = "max_steps_reached"
	return res, nil
}

func callAnthropicMessages(ctx context.Context, client *http.Client, cfg ModelConfig, systemPrompt string, messages []anthropicMessage, tools []map[string]any) (string, []anthropicContentBlock, chatUsage, error) {
	body := map[string]any{
		"model":       cfg.Model,
		"max_tokens":  1024,
		"temperature": 0.1,
		"messages":    messages,
	}
	if strings.TrimSpace(systemPrompt) != "" {
		body["system"] = systemPrompt
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	payload, _ := json.Marshal(body)
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := doUpstreamRequest(reqCtx, client, cfg.Sandbox, cfg.URL, func(target string) (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(reqCtx, http.MethodPost, target, bytes.NewReader(payload))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("anthropic-version", anthropicVersion)
		if cfg.APIKey != "" {
			req.Header.Set("x-api-key", cfg.APIKey)
		}
		return req, nil
	})
	if err != nil {
		return "", nil, chatUsage{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", nil, chatUsage{}, &Error{Status: 502, Code: "ops_agent_unavailable", Detail: upstreamStatusDetail(res)}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", nil, chatUsage{}, &Error{Status: 502, Code: "ops_agent_invalid_response"}
	}
	var out struct {
		Content []anthropicContentBlock `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.Content) == 0 {
		return "", nil, chatUsage{}, &Error{Status: 502, Code: "ops_agent_invalid_response"}
	}
	var textParts []string
	for _, blk := range out.Content {
		if blk.Type == "text" && strings.TrimSpace(blk.Text) != "" {
			textParts = append(textParts, blk.Text)
		}
	}
	usage := chatUsage{
		PromptTokens:     out.Usage.InputTokens,
		CompletionTokens: out.Usage.OutputTokens,
	}
	return strings.Join(textParts, "\n"), out.Content, usage, nil
}

func parseFinalAnswer(raw string, res *RunResult) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var parsed struct {
		Summary        string   `json:"summary"`
		RootCause      string   `json:"root_cause"`
		Recommendation string   `json:"recommendation"`
		EvidenceIDs    []string `json:"evidence_ids"`
	}
	if json.Unmarshal([]byte(cleaned), &parsed) == nil && (parsed.Summary != "" || parsed.RootCause != "") {
		res.Summary = parsed.Summary
		res.RootCause = parsed.RootCause
		res.Recommendation = parsed.Recommendation
		res.EvidenceIDs = parsed.EvidenceIDs
		return
	}
	res.Summary = truncateRunes(cleaned, 1500)
	if res.RootCause == "" {
		res.RootCause = "详见摘要分析。"
	}
	if res.Recommendation == "" {
		res.Recommendation = "请结合 MCP 工具调用证据确认后续处置。"
	}
}

func runPlaybook(ctx context.Context, client MCPClient, req RunRequest) (RunResult, error) {
	allTools, err := client.ListTools(ctx)
	if err != nil {
		return RunResult{}, err
	}
	_, allowedMap := filterAllowedTools(allTools, req.AllowPropose)

	res := RunResult{
		Mode:   "deterministic_playbook",
		Model:  "rule-playbook-v1",
		Status: "completed",
		Steps:  []StepTrace{},
	}
	stepNum := 0
	execTool := func(name string, args any) (map[string]any, bool) {
		if _, ok := allowedMap[name]; !ok || stepNum >= MaxSteps {
			return nil, false
		}
		stepNum++
		rawArgs, _ := json.Marshal(args)
		tStart := time.Now()
		text, structured, isErr, callErr := client.CallTool(ctx, name, rawArgs)
		if callErr != nil {
			isErr = true
			text = callErr.Error()
		}
		res.Steps = append(res.Steps, StepTrace{
			StepIndex:     stepNum,
			ToolName:      name,
			ArgumentsJSON: string(rawArgs),
			ResultSummary: truncateRunes(text, maxStepResultChars),
			Structured:    structured,
			IsError:       isErr,
			DurationMS:    time.Since(tStart).Milliseconds(),
		})
		if pid := extractProposalID(name, structured, isErr); pid != "" {
			res.ProposalRequestID = pid
		}
		if isErr {
			return nil, false
		}
		m, _ := structured.(map[string]any)
		return m, true
	}

	qLower := strings.ToLower(req.Question)
	reqID := strings.TrimSpace(req.RequestID)
	if reqID == "" {
		reqID = hexRequestIDRe.FindString(req.Question)
	}
	playerID := strings.TrimSpace(req.PlayerID)
	activityID := strings.TrimSpace(req.ActivityID)

	var healthData, playerData, rewardData, traceData, logData, auditData map[string]any

	if reqID != "" {
		traceData, _ = execTool("trace_request", map[string]any{"game_id": req.GameID, "request_id": reqID})
		logData, _ = execTool("query_ops_logs", map[string]any{"game_id": req.GameID, "request_id": reqID, "limit": 20})
	}

	if playerID != "" {
		playerData, _ = execTool("diagnose_player", map[string]any{"game_id": req.GameID, "player_id": playerID})
		if activityID != "" || containsAny(qLower, "奖励", "道具", "邮件", "活动", "未到账", "补偿", "reward", "mail", "item", "activity") {
			rewardData, _ = execTool("explain_reward_issue", map[string]any{"game_id": req.GameID, "player_id": playerID, "activity_id": activityID})
		}
		if logData == nil {
			logData, _ = execTool("query_ops_logs", map[string]any{"game_id": req.GameID, "player_id": playerID, "limit": 15})
		}
	}

	if (playerID == "" && reqID == "") || containsAny(qLower, "健康", "服务", "宕机", "延迟", "报错", "503", "500", "rulecore", "状态", "连接池", "死锁", "限流", "health", "error", "outage") {
		healthData, _ = execTool("inspect_service_health", map[string]any{"game_id": req.GameID})
		if logData == nil {
			logData, _ = execTool("query_ops_logs", map[string]any{"game_id": req.GameID, "limit": 15})
		}
	}

	if containsAny(qLower, "审计", "审批", "工单", "audit", "request") {
		auditData, _ = execTool("query_audit_trail", map[string]any{"game_id": req.GameID, "limit": 15})
	}

	if req.AllowPropose {
		if activityID != "" && (containsAny(qLower, "停用", "停止", "关闭", "止损", "stop") || req.ProposalItem == "") {
			execTool("propose_activity_stop", map[string]any{
				"game_id":     req.GameID,
				"activity_id": activityID,
				"reason":      "排查发现异常需人工复核停用活动：" + req.Question,
			})
		} else if playerID != "" && strings.TrimSpace(req.ProposalItem) != "" && req.ProposalAmount > 0 {
			execTool("propose_reward_compensation", map[string]any{
				"game_id":   req.GameID,
				"player_id": playerID,
				"item":      strings.TrimSpace(req.ProposalItem),
				"amount":    req.ProposalAmount,
				"reason":    "排障核实后生成受控补发提案：" + req.Question,
			})
		}
	}

	summarizePlaybook(&res, req, reqID, healthData, playerData, rewardData, traceData, logData, auditData)
	return res, nil
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func summarizePlaybook(res *RunResult, req RunRequest, reqID string, healthData, playerData, rewardData, traceData, logData, auditData map[string]any) {
	var summaries, causes, recs []string

	if healthData != nil {
		dbReady, _ := healthData["db_ready"].(bool)
		ruleOK, _ := healthData["rulecore_reachable"].(bool)
		var ruleErrors int64
		var winReqs int
		if tel, ok := healthData["telemetry"].(map[string]any); ok {
			if v, ok := tel["rulecore_errors"].(int64); ok {
				ruleErrors = v
			} else if vf, ok := tel["rulecore_errors"].(float64); ok {
				ruleErrors = int64(vf)
			}
			if v, ok := tel["window_5m_requests"].(int); ok {
				winReqs = v
			} else if vf, ok := tel["window_5m_requests"].(float64); ok {
				winReqs = int(vf)
			}
		}
		summaries = append(summaries, fmt.Sprintf("服务健康检查：db_ready=%v，rulecore_reachable=%v，RuleCore 累计错误=%d，近 5 分钟请求=%d。", dbReady, ruleOK, ruleErrors, winReqs))
		if !dbReady {
			causes = append(causes, "MySQL 数据库未就绪或连接异常。")
			recs = append(recs, "优先检查 MySQL 容器与连接配置，确认 /ready 探针恢复。")
		}
		if !ruleOK {
			causes = append(causes, fmt.Sprintf("C++ RuleCore gRPC 服务不可达（rulecore_reachable=false，累计 RPC 错误 %d），会导致认证回放榜单校验返回 503，但不影响普通云存档与活动。", ruleErrors))
			recs = append(recs, "检查 rulecore 服务状态与 RULECORE_ADDR 连通性并重启恢复。")
		}
	}

	if traceData != nil {
		evCount := sliceLen(traceData["ops_events"])
		auditCount := sliceLen(traceData["audits"])
		summaries = append(summaries, fmt.Sprintf("Request ID(%s) 关联到 %d 条异常事件、%d 条审计记录。", reqID, evCount, auditCount))
		if evCount > 0 {
			if ev0 := firstMap(traceData["ops_events"]); ev0 != nil {
				causes = append(causes, fmt.Sprintf("请求 %s 在 %s %s 触发状态 %v（错误码 %v）。", reqID, ev0["method"], ev0["path"], ev0["status_code"], ev0["error_code"]))
			}
		}
	}

	if rewardData != nil {
		if lc, ok := rewardData["likely_causes"].([]string); ok && len(lc) > 0 {
			causes = append(causes, lc...)
			summaries = append(summaries, fmt.Sprintf("奖励排查识别出 %d 项线索（%s）。", len(lc), strings.Join(lc, "；")))
		} else {
			summaries = append(summaries, "已完成奖励链路排查（活动、事件、进度、发奖、邮件与库存）。")
		}
	} else if playerData != nil {
		summaries = append(summaries, fmt.Sprintf("已聚合玩家 %s（%v）存档、邮件、库存、事件与异常记录。", req.PlayerID, playerData["name"]))
	}

	if logData != nil {
		c := sliceLen(logData["items"])
		summaries = append(summaries, fmt.Sprintf("检索到 %d 条关联业务异常日志。", c))
		if len(causes) == 0 && c > 0 {
			if ev0 := firstMap(logData["items"]); ev0 != nil {
				causes = append(causes, fmt.Sprintf("最近异常：%s %s -> %v (%v)。", ev0["method"], ev0["path"], ev0["status_code"], ev0["error_code"]))
			}
		}
	}

	if auditData != nil {
		summaries = append(summaries, fmt.Sprintf("已核对 %d 条审计日志与 %d 条审批工单。", sliceLen(auditData["audits"]), sliceLen(auditData["requests"])))
	}

	if res.ProposalRequestID != "" {
		recs = append(recs, fmt.Sprintf("已生成待审工单 %s（state=pending），请由另一位具备 approve 权限的审核人核对后批准执行。", res.ProposalRequestID))
	}
	if len(summaries) == 0 {
		summaries = append(summaries, "已完成多维度 MCP 诊断采集。")
	}
	if len(causes) == 0 {
		causes = append(causes, "当前检查范围内未发现阻断性异常；若仍有问题请提供具体 player_id 或 X-Request-ID 进一步缩小范围。")
	}
	if len(recs) == 0 {
		recs = append(recs, "请核对上述 MCP 工具返回的原始数据；如需补偿或停用活动，可开启受控提案并交由人工审批。")
	}

	res.Summary = strings.Join(summaries, " ")
	res.RootCause = strings.Join(causes, " ")
	res.Recommendation = strings.Join(recs, " ")
}

func sliceLen(v any) int {
	switch x := v.(type) {
	case []any:
		return len(x)
	case []map[string]any:
		return len(x)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	var arr []any
	if json.Unmarshal(raw, &arr) == nil {
		return len(arr)
	}
	return 0
}

func firstMap(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		return arr[0]
	}
	return nil
}
