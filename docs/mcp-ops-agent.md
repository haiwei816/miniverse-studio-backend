# MCP 服务与 AI Ops Agent 智能运维指南

MiniVerse Studio Backend 内置符合 **MCP JSON-RPC 2.0**（`protocolVersion: "2025-03-26"`）的服务端运维协议层、运行期可观测性采集器与多步 **AI Ops Agent** 排障引擎，将玩家业务数据（档案、云存档、活动进度、发奖流水、邮箱、库存账本、排行榜、回放）、业务异常日志（`ops_events`）、审计工单（`audits` / `gm_requests`）以及 Go / MySQL / C++ RuleCore 服务健康度关联起来，实现 AI 辅助排障与受控运维提案。

---

## 1. 整体架构与数据关联链路

```text
  HTTP 请求 / RuleCore RPC / MySQL 事务
      │
      ├─► X-Request-ID 中间件 + 进程内 Telemetry（P50/P95/5m错误率/DB连接池/死锁/限流/RuleCore探活）
      │        ├─ GET /gm/system/telemetry  (JSON 实时快照 + RuleCore TCP 探测)
      │        └─ GET /gm/system/metrics    (Prometheus 0.0.4 文本指标)
      │
      └─► 异常采集器（409/422/429/5xx）─► 内存环形缓冲(200) + MySQL ops_events (迁移 020)
               └─ GET /gm/games/{game}/ops-events & /gm/games/{game}/players/{player}/diagnostics

  AI Ops Agent (POST /gm/games/{game}/ops-agent/diagnose) / 外部 MCP 客户端
      │
      ├──► MCP Server (POST /gm/mcp 或 stdio cmd/mcp 桥接)
      │       ├─ 只读诊断工具 x6（inspect_service_health, diagnose_player, query_ops_logs,
      │       │                 trace_request, explain_reward_issue, query_audit_trail）
      │       ├─ 受控提案工具 x2（propose_reward_compensation, propose_activity_stop）
      │       └─ 只读资源 x3（miniverse://health/snapshot, miniverse://ops/runbook, miniverse://ops/games）
      │
      └──► 诊断轨迹持久化 (ops_agent_runs + ops_agent_steps, 迁移 021) + audits (ops_agent_diagnose)
               │
               ▼ (若生成补偿或停用提案)
          gm_requests (state = 'pending') ─► 另一位人类 reviewer 审核批准 ─► 事务执行（发奖/停用）
```

---

## 2. 受控运维安全护栏（Human-in-the-Loop）

1. **AI 永不直接修改生产玩家资产或配置**：Agent 与 MCP 写工具（`propose_reward_compensation`、`propose_activity_stop`）**仅能创建 `state = 'pending'` 的审批草案**，理由会自动加上 `[AI-MCP]` 前缀并受长度截断保护。
2. **职责分离与不可自审**：`gm_requests` 严格要求另一位具有 `approve` 权限的管理员（如 `reviewer`）批准执行；提案创建人审批自身工单会触发 `403 self_approval_forbidden`。
3. **RBAC 与多游戏租户隔离**：
   - `support` / `reviewer` 调用 `tools/list` 时会被协议层 `toolFilter` 自动隐藏所有非只读工具（`propose_*`），直接调用 `tools/call` 会返回 JSON-RPC `-32602` 错误。
   - 所有工具均复用 `authorizeGMGame` 检查 GM 对目标 `game_id` 的读/写授权（例如仅拥有 `sky-island` 权限的 `support` 诊断 `card-game` 玩家会返回 `game_forbidden`）。
4. **默认只读与并发熔断**：`POST /gm/games/{game}/ops-agent/diagnose` 必须显式传入 `allow_propose: true` 才会放开 `propose_*` 工具；接口受单次 45s 超时及进程级最大 8 并发信号量（超限返回 `503 ops_agent_busy`）保护。

---

## 3. MCP 工具与资源清单

### 3.1 工具列表（8 个）

| 工具名 | 权限类别 | 说明与主要关联表 |
| --- | --- | --- |
| `inspect_service_health` | ReadOnly | 返回 `db_ready`、连接池 `db_pool`、Schema 版本、5 分钟 QPS/错误率、P50/P95、死锁重试、限流及 RuleCore 实时探活状态 |
| `diagnose_player` | ReadOnly | 聚合 360° 玩家视图：档案、云存档、近期存档历史、活动进度、发奖流水、库存、流水账本、邮件、事件、榜分、回放与近期 `ops_events` |
| `query_ops_logs` | ReadOnly | 按 `game_id`、`player_id`、`request_id`、`error_code`、`category`、`since` 检索 `ops_events`（限制最多 100 条） |
| `trace_request` | ReadOnly | 根据 `X-Request-ID` 横向关联同游戏下的 `ops_events` 与 `audits` 操作记录 |
| `explain_reward_issue` | ReadOnly | 专项诊断“完成活动未收到道具”：串查活动开放窗、`client_allowed`、事件上报、`activity_progress`、`grants`、`mail`（未领/过期/已删/已领）与 `inventory_ledger` |
| `query_audit_trail` | ReadOnly | 查询指定游戏最近的 `audits` 审计记录与 `gm_requests` 审批工单 |
| `propose_reward_compensation` | 受控写（`admin`/`operator`） | 生成单玩家 `kind='reward'` 补偿审批单（`state='pending'`），需人工复核批准 |
| `propose_activity_stop` | 受控写（`admin`/`operator`） | 生成 `kind='activity_stop'` 停用异常活动审批单（`state='pending'`），人工批准后将 `activities.active` 设为 `FALSE` |

> 为防止大模型上下文爆仓，MCP 返回文本超过 **16 KB** 时自动截断，并标注截断提示；内部 Agent 每步结果摘要上限为 **6000 字符**，单次运行最多 **6 步**工具调用。

### 3.2 只读资源（3 个）

- `miniverse://health/snapshot`：当前服务健康度与指标 JSON 快照。
- `miniverse://ops/runbook`：常见错误码（`rulecore_unavailable`、`revision_conflict`、`mail_expired`、`rate_limited`、`transaction_busy`）与审批护栏指南。
- `miniverse://ops/games`：当前 GM 账号拥有权限的活跃游戏清单。

---

## 4. 两种诊断模式：`llm` 与 `deterministic_playbook`

| 配置状态 | `APP_ENV=sandbox` | `APP_ENV=production` |
| --- | --- | --- |
| 配置了 `OPS_AGENT_URL` + `OPS_AGENT_MODEL` | **`llm` 模式**：标准 OpenAI Chat Completions `tool_calls` 多轮 ReAct 循环（最多 6 步），允许 `https://` 或本地演练 `http://` | **`llm` 模式**：仅允许 `https://`（且禁止 URL 内嵌凭证） |
| 未配置 `OPS_AGENT_URL` | **`deterministic_playbook` 模式**：**非模拟数据**——按排障规则真实调用进程内 MCP 工具检索 MySQL 与实时遥测，并明确返回 `mode="deterministic_playbook"`、`real_ai=false` | 拒绝运行，返回 `503 ops_agent_not_configured` |

环境变量说明（见 `.env.example` 与 `deploy/compose.yaml`）：
- `OPS_AGENT_URL`：兼容 OpenAI `/v1/chat/completions` 的完整或基础地址（例如 `https://api.example.com/v1/chat/completions` 或 `https://api.example.com/v1`）。
- `OPS_AGENT_API_KEY`：模型 API Key（仅服务端持有，不回传前端）。
- `OPS_AGENT_MODEL`：模型名称（例如 `gpt-4o-mini`、`qwen-plus` 等）。

---

## 5. 接入外部 AI 客户端 / IDE（`mcp.json` 示例）

MiniVerse 同时支持 **Streamable/HTTP POST** 与 **标准 MCP Stdio 二进制桥接（`cmd/mcp`）**。

### 方式 A：HTTP 直接接入 `/gm/mcp`（需要携带 GM Bearer Token）

先通过 `POST /gm/login` 获取短期 GM `access_token`，或使用支持请求头的 MCP 客户端：

```json
{
  "mcpServers": {
    "miniverse-ops-http": {
      "url": "http://127.0.0.1:18080/gm/mcp",
      "headers": {
        "Authorization": "Bearer <YOUR_GM_ACCESS_TOKEN>"
      }
    }
  }
}
```

### 方式 B：Stdio 桥接二进制 `cmd/mcp`（推荐本地 IDE / Agent CLI）

容器镜像中已编译 `/app/mcp`，本地也可以在 `backend/` 目录运行 `go run ./cmd/mcp`。`cmd/mcp` 支持 `MINIVERSE_GM_TOKEN` 或 `MINIVERSE_GM_USER` + `MINIVERSE_GM_PASSWORD`（启动时自动登录换取 Token）：

```json
{
  "mcpServers": {
    "miniverse-ops-stdio": {
      "command": "docker",
      "args": [
        "compose",
        "-f",
        "d:/MVP/miniverse-studio-backend/deploy/compose.yaml",
        "exec",
        "-T",
        "-e",
        "MINIVERSE_API_URL=http://127.0.0.1:8080",
        "-e",
        "MINIVERSE_GM_USER=admin",
        "-e",
        "MINIVERSE_GM_PASSWORD=sandbox-admin",
        "api",
        "/app/mcp"
      ]
    }
  }
}
```

> 建议：若希望外部 Agent 仅具备只读排障能力、完全无法创建审批单，请将认证账号切换为 `support`（`sandbox-support`）或 `reviewer`（`sandbox-reviewer`）。

---

## 6. GM 管理端界面与 Prometheus 观测

1. **GM 控制台 — `🤖 AI 运维助手` 视图**（`admin/web/src/views/ops_agent.js`）：
   - 预置场景快捷问题（奖励未到账排查、503 RuleCore 故障排查、`X-Request-ID` 冲突链路追踪）。
   - 控制 `allow_propose` 开关（对 `support`/`reviewer` 自动禁用）及可选补偿物品/数量或目标 `activity_id`。
   - 清晰展示运行模式徽标（`deterministic_playbook` 明确标注“未接入外部大模型 API”）、根因结论、处置建议、可跳转审批单卡片，以及完整折叠式 MCP 工具调用时间线与历史记录回放。
2. **GM 控制台 — `系统状态与日志` 视图**（`admin/web/src/views/system.js`）：
   - 实时展示 API P50/P95、近 5 分钟 QPS 与错误率、DB 连接池活跃/等待、死锁重试、限流、RuleCore TCP 探活与异常次数。
   - 支持按 `player_id` / `request_id` / `error_code` / `category` 过滤 `ops_events`，并可点击每条异常日志上的 **“AI 排障”** 直接携带上下文跳转到 AI 运维助手。
3. **Prometheus 指标与监控栈**：`GET /gm/system/metrics` 支持独立采集凭据 `METRICS_BEARER_TOKEN` 或 GM 会话鉴权，输出标准 `text/plain; version=0.0.4` 指标，包含 `miniverse_http_requests_total`、`miniverse_http_errors_total`、`miniverse_http_5m_error_rate`、`miniverse_http_latency_p50_ms`、`miniverse_http_latency_p95_ms`、`miniverse_http_latency_p99_ms`、`miniverse_db_connections_open`、`miniverse_db_connections_in_use`、`miniverse_deadlock_retries_total`、`miniverse_rate_limited_total`、`miniverse_rulecore_calls_total`、`miniverse_rulecore_errors_total`、`miniverse_schema_version`。可通过 `--profile observability` 启动 Prometheus 与 Grafana 开箱即用看板。
4. **Ops Agent 安全与受控提案**：通过 `OPS_AGENT_SECRET_KEY` 使用 AES-256-GCM 加密落盘外部模型 API Key（未配置密钥时仅驻留内存），配合 DNS 解析后 IP 校验（`ErrSSRFBlocked`）拦截内网/元数据 SSRF；MCP 写工具（`propose_reward_grant`、`propose_activity_stop`、`propose_player_freeze`）均强制创建 `pending` 审批单等待人类 `reviewer` 审核生效。
