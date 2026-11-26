# MiniVerse Studio Backend — 项目系统教学与面试实战指南

> 本文档专为“3年传统 C++ 游戏服务端 → 2027 上海 Go 通用后端 / 现代游戏平台 / Go+AI 工程化中级工程师”打造，结合 `miniverse-studio-backend` **真实源码与实测数据**，带你吃透整个项目的每一层设计与社招面试标准答法。

---

## 第一章：全局架构与源码地图（先看森林，再看树木）

### 1.1 一句话定性本项目
MiniVerse 是一个**“以 Go 模块化单体 + MySQL 为权威账本，按需叠加 L1/L2 缓存、Outbox+Redpanda 异步消息、C++20/gRPC 确定性规则引擎、以及四钩子 AI 安全网关”**的多游戏运营与智能排障平台。

> **架构设计铁律**（详见 [ADR-0001](adr/0001-modular-monolith.md) 与 [ADR-0003](adr/0003-hybrid-consistency-and-ai-gateway.md)）：  
> **能用单机本地事务解决的资产一致性绝不搞分布式事务；只有读放大热点（榜单/封禁/配置）才下沉 Redis；只有大批量耗时任务与异常日志才走 Outbox/异步削峰；AI 一律视为概率性不可信下游，必须被确定性四钩子网关和双人审批流包住。**

### 1.2 全景架构图

```mermaid
flowchart TB
    Clients["Godot 4 SDK / Vue3 玩家端 / Vue3 GM 控制台"] -->|"HTTP + X-Request-ID + W3C traceparent"| MW["httpserver.Logging 中间件"]
    MW --> Limiter["两级限流 (本地Map + Redis Lua)"]
    Limiter --> Service["internal/service (业务编排层)"]

    Service -->|"L1: 16分片 O(1) LRU + Singleflight"| Cache["internal/cache"]
    Service -->|"L2: ZSET 字典序同分榜 / Ban缓存 / PubSub失效 / Lua锁"| Redis[("Redis 7.4")]
    Service -->|"核心强一致事务: 存档/发奖/邮件/库存/审批/Outbox"| MySQL[("MySQL 8.4")]
    Service -->|"三态熔断器 + gRPC Metadata Trace"| RuleCore["C++20 RuleCore (:50051)"]

    MySQL -->|"OutboxRelay 轮询 + 指数退避 + DLQ"| Redpanda[("Redpanda / Kafka")]
    Redpanda -->|"幂等消费 (mq_consumed_events)"| BatchWorker["万人异步发奖 Worker"]

    Service -->|"异常采样 + 有界队列 + 微批写库"| OpsObs["ops_events + Prometheus + pprof"]

    Service -->|"opsagent / planner / companion"| AIGW["internal/aigateway (四钩子安全网关)"]
    AIGW -->|"Hook2/3 校验与裁剪"| MCP["internal/mcp (12个Tools + 3个Resources)"]
    AIGW -->|"Hook1 脱敏 / Hook4 幻觉降级"| LLM["外部 OpenAI / Anthropic 或 Playbook兜底"]
```

### 1.3 代码目录导读（从下往上读最省力）

| 包路径 | 核心文件 | 你要掌握的核心代码机制 |
| --- | --- | --- |
| `backend/internal/cache/` | [lru.go](../backend/internal/cache/lru.go), [singleflight.go](../backend/internal/cache/singleflight.go) | 16 分片泛型 `O(1)` 双向链表+Map LRU、负缓存哨兵、10% TTL 随机抖动、`Singleflight` 防击穿 |
| `backend/internal/resilience/` | [circuitbreaker.go](../backend/internal/resilience/circuitbreaker.go), [ratelimiter.go](../backend/internal/resilience/ratelimiter.go) | Closed/Open/Half-Open 三态熔断器、两级限流器 |
| `backend/internal/redisx/` & `lock/` | [redisx.go](../backend/internal/redisx/redisx.go), [lock.go](../backend/internal/lock/lock.go) | nil 安全降级封装、Pub/Sub 跨实例缓存失效、Lua `SET NX PX` + Watchdog 自动续期 + Lua 校验 owner 防误删 |
| `backend/internal/mq/` | [outbox.go](../backend/internal/mq/outbox.go), [consumer.go](../backend/internal/mq/consumer.go) | 本地事务 `EnqueueTx`、Relay 轮询与退避、`mq_consumed_events` 幂等消费、DLQ 死信路由 |
| `backend/internal/aigateway/` | [pii.go](../backend/internal/aigateway/pii.go), [policy.go](../backend/internal/aigateway/policy.go), [compactor.go](../backend/internal/aigateway/compactor.go), [groundedness.go](../backend/internal/aigateway/groundedness.go), [gateway.go](../backend/internal/aigateway/gateway.go) | 4 钩子拦截流水线：PII 脱敏、Prompt 注入检测、Schema+OPA 策略+死循环熔断、JSON 结构化裁剪、Groundedness 事实引用校验 |
| `backend/internal/opsagent/` | [agent.go](../backend/internal/opsagent/agent.go), [ssrf.go](../backend/internal/opsagent/ssrf.go) | 多轮 ReAct、SSE 事件流、`deterministic_playbook` 降级、DialContext 防 DNS Rebinding SSRF |
| `backend/internal/service/` | [saves.go](../backend/internal/service/saves.go), [boards.go](../backend/internal/service/boards.go), [distributed_jobs.go](../backend/internal/service/distributed_jobs.go), [ops_observability.go](../backend/internal/service/ops_observability.go), [replays.go](../backend/internal/service/replays.go), [planner.go](../backend/internal/service/planner.go), [mcp_server.go](../backend/internal/service/mcp_server.go) | 业务核心：`sync.Pool` 压缩池、Redis ZSET 字典序同分榜、异步微批 `ops_events`、RuleCore 熔断与 `traceparent` 透传、Planner 自修复 Workflow |
| `backend/cmd/bench/` & `eval/` | [bench/main.go](../backend/cmd/bench/main.go), [eval/main.go](../backend/cmd/eval/main.go) | 多 IP 稳态压测器、30 用例 Golden Set 离线消融评测器 |

---

## 第二章：四条核心链路逐行拆解（照着源码学）

### 链路 1：高并发读与缓存三灾治理（[boards.go](../backend/internal/service/boards.go) + [distributed_jobs.go](../backend/internal/service/distributed_jobs.go)）
**痛点**：原排行榜 `nearby` 每次请求在事务里执行 `SELECT COUNT(*) FROM scores WHERE ...`，且同分要求“先达到者排在前面”。  
**怎么解**：
1. **ZSET 零精度损失同分编码**（见 `encodeBoardMember`）：Redis 普通 `float64` score 只有 53 位有效精度，无法同时塞下“分数 + 19 位纳秒时间戳 + 玩家 ID”。因此我们令 ZSET 所有成员的 `score = 0`，Redis 自动转为**纯字典序排序**：
   - `max/sum` 模式：`member = "%016d:%020d:%s"`，高位存 `9999999999999999 - score`（分越高越靠前），中位存 `reached_at` 纳秒时间戳（越早越小越靠前），低位存 `player_id`。
   - 配套 Hash `mv:board:h:{game}:{board}:{season}` 存 `player_id -> 当前member`，更新分数时通过 **Lua 脚本 `boardZSetUpsertScript`** 原子删旧增新。
2. **三灾防御**：
   - **穿透**：空榜写入 15s 负缓存哨兵 `mv:board:empty:...`；
   - **击穿**：缓存未命中时通过 `s.boardSF.Do(...)`（`Singleflight`）保证 1000 个并发只打 1 次 MySQL 回源；
   - **雪崩**：过期时间 `5m + rand(0..60s)` 随机打散。

### 链路 2：核心资产一致性 vs 异步削峰（[operations.go](../backend/internal/service/operations.go) + [mq/outbox.go](../backend/internal/mq/outbox.go)）
**为什么单玩家领取用同步事务，万人补发用 Outbox？**
- 玩家领邮件（[mail.go](../backend/internal/service/mail.go) `claimMail` + [inventory.go](../backend/internal/service/inventory.go) `creditInventory`）：单行 `FOR UPDATE` 锁邮件 + 写账本，耗时仅毫秒级，必须**同步强一致**，绝不引入跨系统中间态。
- 运营发全服补偿（`kind = "reward_batch"`）：涉及成千上万玩家，如果堵在审批 HTTP 请求的单个事务里必超时且锁死全表。
  1. Reviewer 点击审批通过时，MySQL **同一事务内**更新 `gm_requests.state='executed'`、创建 `batch_grant_jobs`、并调用 `mq.EnqueueTx` 插入 `outbox_events`；
  2. 后台 `OutboxRelay` 投递到 Redpanda Topic `miniverse.reward.batch`（`RequiredAcks: RequireAll`），失败按 $2^n$ 秒指数退避，超过 5 次进 `.dlq` 死信队列；
  3. 消费者通过 `mq.ProcessIdempotent` 查写 `mq_consumed_events` 表防重，按每批 200 人调用幂等 `grant()` 入库（依靠 `grants` 表唯一业务键 `batch:{request_id}` 实现“即使发生极端重复消费也零重复发奖”）。

### 链路 3：四钩子 AI 安全网关与 Workflow/Agent 边界（[aigateway/](../backend/internal/aigateway/gateway.go)）
本项目故意在一个仓库内展示了 **3 种不同的 AI 工程架构范式**，面试可直接对比：
1. **固定流程用 Deterministic Workflow（[planner.go](../backend/internal/service/planner.go)）**：生成活动配置步骤确定（生成 JSON -> Schema 校验 -> 业务校验 -> 数值仿真 `simulate()` -> 报错回喂自修复），**不用 ReAct Agent**，可控且便宜。
2. **动态排障用 Single Agent + MCP（[opsagent/agent.go](../backend/internal/opsagent/agent.go)）**：因为故障原因不确定（可能是 RuleCore 503、也可能是邮件过期），才用单 Agent 配合 `search_runbook` / `inspect_player_summary` 渐进式工具按需下钻。
3. **玩家闲聊用 Zero-Tool Sandboxed Context（[companion.go](../backend/internal/service/companion.go)）**：物理不挂载任何写工具，用 `epoch` 严格隔离跨游戏记忆。

所有出网 AI 调用统一受 **4 个确定性钩子**约束：
- `LLMInputHook`：正则脱敏手机号/邮箱/IP/API Key，拦截 Prompt 注入；
- `ToolPreInvokeHook`：补齐 MCP 入参 JSON Schema 强校验 + OPA 单笔补发上限（`<= 10000`）+ 同参数重复调用 2 次即触发 `loop_circuit_broken`；
- `ToolPostInvokeHook`：结构化裁剪大 JSON（解决直接截断字符串破坏 JSON 语法的隐蔽 Bug）；
- `LLMOutputHook`：核查结论中的 24 位 `request_id` 与 `evidence_ids` 是否真实存在于工具返回语料中，一旦发现捏造或“已直接发奖”幻觉，立即降级至 `deterministic_playbook`。

### 链路 4：从“限流反打爆 DB”到 `sync.Pool` 优化的排障闭环（[ops_observability.go](../backend/internal/service/ops_observability.go) + [saves.go](../backend/internal/service/saves.go)）
- **故障修复 1（`429` 洪峰打爆 MySQL）**：在 `service.go` 触发 `429 rate_limited` 时，原代码会同步 `INSERT INTO ops_events`。在 `cmd/bench` 3 秒 27,970 次 `429` 洪峰下，同步写库会立刻打光数据库连接池。改造为**同类错误按秒采样 + 容量 2048 有界 Channel + 后台 64 条微批 INSERT + 满队列非阻塞丢弃计数 `miniverse_ops_events_dropped_total`**，实测 `5xx = 0`、P99 = `8.15 ms`。
- **故障修复 2（`gzip` 临时堆分配优化）**：[saves.go](../backend/internal/service/saves.go) 用 `sync.Pool` 复用 `*gzip.Writer` 与 `<=64KB` Buffer，实测耗时从 `254,478 ns/op` 降至 `18,328 ns/op`（**13.9×**），每请求堆分配从 `1.07 MiB` 降至 `53.3 KiB`（**20.6×**）。

---

## 第三章：上海社招“三层连环追问”标准答法（直接对标报告 Rank 1–Rank 6）

### Q1（Rank 1）：为什么你们项目里 `saves.go` 用 `sync.Pool` 池化 `gzip.Writer`，而 `gameCache` 却要手写分片 LRU？两者不能互换吗？
- **第一层**：`sync.Pool` 每次 GC 来临（`STW` 前后 `poolCleanup`）可能随时被清空，且没有容量和过期语义，**绝不能当作业务缓存**；而 `compress/flate` 内部每次初始化约 800KB 字典内存，正好用 `sync.Pool` 做无状态临时对象复用。
- **第二层**：`sync.Pool` 存 `*bytes.Buffer` 时必须做容量上限守卫（我们设了 `maxPooledBufferCap = 64KB`，见 [saves.go:13](../backend/internal/service/saves.go#L13)），防止一次 1MB 的大存档把大 Buffer 永远钉死在 Pool 里造成内存驻留膨胀。
- **第三层**：早期 `gameCache` 是单把 `RWMutex` + 满 1024 直接 `clear()`，高并发下会引起锁争抢和缓存瞬时全清空抖动。改造成 `cache.ShardedLRU`（16 分片 FNV-1a 哈希 + `container/list` 双向链表 + Map）后，锁粒度缩小 16 倍且支持 `O(1)` 淘汰与 10% TTL 随机抖动。

### Q2（Rank 2）：你们的分布式锁如果遇到 Redis 主从异步切换（锁丢失）怎么办？
- **第一层**：加锁用 `SET key owner NX PX ttl` 保证原子性，value 放随机 `owner` token；解锁用 Lua 脚本先 `GET` 比对 `owner` 再 `DEL`，防止业务超时后误删别人的锁。
- **第二层**：引入 Watchdog 协程每 `ttl/3` 自动执行 Lua `PEXPIRE` 续期（见 [lock.go](../backend/internal/lock/lock.go)），并通过 `Lost()` channel 通知业务侧续期失败。
- **第三层（关键加分项）**：主从异步复制下 Redis 单实例锁本质仍非强一致（AP 倾向）。所以在我们的**到期赛季自动结算调度器**（[distributed_jobs.go](../backend/internal/service/distributed_jobs.go)）中，**Redis 锁只承担“多实例互斥减少无效数据库争抢”的优化角色**；真正的资金/发奖正确性由 MySQL `SELECT ... FOR UPDATE` 检查 `settled=FALSE` 以及 `grants` 表唯一业务键 `season:{board}:{season}` 兜底——即使两个实例同时拿到锁，也只会发放一笔奖励。

### Q3（Rank 2）：Outbox + Kafka 消费时如果程序重启、消息重复投递了怎么办？如果某条消息一直失败怎么处理？
- **第一层**：业务状态（如审批单通过、创建 `batch_grant_jobs`）与写 `outbox_events` 在**同一个 MySQL 事务提交**，消除“库写了但 MQ 没发出去”的双写不一致窗口。
- **第二层**：消费端采用 `mq.ProcessIdempotent` 查写 `mq_consumed_events` 去重表，同时底层 `grant()` 函数本身携带 `business_key = "batch:"+request_id` 唯一键，实现业务层兜底幂等。
- **第三层**：Outbox Relay 与 Consumer 均支持指数退避重试，超过最大重试次数后自动更新状态并投递至 `<topic>.dlq` 死信队列，同时递增 `miniverse_outbox_dlq_total` 告警指标，运营可通过 MCP 工具 `inspect_mq_and_jobs` 排查。

### Q4（Rank 4）：为什么你们做了“四钩子安全网关”和 Golden Set 消融评测，而不是直接用 LangChain 或堆 Multi-Agent？
- **第一层**：运维排障场景工具集合清晰（12 个 MCP 工具），单 Agent + 渐进式检索比多 Agent 互相传话延迟更低、错误率更可控；活动生成步骤固定，直接用 `planner.go` 确定性 Workflow + 自修复循环即可。
- **第二层**：我们把大模型视为“概率性不可信下游”，在 `LLMInput`、`ToolPreInvoke`、`ToolPostInvoke`、`LLMOutput` 四处挂确定性护栏（PII 脱敏、Schema 校验、OPA 额度拦截、死循环熔断、Groundedness 幻觉降级）。
- **第三层**：用实测消融数据说话（[golden-set-eval-2027.json](verification/golden-set-eval-2027.json)）：30 条评测集中，渐进检索先砍掉 **50.7%** Prompt Token；叠加四钩子网关后，综合通过率从 **56.67% 提升到 100%**，安全拦截率从 **25% 提升到 100%**，累计节省 **64.9%** Token。

---

## 第四章：三大目标赛道简历写法与本地动手实验

### 4.1 简历表达参考（根据投递岗位微调侧重点）
- **投 Go 通用后端**：突出 `sync.Pool` 压缩池化（耗时降 13.9× / 分配降 20.6×）、手写 16 分片 `O(1)` LRU + `Singleflight`、Redis ZSET 字典序同分榜（2,610 RPS / P99 18.81ms）、“429 采样 + 异步微批”解决连接池打爆故障（洪峰下 5xx=0 / P99 8.15ms）、Outbox + Kafka 幂等异步任务与 Lua Watchdog 分布式锁。
- **投 现代游戏平台后端**：突出多游戏 `game_id` 隔离、乐观锁云存档与历史恢复、C++20/gRPC 确定性回放校验（三态熔断器 + 服务端挑战种子 `match_tickets` 防刷）、支持万人全服异步发奖与 GM 双人审批审计闭环。
- **投 Go+AI 工程化**：突出独立实现的 Go 四钩子 AI 安全网关（PII 脱敏 / JSON Schema + OPA 额度策略 / 死循环熔断 / Groundedness 幻觉降级）、标准 MCP Server（HTTP + Stdio + RBAC）、渐进式检索与 30 用例 Golden Set 消融评测（Token -64.9%，拦截率 100%）、SSE 流式诊断与级联取消。

### 4.2 你自己如何动手复现与学习（推荐 4 步实操）
1. **启动完整环境**：
   ```powershell
   docker compose -f deploy/compose.yaml --profile gm --profile player --profile rules --profile observability up -d --build
   ```
   打开 Grafana `http://127.0.0.1:13000`（`admin` / `sandbox-admin`）观察实时指标。
2. **跑一次 `pprof` 内存/性能实验**：
   ```powershell
   cd backend
   go test -bench=BenchmarkSavePackUnpack -benchmem ./internal/service
   ```
   对比 Pooled 与 Unpooled 的 `ns/op` 和 `B/op`，再用 `curl -H "Authorization: Bearer sandbox-metrics-scrape-token" "http://127.0.0.1:18080/gm/system/pprof/heap?debug=1"` 观察线上堆快照。
3. **跑一次 Golden Set 消融评测**：
   ```powershell
   cd backend
   go run ./cmd/eval
   ```
   修改 [eval/main.go](../backend/cmd/eval/main.go) 中一条测试用例的金额或提示词，亲眼观察 4 钩子网关是如何拦截的。
4. **跑一次旗舰全链路验收**：
   ```powershell
   node tests/flagship-2027.mjs
   ```
   对照控制台输出与 [operations.go](../backend/internal/service/operations.go)、[distributed_jobs.go](../backend/internal/service/distributed_jobs.go) 代码，走一遍“审批 -> Outbox -> Redpanda -> 幂等消费 -> 邮件到账”的完整事务流。
