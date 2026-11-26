package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"miniverse/backend/internal/mcp"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type mcpGMKey struct{}

func withMCPGM(ctx context.Context, gm GM) context.Context {
	return context.WithValue(ctx, mcpGMKey{}, gm)
}

func mcpGMFromCtx(ctx context.Context) (GM, bool) {
	gm, ok := ctx.Value(mcpGMKey{}).(GM)
	return gm, ok && gm.ID != ""
}

func sanitizeAIReason(prefix, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 3 {
		return "", bad(400, "reason_required")
	}
	if !strings.HasPrefix(trimmed, prefix) {
		trimmed = prefix + " " + trimmed
	}
	if len(trimmed) <= 250 {
		return trimmed, nil
	}
	limit := 250
	for limit > 0 && !utf8.ValidString(trimmed[:limit]) {
		limit--
	}
	return trimmed[:limit], nil
}

func (s *Service) newMCPServer() *mcp.Server {
	srv := mcp.NewServer("miniverse-ops-mcp", "1.0.0", func(ctx context.Context, tool mcp.ToolSpec) bool {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return false
		}
		if tool.ReadOnly {
			return true
		}
		// Only admin and operator may propose pending changes; reviewer and support cannot.
		return gm.Role == "admin" || gm.Role == "operator"
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "inspect_service_health",
		Description: "检查 API、MySQL 数据库连接池、迁移版本、近 5 分钟请求/错误率/P95 延迟、死锁重试、限流与 C++ RuleCore 实时连通性。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"game_id": map[string]any{"type": "string", "description": "可选，指定游戏 ID 时顺带核验当前账号对该游戏的访问权限"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID string `json:"game_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil {
			return nil, bad(400, "invalid_arguments")
		}
		if strings.TrimSpace(b.GameID) != "" {
			if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
				return nil, err
			}
		}
		return s.inspectServiceHealth(ctx), nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "diagnose_player",
		Description: "按 game_id 和 player_id 聚合查询玩家档案、云存档、活动进度、发奖流水、邮箱状态、背包库存、近期行为事件、排行榜成绩、回放记录与近期异常日志。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id", "player_id"},
			"properties": map[string]any{
				"game_id":   map[string]any{"type": "string"},
				"player_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID   string `json:"game_id"`
			PlayerID string `json:"player_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" || strings.TrimSpace(b.PlayerID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
			return nil, err
		}
		return s.playerDiagnostics(ctx, b.GameID, b.PlayerID)
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "query_ops_logs",
		Description: "按游戏、玩家、request_id、错误码或分类检索运行期业务异常日志（ops_events）。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id"},
			"properties": map[string]any{
				"game_id":    map[string]any{"type": "string"},
				"player_id":  map[string]any{"type": "string"},
				"request_id": map[string]any{"type": "string"},
				"error_code": map[string]any{"type": "string"},
				"category":   map[string]any{"type": "string"},
				"since":      map[string]any{"type": "integer"},
				"limit":      map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID    string `json:"game_id"`
			PlayerID  string `json:"player_id"`
			RequestID string `json:"request_id"`
			ErrorCode string `json:"error_code"`
			Category  string `json:"category"`
			Since     int64  `json:"since"`
			Limit     int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
			return nil, err
		}
		limit := b.Limit
		if limit <= 0 || limit > 100 {
			limit = 30
		}
		events, err := s.queryOpsEvents(ctx, b.GameID, b.PlayerID, b.RequestID, b.ErrorCode, b.Category, b.Since, limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": events, "count": len(events)}, nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "trace_request",
		Description: "根据 X-Request-ID 横向关联指定游戏下的异常事件（ops_events）与 GM 操作审计记录（audits）。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id", "request_id"},
			"properties": map[string]any{
				"game_id":    map[string]any{"type": "string"},
				"request_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID    string `json:"game_id"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" || strings.TrimSpace(b.RequestID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
			return nil, err
		}
		return s.traceRequestByID(ctx, b.GameID, b.RequestID)
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "explain_reward_issue",
		Description: "专项排查“玩家反馈完成活动/任务未收到道具奖励”的根因，串联活动开放时间窗、事件上报、进度、发奖流水、邮件过期/领取/删除与库存入账。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id", "player_id"},
			"properties": map[string]any{
				"game_id":     map[string]any{"type": "string"},
				"player_id":   map[string]any{"type": "string"},
				"activity_id": map[string]any{"type": "string", "description": "可选，不传则检查该游戏所有活动"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID     string `json:"game_id"`
			PlayerID   string `json:"player_id"`
			ActivityID string `json:"activity_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" || strings.TrimSpace(b.PlayerID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
			return nil, err
		}
		return s.explainRewardIssue(ctx, b.GameID, b.PlayerID, b.ActivityID)
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "query_audit_trail",
		Description: "查询指定游戏的近期 GM 审计日志与审批工单状态。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id"},
			"properties": map[string]any{
				"game_id": map[string]any{"type": "string"},
				"action":  map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID string `json:"game_id"`
			Action string `json:"action"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
			return nil, err
		}
		limit := b.Limit
		if limit <= 0 || limit > 100 {
			limit = 30
		}
		auditQuery := "SELECT id,admin_id,action,reason,details,created_at FROM audits WHERE game_id=?"
		auditArgs := []any{b.GameID}
		if b.Action != "" {
			auditQuery += " AND action=?"
			auditArgs = append(auditArgs, b.Action)
		}
		auditQuery += " ORDER BY id DESC LIMIT ?"
		auditArgs = append(auditArgs, limit)
		audits, err := s.queryRowsGeneric(ctx, auditQuery, auditArgs...)
		if err != nil {
			return nil, err
		}
		reqs, err := s.queryRowsGeneric(ctx, "SELECT id,kind,creator,COALESCE(reviewer,'') AS reviewer,state,reason,created_at FROM gm_requests WHERE game_id=? ORDER BY created_at DESC,id LIMIT ?", b.GameID, limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"audits": audits, "requests": reqs}, nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "inspect_mq_and_jobs",
		Description: "查询 Transactional Outbox 事件积压/死信(DLQ)状态与大批量发奖任务(batch_grant_jobs)执行进度。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id"},
			"properties": map[string]any{
				"game_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID string `json:"game_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
			return nil, err
		}
		outboxStats, err := s.queryRowsGeneric(ctx, "SELECT status,COUNT(*) AS cnt FROM outbox_events GROUP BY status")
		if err != nil {
			return nil, err
		}
		jobs, err := s.queryRowsGeneric(ctx, "SELECT id,request_id,item,amount,total_players,succeeded_players,failed_players,state,last_error,created_at FROM batch_grant_jobs WHERE game_id=? ORDER BY created_at DESC LIMIT 20", b.GameID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"outbox_stats": outboxStats, "batch_jobs": jobs}, nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "search_runbook",
		Description: "渐进式检索运维 Runbook 知识库分块（BM25/关键词重排），按需返回最相关的 Top-K 排障指引以节省上下文 Token。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"top_k": map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		if _, ok := mcpGMFromCtx(ctx); !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			Query string `json:"query"`
			TopK  int    `json:"top_k"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.Query) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if b.TopK <= 0 || b.TopK > 5 {
			b.TopK = 2
		}
		return map[string]any{"chunks": searchRunbookChunks(b.Query, b.TopK)}, nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "inspect_player_summary",
		Description: "轻量级渐进式玩家摘要：仅返回玩家名称、未领/过期邮件计数、库存种类数与最近 3 条异常错误码，避免直接调用全量 diagnose_player 导致上下文膨胀。",
		ReadOnly:    true,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id", "player_id"},
			"properties": map[string]any{
				"game_id":   map[string]any{"type": "string"},
				"player_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID   string `json:"game_id"`
			PlayerID string `json:"player_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" || strings.TrimSpace(b.PlayerID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		if _, err := s.authorizeGMGame(ctx, gm, b.GameID, "read"); err != nil {
			return nil, err
		}
		var name string
		if err := s.DB.QueryRowContext(ctx, "SELECT name FROM profiles WHERE game_id=? AND player_id=?", b.GameID, b.PlayerID).Scan(&name); err != nil {
			return nil, bad(404, "player_not_found")
		}
		var unclaimed, expired int64
		now := time.Now().Unix()
		_ = s.DB.QueryRowContext(ctx, "SELECT COALESCE(SUM(claimed=FALSE AND expires_at>?),0),COALESCE(SUM(claimed=FALSE AND expires_at<=?),0) FROM mail WHERE game_id=? AND player_id=?", now, now, b.GameID, b.PlayerID).Scan(&unclaimed, &expired)
		opsEvents, _ := s.queryOpsEvents(ctx, b.GameID, b.PlayerID, "", "", "", 0, 3)
		return map[string]any{
			"game_id":           b.GameID,
			"player_id":         b.PlayerID,
			"name":              name,
			"unclaimed_mails":   unclaimed,
			"expired_mails":     expired,
			"recent_ops_events": opsEvents,
		}, nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "propose_reward_compensation",
		Description: "受控写操作：为单个玩家创建一笔状态为 pending 的道具补发审批单（必须由另一位人类 reviewer 审核批准后才会发奖，AI 无权直接发奖或自审）。",
		ReadOnly:    false,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id", "player_id", "item", "amount", "reason"},
			"properties": map[string]any{
				"game_id":    map[string]any{"type": "string"},
				"player_id":  map[string]any{"type": "string"},
				"item":       map[string]any{"type": "string"},
				"amount":     map[string]any{"type": "integer", "minimum": 1, "maximum": 1000000},
				"reason":     map[string]any{"type": "string", "minLength": 3, "maxLength": 240},
				"request_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID    string `json:"game_id"`
			PlayerID  string `json:"player_id"`
			Item      string `json:"item"`
			Amount    int64  `json:"amount"`
			Reason    string `json:"reason"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" || strings.TrimSpace(b.PlayerID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		authGM, err := s.authorizeGMGame(ctx, gm, b.GameID, "write")
		if err != nil {
			return nil, err
		}
		reason, err := sanitizeAIReason("[AI-MCP]", b.Reason)
		if err != nil {
			return nil, err
		}
		reqID := strings.TrimSpace(b.RequestID)
		if reqID == "" {
			reqID = "mcp-rw-" + random()[:16]
		}
		payload, _ := json.Marshal(RewardRequest{
			Players: []string{b.PlayerID},
			Item:    b.Item,
			Amount:  b.Amount,
		})
		createdID, err := s.createGMRequest(ctx, authGM, reqID, "reward", reason, payload)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"request_id":              createdID,
			"kind":                    "reward",
			"state":                   "pending",
			"creator":                 authGM.ID,
			"requires_human_approval": true,
		}, nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "propose_activity_stop",
		Description: "受控写操作：创建一笔状态为 pending 的“停用异常活动”审批单，必须经人类 reviewer 批准后生效。",
		ReadOnly:    false,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id", "activity_id", "reason"},
			"properties": map[string]any{
				"game_id":     map[string]any{"type": "string"},
				"activity_id": map[string]any{"type": "string"},
				"reason":      map[string]any{"type": "string", "minLength": 3, "maxLength": 240},
				"request_id":  map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID     string `json:"game_id"`
			ActivityID string `json:"activity_id"`
			Reason     string `json:"reason"`
			RequestID  string `json:"request_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" || strings.TrimSpace(b.ActivityID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		authGM, err := s.authorizeGMGame(ctx, gm, b.GameID, "write")
		if err != nil {
			return nil, err
		}
		reason, err := sanitizeAIReason("[AI-MCP]", b.Reason)
		if err != nil {
			return nil, err
		}
		reqID := strings.TrimSpace(b.RequestID)
		if reqID == "" {
			reqID = "mcp-stop-" + random()[:16]
		}
		payload, _ := json.Marshal(ActivityStopRequest{ActivityID: b.ActivityID})
		createdID, err := s.createGMRequest(ctx, authGM, reqID, "activity_stop", reason, payload)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"request_id":              createdID,
			"kind":                    "activity_stop",
			"state":                   "pending",
			"creator":                 authGM.ID,
			"requires_human_approval": true,
		}, nil
	})

	srv.RegisterTool(mcp.ToolSpec{
		Name:        "propose_player_freeze",
		Description: "受控写操作：创建一笔状态为 pending 的“封禁/冻结异常玩家”审批单，必须经人类 reviewer 批准后生效。",
		ReadOnly:    false,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"game_id", "player_id", "reason"},
			"properties": map[string]any{
				"game_id":    map[string]any{"type": "string"},
				"player_id":  map[string]any{"type": "string"},
				"reason":     map[string]any{"type": "string", "minLength": 3, "maxLength": 240},
				"expires_at": map[string]any{"type": "integer"},
				"request_id": map[string]any{"type": "string"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (any, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return nil, bad(401, "gm_unauthorized")
		}
		var b struct {
			GameID    string `json:"game_id"`
			PlayerID  string `json:"player_id"`
			Reason    string `json:"reason"`
			ExpiresAt *int64 `json:"expires_at"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(args, &b); err != nil || strings.TrimSpace(b.GameID) == "" || strings.TrimSpace(b.PlayerID) == "" {
			return nil, bad(400, "invalid_arguments")
		}
		authGM, err := s.authorizeGMGame(ctx, gm, b.GameID, "write")
		if err != nil {
			return nil, err
		}
		reason, err := sanitizeAIReason("[AI-MCP]", b.Reason)
		if err != nil {
			return nil, err
		}
		reqID := strings.TrimSpace(b.RequestID)
		if reqID == "" {
			reqID = "mcp-ban-" + random()[:16]
		}
		payload, _ := json.Marshal(PlayerBanRequest{PlayerID: b.PlayerID, Reason: reason, ExpiresAt: b.ExpiresAt})
		createdID, err := s.createGMRequest(ctx, authGM, reqID, "player_ban", reason, payload)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"request_id":              createdID,
			"kind":                    "player_ban",
			"state":                   "pending",
			"creator":                 authGM.ID,
			"requires_human_approval": true,
		}, nil
	})

	srv.RegisterResource(mcp.ResourceSpec{
		URI:         "miniverse://health/snapshot",
		Name:        "Service Health Snapshot",
		Description: "当前进程健康度、数据库连接池、迁移版本、延迟分位数及 RuleCore 状态快照。",
		MimeType:    "application/json",
	}, func(ctx context.Context) (string, error) {
		raw, err := json.MarshalIndent(s.inspectServiceHealth(ctx), "", "  ")
		return string(raw), err
	})

	srv.RegisterResource(mcp.ResourceSpec{
		URI:         "miniverse://ops/runbook",
		Name:        "MiniVerse Ops Troubleshooting Runbook",
		Description: "MiniVerse 多游戏服务端标准排障指南、常见错误码与受控审批说明。",
		MimeType:    "text/markdown",
	}, func(_ context.Context) (string, error) {
		return opsRunbookMarkdown, nil
	})

	srv.RegisterResource(mcp.ResourceSpec{
		URI:         "miniverse://ops/games",
		Name:        "Authorized Games List",
		Description: "当前 GM 账号拥有访问权限的游戏列表。",
		MimeType:    "application/json",
	}, func(ctx context.Context) (string, error) {
		gm, ok := mcpGMFromCtx(ctx)
		if !ok {
			return "", bad(401, "gm_unauthorized")
		}
		rows, err := s.queryRowsGeneric(ctx, "SELECT g.id AS game_id,g.name,g.config_version FROM games g JOIN gm_access a ON a.game_id=g.id WHERE a.admin_id=? AND g.active=TRUE ORDER BY g.id", gm.ID)
		if err != nil {
			return "", err
		}
		raw, err := json.MarshalIndent(rows, "", "  ")
		return string(raw), err
	})

	return srv
}

const opsRunbookMarkdown = `# MiniVerse Ops Troubleshooting Runbook

## 1. 常见错误码与排障顺序
- **rulecore_unavailable (503)**: Go 到 C++20 RuleCore gRPC 服务超时或不可达。先调用 inspect_service_health 检查 rulecore_reachable 与 rulecore_errors，再查 query_ops_logs。普通云存档与活动不依赖 RuleCore。
- **revision_conflict (409)**: 玩家多端并发或离线旧存档覆盖。使用 diagnose_player 对比 saves.revision 与 save_history_recent，引导客户端拉取云端版本后解决冲突。
- **mail_expired (410) / 奖励未到账**: 使用 explain_reward_issue 依次核对活动时间窗（UTC）、client_allowed、事件去重、activity_progress、grants、mail 状态（未领/已过期/未领先删/已领入账）。
- **rate_limited (429) / auth_rate_limited (429)**: 单 IP 或账号触发频控（普通 600/min，登录 30/min）。通过 query_ops_logs 按 IP/接口统计突增来源。
- **transaction_busy (503)**: MySQL 死锁/锁超时重试耗尽，结合 telemetry.deadlock_retries 与近期并发写热点分析。
- **rulecore_circuit_open (503)**: RuleCore 连续失败触发三态熔断器快速失败保护，等待 3 秒冷却后自动进入 Half-Open 探测恢复。
- **outbox_dlq / batch_grant_jobs**: 大批量发奖由 Transactional Outbox + Kafka 异步分批执行，失败超限转入 DLQ，可通过 inspect_mq_and_jobs 核查。

## 2. 受控运维护栏
- AI / MCP 仅允许执行只读诊断工具，或生成 state=pending 的审批草案（propose_reward_compensation / propose_activity_stop / propose_player_freeze）。
- 任何写操作必须由另一位具备 approve 权限的管理员（reviewer / admin）核对证据后人工批准。
`

func searchRunbookChunks(query string, topK int) []map[string]any {
	type chunk struct {
		id    string
		title string
		body  string
	}
	chunks := []chunk{
		{id: "rulecore_unavailable", title: "rulecore_unavailable / rulecore_circuit_open (503)", body: "Go 到 C++20 RuleCore gRPC 服务超时、不可达或触发三态熔断器。调用 inspect_service_health 核查 rulecore_reachable 与 rulecore_circuit_opens，普通云存档与活动不受影响。"},
		{id: "revision_conflict", title: "revision_conflict (409)", body: "玩家多端并发或离线旧存档覆盖。使用 inspect_player_summary 或 diagnose_player 对比 saves.revision，引导客户端拉取云端版本解决冲突。"},
		{id: "reward_mail_expired", title: "mail_expired (410) / 奖励未到账", body: "使用 explain_reward_issue 依次核对活动时间窗（UTC）、client_allowed、事件去重、activity_progress、grants 与 mail 状态（未领/已过期/已领入账）。"},
		{id: "rate_limited", title: "rate_limited (429) / 限流采样", body: "触发单机或 Redis 两级限流（普通 600/min，登录 30/min）。系统采用 429 突发采样与异步微批写入 ops_events 保护 MySQL 连接池。"},
		{id: "outbox_dlq", title: "outbox / batch_grant_jobs 异步发奖", body: "大批量或全服补偿通过 Outbox + Kafka 异步分批执行。使用 inspect_mq_and_jobs 检查 outbox_events 待投递/DLQ 计数及 batch_grant_jobs 进度。"},
		{id: "hitl_guardrails", title: "AI 四钩子护栏与双人审批", body: "AI Gateway 在 LLMInput/ToolPreInvoke/ToolPostInvoke/LLMOutput 执行 PII 脱敏、Schema 校验、单笔额度限制（<=10000）与 Groundedness 校验，写操作仅生成 pending 工单。"},
	}
	qLower := strings.ToLower(query)
	terms := strings.Fields(strings.NewReplacer(",", " ", "，", " ", ":", " ", "_", " ").Replace(qLower))
	type scored struct {
		c     chunk
		score int
	}
	var ranked []scored
	for _, c := range chunks {
		hay := strings.ToLower(c.id + " " + c.title + " " + c.body)
		sc := 0
		if strings.Contains(hay, qLower) {
			sc += 5
		}
		for _, t := range terms {
			if len(t) >= 2 && strings.Contains(hay, t) {
				sc += 2
			}
		}
		ranked = append(ranked, scored{c: c, score: sc})
	}
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].score > ranked[i].score {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	out := make([]map[string]any, 0, min(topK, len(ranked)))
	for i := 0; i < len(ranked) && i < topK; i++ {
		out = append(out, map[string]any{
			"chunk_id": ranked[i].c.id,
			"title":    ranked[i].c.title,
			"score":    ranked[i].score,
			"content":  ranked[i].c.body,
		})
	}
	return out
}

func (s *Service) explainRewardIssue(ctx context.Context, game, player, activityID string) (map[string]any, error) {
	var profileName string
	if err := s.DB.QueryRowContext(ctx, "SELECT name FROM profiles WHERE game_id=? AND player_id=?", game, player).Scan(&profileName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map[string]any{
				"game_id":       game,
				"player_id":     player,
				"profile_found": false,
				"findings":      []string{"player_profile_missing"},
				"likely_causes": []string{"该玩家在当前游戏尚无档案，请核对 player_id 或确认其是否首次登录。"},
			}, nil
		}
		return nil, err
	}

	actQuery := "SELECT id,version,config,active FROM activities WHERE game_id=?"
	actArgs := []any{game}
	if strings.TrimSpace(activityID) != "" {
		actQuery += " AND id=?"
		actArgs = append(actArgs, strings.TrimSpace(activityID))
	}
	actQuery += " ORDER BY id"

	rows, err := s.DB.QueryContext(ctx, actQuery, actArgs...)
	if err != nil {
		return nil, err
	}
	type actRow struct {
		cfg    Activity
		active bool
	}
	acts := []actRow{}
	for rows.Next() {
		var id string
		var version int
		var raw []byte
		var active bool
		if err = rows.Scan(&id, &version, &raw, &active); err != nil {
			rows.Close()
			return nil, err
		}
		cfg, _ := readActivity(raw)
		if cfg.ID == "" {
			cfg.ID = id
		}
		if cfg.Version == 0 {
			cfg.Version = version
		}
		acts = append(acts, actRow{cfg: cfg, active: active})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	now := time.Now()
	nowUnix := now.Unix()
	findings := []string{}
	causes := []string{}

	if len(acts) == 0 {
		findings = append(findings, "no_matching_activity")
		causes = append(causes, "游戏中没有匹配的运营活动配置。")
	}

	progressList := []map[string]any{}
	for _, a := range acts {
		bucket := bucketFor(a.cfg, now)
		var prog int64
		err := s.DB.QueryRowContext(ctx, "SELECT progress FROM activity_progress WHERE game_id=? AND player_id=? AND activity_id=? AND version=? AND bucket=?", game, player, a.cfg.ID, a.cfg.Version, bucket).Scan(&prog)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		var evtCount int64
		if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE game_id=? AND player_id=? AND event_type=?", game, player, a.cfg.Event).Scan(&evtCount); err != nil {
			return nil, err
		}
		inWindow := nowUnix >= a.cfg.Starts && nowUnix < a.cfg.Ends
		progressList = append(progressList, map[string]any{
			"activity_id":     a.cfg.ID,
			"version":         a.cfg.Version,
			"kind":            a.cfg.Kind,
			"event_type":      a.cfg.Event,
			"bucket":          bucket,
			"active":          a.active,
			"in_time_window":  inWindow,
			"client_allowed":  a.cfg.ClientAllowed,
			"progress":        prog,
			"target":          a.cfg.Target,
			"matching_events": evtCount,
			"reward_item":     a.cfg.Item,
			"reward_amount":   a.cfg.Amount,
		})

		if !a.active {
			findings = append(findings, "activity_inactive:"+a.cfg.ID)
			causes = append(causes, fmt.Sprintf("活动 %s 当前处于停用状态（active=false）。", a.cfg.ID))
		} else if !inWindow {
			findings = append(findings, "activity_outside_window:"+a.cfg.ID)
			causes = append(causes, fmt.Sprintf("活动 %s 不在当前服务端 UTC 开放时间窗内。", a.cfg.ID))
		} else if !a.cfg.ClientAllowed {
			findings = append(findings, "client_events_blocked:"+a.cfg.ID)
			causes = append(causes, fmt.Sprintf("活动 %s 未允许客户端事件推进（client_allowed=false）。", a.cfg.ID))
		} else if prog < a.cfg.Target {
			findings = append(findings, "incomplete_progress:"+a.cfg.ID)
			if evtCount == 0 {
				causes = append(causes, fmt.Sprintf("活动 %s 尚未收到玩家上报 %s 事件（进度 %d/%d）。", a.cfg.ID, a.cfg.Event, prog, a.cfg.Target))
			} else {
				causes = append(causes, fmt.Sprintf("活动 %s 当前周期(%s)进度未达标（%d/%d），请确认是否存在重复 event_id 去重或事件时间跨天。", a.cfg.ID, bucket, prog, a.cfg.Target))
			}
		} else {
			key := fmt.Sprintf("activity:%s:%d:%s", a.cfg.ID, a.cfg.Version, bucket)
			var grantID string
			err := s.DB.QueryRowContext(ctx, "SELECT id FROM grants WHERE game_id=? AND player_id=? AND business_key=?", game, player, key).Scan(&grantID)
			if errors.Is(err, sql.ErrNoRows) {
				findings = append(findings, "grant_missing_after_completion:"+a.cfg.ID)
				causes = append(causes, fmt.Sprintf("活动 %s 进度已达标（%d/%d）但未查到发奖流水(%s)，可能需要核实后补发。", a.cfg.ID, prog, a.cfg.Target, key))
			} else if err != nil {
				return nil, err
			}
		}
	}

	mails, err := s.queryRowsGeneric(ctx,
		"SELECT m.id,m.title,m.claimed,COALESCE(st.deleted_at,0)>0 AS deleted,m.expires_at,g.business_key,g.item,g.amount FROM mail m JOIN grants g ON g.id=m.grant_id LEFT JOIN mail_states st ON st.mail_id=m.id WHERE m.game_id=? AND m.player_id=? ORDER BY g.created_at DESC,m.id LIMIT 20",
		game, player,
	)
	if err != nil {
		return nil, err
	}
	unclaimedActive, expiredMails, claimedMails := 0, 0, 0
	for _, m := range mails {
		claimed, _ := m["claimed"].(int64)
		if claimedBool, isBool := m["claimed"].(bool); isBool && claimedBool {
			claimed = 1
		}
		deleted, _ := m["deleted"].(int64)
		exp, _ := m["expires_at"].(int64)
		if claimed == 1 {
			claimedMails++
		} else if exp > 0 && exp <= nowUnix {
			expiredMails++
		} else if deleted == 0 {
			unclaimedActive++
		}
	}
	if unclaimedActive > 0 {
		findings = append(findings, "reward_mail_unclaimed")
		causes = append(causes, fmt.Sprintf("玩家邮箱中有 %d 封未过期且未领取的奖励邮件，道具需点击领取后才会进入背包库存。", unclaimedActive))
	}
	if expiredMails > 0 {
		findings = append(findings, "reward_mail_expired")
		causes = append(causes, fmt.Sprintf("玩家有 %d 封已过期的未领奖励邮件，无法再直接领取，可结合工单评估补发。", expiredMails))
	}
	if unclaimedActive == 0 && expiredMails == 0 && claimedMails > 0 {
		findings = append(findings, "reward_already_claimed")
		causes = append(causes, fmt.Sprintf("已有 %d 封奖励邮件被领取，请核对库存与账本流水（inventory_ledger）确认具体变动。", claimedMails))
	}

	inv, err := s.queryRowsGeneric(ctx, "SELECT item,amount FROM inventory WHERE game_id=? AND player_id=? ORDER BY item", game, player)
	if err != nil {
		return nil, err
	}
	ledger, err := s.queryRowsGeneric(ctx, "SELECT id,item,delta,balance,reason,business_key,created_at FROM inventory_ledger WHERE game_id=? AND player_id=? ORDER BY id DESC LIMIT 10", game, player)
	if err != nil {
		return nil, err
	}
	opsEvents, err := s.queryOpsEvents(ctx, game, player, "", "", "", 0, 10)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"game_id":           game,
		"player_id":         player,
		"profile_name":      profileName,
		"activities":        progressList,
		"mail_summary":      map[string]int{"total": len(mails), "unclaimed_active": unclaimedActive, "expired_unclaimed": expiredMails, "claimed": claimedMails},
		"mails":             mails,
		"inventory":         inv,
		"inventory_ledger":  ledger,
		"recent_ops_events": opsEvents,
		"findings":          findings,
		"likely_causes":     causes,
	}, nil
}

func (s *Service) mcpRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /gm/mcp", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		r = r.WithContext(ctx)

		host := clientIP(r, s.TrustedProxies)
		if !s.allowRequest(host, time.Now()) {
			if s.Telemetry != nil {
				s.Telemetry.RecordRateLimit()
				s.Telemetry.RecordRequest("POST /gm/mcp", r.Method, 429, time.Since(start))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
			return
		}

		gm, err := s.gmSession(r)
		if err != nil {
			status, code := 401, "gm_unauthorized"
			var p Problem
			if errors.As(err, &p) {
				status, code = p.Status, p.Code
			}
			if s.Telemetry != nil {
				s.Telemetry.RecordRequest("POST /gm/mcp", r.Method, status, time.Since(start))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": code})
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_mcp_request"})
			return
		}

		resp, hasResp := s.newMCPServer().HandleRequest(withMCPGM(r.Context(), gm), body)
		if s.Telemetry != nil {
			s.Telemetry.RecordRequest("POST /gm/mcp", r.Method, 200, time.Since(start))
		}
		if !hasResp {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	})
}
