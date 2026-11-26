# MiniVerse Studio Backend — 2027 社招旗舰升级总控方案（Upgrade Master Plan）

> 制定日期：2026-11-26  
> 适用对象：毕业三年传统 C++ 游戏服务端开发者转型 **Go 通用后端 / 现代游戏平台后端 / Go+AI 工程化中级工程师**  
> 基线仓库：`D:\MVP\miniverse-studio-backend`（`schema_version = 25` → 升级至 `26`）

---

## 一、 现状诊断：现有 MVP 与社招中级标准的 8 大核心缺口

`miniverse-studio-backend` 当前已具备非常扎实的底座（Go 1.27 模块化单体、MySQL 8.4 幂等迁移与锁、C++20/gRPC RuleCore、Godot 4 SDK、Vue 3 GM/玩家端、MCP Server 与 Ops Agent 原型、Prometheus/Grafana 观测栈）。但在社招二面深挖与高并发场景下，源码层面仍暴露出 **8 个具体工程破绽**：

| 编号 | 现状代码位置 | 存在的真实问题 | 对应的社招考察维度 |
| --- | --- | --- | --- |
| **G1** | [ADR-0001](adr/0001-modular-monolith.md) vs 转型需求 | `ADR-0001` 声明“不引入 Redis/Kafka”；直接加中间件会产生架构自相矛盾，且 `internal/service/` 单包堆了 30+ 文件、`Service` 结构体挂 25+ 字段与 6 把锁 | 架构演进取舍（Trade-offs）与模块化边界 |
| **G2** | [saves.go](../backend/internal/service/saves.go#L21-L39) | 每次云存档读写都 `gzip.NewWriter`/`NewReader`，`compress/flate` 单次初始化分配约 800KB 堆内存，高并发下 `alloc_space` 与 GC 压力剧增 | Rank 1：Go 逃逸分析、GC 与 `sync.Pool` |
| **G3** | [service.go](../backend/internal/service/service.go#L144-L146) | `gameCache` 满 1024 直接 `clear()` 引发全量缓存雪崩；缺少防击穿合并；多实例时 `InvalidateGameCache` 仅清单机内存 | Rank 1/3：手写 `O(1)` LRU、Singleflight 与缓存三灾 |
| **G4** | [database.go](../backend/internal/database/database.go#L12-L20) & [ops_observability.go](../backend/internal/service/ops_observability.go#L84-L89) | 连接池未设 `ConnMaxLifetime`/`ConnMaxIdleTime`；更危险的是触发 `429 rate_limited` 时会**同步执行 `INSERT INTO ops_events`**，CC 洪峰会反手打爆 MySQL 连接池 | Rank 2/5：连接池治理与高并发故障隔离 |
| **G5** | [boards.go](../backend/internal/service/boards.go#L166) & [operations.go](../backend/internal/service/operations.go#L50) | 排行榜 `nearby` 每次事务跑 `COUNT(*)`；单机 `limiter.go` 无法跨节点；群发奖励硬编码同步 100 人上限，缺少分布式锁与异步削峰 | Rank 2/6：Redis ZSET 同分排序、Lua 锁续期、Kafka + Outbox |
| **G6** | [replays.go](../backend/internal/service/replays.go#L127-L137) & [rulecore/main.cpp](../rulecore/main.cpp) | Go→C++ gRPC 仅有 2s 超时无三态熔断器（下游卡死拖垮 Goroutine）；缺少跨 Go/C++ 的 W3C `traceparent` 与鉴权 `pprof` | Rank 5/6：微服务熔断、超时预算与全链路观测 |
| **G7** | [agent.go](../backend/internal/opsagent/agent.go#L537), [mcp/server.go](../backend/internal/mcp/server.go), [planner.go](../backend/internal/service/planner.go), [companion.go](../backend/internal/service/companion.go) | 3 个 AI 模块各自为战（`planner` 硬编码 mock、`companion` 裸调 OpenAI）；`mcp/server.go` 未校验 `InputSchema`；`truncateRunes` 会把 JSON 截成非法半截；缺少 PII 出网脱敏、死循环熔断、Groundedness 事实核验与 SSE 流式 | Rank 4：四钩子安全 AI 网关、Workflow vs Agent 边界 |
| **G8** | [PERFORMANCE_BENCHMARK.md](verification/PERFORMANCE_BENCHMARK.md) & `tests/` | `load.mjs` 因受单 IP `600 req/min` 限制仅跑 550 请求（<0.2s），非稳态压测；缺少离线 Golden Set 消融评测与 K8s 清单 | Rank 4/5/6：可复现压测基准、Eval 消融与云原生 |

---

## 二、 五大升级支柱与代码映射

### 支柱 1：架构解耦与 Go 底层性能基建（Sprint 1）
1. **架构契约升级**：新增 [ADR-0003](adr/0003-hybrid-consistency-and-ai-gateway.md)，确立“核心资产强一致 + 二级缓存 + Outbox 最终一致”的分级策略。
2. **拆分高内聚基础包**：
   - `backend/internal/cache/`：手写泛型 **16 分片线程安全 `O(1)` LRU（双向链表 + 哈希表 + 随机抖动 TTL）** + **`Singleflight`**。
   - `backend/internal/resilience/`：实现 **Closed / Open / Half-Open 三态熔断器** 与 **两级限流器**。
   - `backend/internal/service/saves.go`：`sync.Pool` 复用 `*gzip.Writer`（`Reset`）与 `*bytes.Buffer`（容量 `> 64KB` 不回池防常驻内存膨胀）。
   - `backend/internal/database/database.go`：配置 `SetConnMaxLifetime(15m)` / `SetConnMaxIdleTime(5m)`，升级 `ExpectedSchemaVersion = 26`。
   - **跨语言 Trace 与 Pprof**：`httpserver/middleware.go` 支持 W3C `traceparent`；`replays.go` 挂载三态熔断器并透传 gRPC Metadata 至 `rulecore/main.cpp`；新增鉴权保护的 `/gm/system/pprof/*`。

### 支柱 2：Redis + Redpanda (Kafka) + Transactional Outbox 分布式闭环（Sprint 2）
1. **增量迁移 `026_distributed_and_aigateway.sql`**：严守不修改 `001`–`025` 校验和铁律，新增 `outbox_events`、`mq_consumed_events`、`batch_grant_jobs`、`ai_gateway_audits`。
2. **Redis 深度落点（`internal/redisx` & `internal/lock` & `boards.go` & `limiter.go`）**：
   - **排行榜 ZSET 字典序同分编码**：`score=0`，member 编码 `score` 方向 + `20位时间优先级` + `player_id`（Lua 原子更新），0 精度损失实现 `max`/`sum`/`min` 同分先达排序；并落实空值哨兵、`Singleflight` 与随机 TTL。
   - **Lua 分布式锁 + Watchdog**：保护后台“到期赛季自动结算调度器”，由 MySQL `FOR UPDATE` 与 `settled` 做最终幂等兜底，完美闭环主从异步失效追问。
   - **Pub/Sub 跨实例失效**：游戏上下架、活动发布停用、玩家封禁强踢通过 Redis Pub/Sub 秒级广播清空多节点 L1 缓存。
   - **两级限流**：本地快速过滤 + Redis Lua 原子计数（Redis 宕机优雅降级单机）。
3. **异步削峰与 Outbox（`internal/mq` & `ops_observability.go` & `operations.go`）**：
   - **`ops_events` 异步微批通道**：彻底解决 429 洪峰同步写 DB 拖垮连接池的致命隐患（队列容量 2048，突发 429 采样，满队丢弃计数 `miniverse_ops_events_dropped_total`）。
   - **Outbox Relay + Redpanda/Kafka + DLQ**：支持 `reward_batch` 万人异步分批发奖审批单（突破 100 人限制），消费者通过 `mq_consumed_events` 实现精确幂等，超重试次数进 DLQ Topic。

### 支柱 3：统一四钩子 AI 安全网关与三端收口（Sprint 3）
抽离 `backend/internal/aigateway/` 统一纳管 `opsagent`、`planner`、`companion`：
1. **Hook 1 — `LLMInputHook`**：双向 PII 脱敏保险箱（手机号/邮箱/IPv4/Token 替换为 `<PII_*>`）+ 高危 Prompt Injection 拦截。
2. **Hook 2 — `ToolPreInvokeHook`**：复用 `jsonschema/v5` 补齐 MCP 入参强校验 + 声明式策略（如单笔补发提案额度上限）+ `(tool_name, canonical_args_hash)` 连续重复调用死循环熔断。
3. **Hook 3 — `ToolPostInvokeHook`**：修复 `truncateRunes` 破坏 JSON 结构的 Bug，改为合法 JSON 字段级结构化裁剪 + 工具返回 PII 脱敏。
4. **Hook 4 — `LLMOutputHook`**：JSON Schema 强校验 + **Groundedness 事实引用交叉校验**（校验 `evidence_ids` 是否真实出自本轮工具返回，防幻觉与越权承诺，违者自动降级至 `deterministic_playbook`）+ 审计落库 `ai_gateway_audits`。
5. **渐进式检索与 SSE**：新增 `search_runbook`（分块 BM25 检索）与 `inspect_player_summary`；新增 `POST /gm/games/{game}/ops-agent/diagnose/stream` SSE 流式诊断；打通 `planner.go` 真模型“生成 -> 校验 -> 仿真 -> 自修复”确定性工作流。

### 支柱 4：稳态压测、Golden Set 消融评测与云原生（Sprint 4）
- `backend/cmd/bench`：60s 级稳态多 IP 混合压测与故障隔离对比工具；
- `backend/cmd/eval`：30 用例 Golden Set 离线消融评测套件（Baseline vs +渐进检索 vs +完整四钩子网关）；
- `deploy/compose.yaml`（加入 `redis` 与 `redpanda`）与 `deploy/k8s/`（Deployment/Probes/HPA）；
- `docs/verification/TROUBLESHOOTING_AND_BENCHMARK_2027.md`（真实实测战报）与 `docs/PROJECT_TEACHING_GUIDE.md`（系统教学手册）。
