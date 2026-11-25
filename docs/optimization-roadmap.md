# MiniVerse Studio Backend 分阶段优化路线图（P0 → P4）

制定日期：2026-11-25  
定位：面向高工程质感与架构深度的全面打磨（延续 [ADR-0001](adr/0001-modular-monolith.md) 模块化单体与同步事务原则）。

---

## 一、 基线发现与优化目标

| 维度 | 优化前基线 | 目标状态 |
| --- | --- | --- |
| **RuleCore 镜像体积** | `miniverse-rulecore:local` 约 **1.71 GB**（携带 Go + GCC/CMake 编译链） | 多阶段剥离编译工具链，降至 **< 150 MB** |
| **游戏静态资源** | `witch-cauldron/assets` 约 **32 MB**（未压缩原图 PNG），`nginx.conf` 对 `/games/` 设为 `no-store` | WebP 管线压缩至 **< 4 MB**，HTML `no-cache` 与静态媒体缓存分离并开启 `gzip` |
| **Telemetry 统计精度** | 512 样本环同时负责 5m 请求统计，QPS > 1.7 即发生截断 | 300 秒固定时间桶精确计数 + P50/P95/P99 分位 + 独立 `METRICS_BEARER_TOKEN` |
| **AI Ops 安全边界** | `api_key` 明文存库、配置变更未审计、缺少 `DialContext` IP 层防 SSRF | `AES-256-GCM` 静态加密 + 防 DNS Rebinding 拨号器 + 配置变更写入 `audits` |
| **会话与鉴权体验** | GM 缺少服务端 `/gm/logout` 吊销；网页 `F5` 刷新即掉线 | Web `HttpOnly + SameSite=Strict Cookie`（配 `X-MiniVerse-CSRF`）与 Native/SDK `Bearer` 双模并存 |
| **热点 SQL 与清理** | 每次玩家请求查 `games` 表；`trace_request` 走 `JSON_EXTRACT` 扫描；`ops_agent_runs`/`save_history` 无限增长 | `games` 5s TTL 内存缓存、`audits` 虚拟列索引、诊断有界并发、全表分批清理 + S13 前后压测对比 |
| **游戏反作弊与 GM 闭环** | 客户端自选 `seed`；建榜/结算仅限 `sandbox_only`；缺少封禁强踢 | `match_tickets` 服务端签发种子 + GM 榜单/结算双人审批 + 封禁原子强踢与 MCP 提案 |

---

## 二、 分阶段实施计划

### Phase 0：基线收敛、AI Ops 安全三件套与 CI 闭环
- 增量迁移 `023_ops_agent_security.sql`（绝不修改已锁定校验和的 `001`–`022`）。
- `OPS_AGENT_SECRET_KEY` 驱动的 `AES-256-GCM` 密文持久化（未配置主密钥时仅驻留内存，清除历史明文）。
- `NewSafeHTTPClient` 自定义 `DialContext` 在解析 IP 后拦截 RFC1918 / 环回 / `169.254.0.0/16` 元数据地址，并复用到 `companion` 模型调用。
- 配置修改事务写入 `audits`；补齐 CI 打包 `cmd/mcp`、`admin/web` 单测与 `ops-agent-mcp` 集成回归。

### Phase 1：双模会话鉴权、WebP 资源管线、Nginx 加固与前端模块化
- 服务端 `POST /gm/logout`、`GET /gm/me`；Web 端 `HttpOnly + SameSite=Strict` Cookie + `X-MiniVerse-CSRF` 与 Tauri/Godot/MCP `Bearer` 双模兼容。
- `scripts/optimize-cauldron-assets.py` 将 `witch-cauldron` 美术转换为适配分辨率的 `.webp` 并更新 `manifest.{json,js}` 与 `provenance.json`。
- 双端 `nginx.conf` 开启 `gzip` 与安全头；拆分 `player/web/src/main.js` 视图并格式化测试脚本。

### Phase 2：Telemetry 精确时间桶、热点 SQL 优化与 S13 压测对比
- 300 秒时间桶 + 2048 样本 P99；迁移 `024_indexes_and_retention.sql` 为 `audits.request_id` 与 `ops_agent_runs` 建索引。
- `games.active` 5s TTL 缓存、`grants` 查询加 `LIMIT`、`playerDiagnostics` 有界并发；`cleanup.go` 增加 `ops_agent_runs`/`ops_agent_steps`（30 天）与 `save_history`（每槽位保留最新 50 版）分批清理。
- 多场景并发压测与优化前后对比报告（`docs/verification/PERFORMANCE_BENCHMARK.md`）。

### Phase 3：核心游戏业务与反作弊三大闭环
- 迁移 `025_domain_loops.sql`：`match_tickets`、`rule_boards.require_ticket`、`player_bans`。
- `POST /v1/games/{game}/matches/start` 签发服务端随机种子票据，联动 `RuleCore` 闭环防离线刷种子。
- `GET /gm/games/{game}/boards` 与 `gm_requests`（`board_upsert` / `season_settle` / `player_ban`）双人审批流。
- 玩家封禁/解封秒级强踢全会话及 MCP `propose_player_freeze` 工具。

### Phase 4：RuleCore 极简镜像、可观测性栈与容灾演练
- `rulecore/Dockerfile` 剥离 Go 与编译工具链的多阶段构建。
- `METRICS_BEARER_TOKEN` 鉴权与 `--profile observability`（Prometheus + Grafana 预置看板）。
- `scripts/backup-verify.mjs` 自动化备份、隔离恢复与数据一致性校验脚本。
