# ADR-0003：分级一致性模型、分布式韧性与统一四钩子 AI 安全网关

- 状态：已采纳（2026-11-26，演进自 [ADR-0001](0001-modular-monolith.md)）
- 关联文档：[升级总控方案](../UPGRADE_MASTER_PLAN.md)

## 1. 背景

在 [ADR-0001](0001-modular-monolith.md) 阶段，系统以“Go 模块化单体 + MySQL 本地同步事务”完成了 S0–S12 的正确性闭环，并明确约定“遇到实测瓶颈与生产扩展需求时再考虑缓存分层、批次任务与 Outbox”。随着系统面向 2027 现代游戏平台与高并发社招工程标准演进，源码剖析与压测暴露出四类单靠 MySQL 同步事务无法解决的瓶颈：

1. **读放大与热榜竞争**：排行榜 `nearby` 每次请求在事务内执行 `SELECT COUNT(*)`，且 `sessions` / `player_bans` / `games` 高频读取穿透至数据库。
2. **同步阻塞与级联雪崩**：`ops_events` 在 `429 rate_limited` 路径同步写入 MySQL，突增流量会反手耗尽数据库连接池；`gm_requests` 群发奖励硬编码 100 人同步事务上限；Go→C++ `RuleCore` gRPC 缺少熔断保护。
3. **多实例一致性缺失**：单机 `gameCache` 满 1024 直接 `clear()` 引发抖动，且 `limiter.go` 与本地缓存无法跨实例同步。
4. **AI 模块割裂与安全护栏缺位**：`opsagent`、`planner`、`companion` 各自发起外部 LLM 请求，缺少统一的 PII 脱敏、MCP 入参 Schema 校验、重复参数死循环熔断与 Groundedness 幻觉兜底。

## 2. 架构决策

保持 **Go 模块化单体 + MySQL 作为权威账本（System of Record）** 不变，不盲目拆分为分布式微服务，而是引入**分级一致性模型**与**统一 AI 安全网关**：

### 决策 A：三层分级一致性策略
1. **核心资产强一致层（保留 MySQL 单事务 + 唯一业务键）**：
   - 云存档 `revision` 乐观锁（`FOR UPDATE`）、`claimMail` + `creditInventory`（邮件领取与库存账本）、`gm_requests` 双人审批流转继续在 MySQL 单事务内原子提交。
   - **权衡理由**：避免为游戏内货币/道具引入分布式事务（2PC/TCC）的高复杂度与悬挂风险；即便上层 Redis 分布式锁因主从切换失效，MySQL 唯一业务键与行锁仍保证零超发、零重复结算。
2. **高频热读与跨实例协同层（L1 分片 LRU + `Singleflight` + L2 Redis + Pub/Sub）**：
   - `games` / `activities` 等低频写配置使用“本地 16 分片 `O(1)` LRU + Redis Pub/Sub 主动失效”；
   - 排行榜使用 Redis ZSET 字典序精确同分编码加速读查询，落实空值哨兵防穿透、`Singleflight` 防击穿、随机抖动 TTL 防雪崩；
   - 到期赛季自动结算使用“Redis Lua 分布式锁 + Watchdog 续期”做多实例互斥调度，MySQL `settled` 状态位兜底。
   - **降级策略**：未配置 `REDIS_ADDR` 或 Redis 短时不可达时，自动退化为单机 LRU + 单机限流 + MySQL 直查，不阻断核心业务。
3. **异步削峰与大批量任务层（Transactional Outbox + Redpanda/Kafka + DLQ）**：
   - 异常事件 `ops_events` 改为有界内存通道 + 突发 429 采样 + 后台微批异步写入，满队列非阻塞丢弃并计入 Prometheus 指标，彻底切断“限流写日志拖垮 DB”路径；
   - 大批量/全服发奖审批（`reward_batch`）通过 `outbox_events` 与业务审批在同一事务提交，由 Outbox Relay 异步投递至 Kafka Topic，消费者查写 `mq_consumed_events` 保证幂等，失败进入死信队列（DLQ）。

### 决策 B：独立基础设施子包解耦 `service` 包
将原 `internal/service/` 中的通用机制下沉为独立可测子包：`internal/cache`、`internal/resilience`、`internal/redisx`、`internal/lock`、`internal/mq`、`internal/aigateway`。

### 决策 C：统一四钩子 AI 安全网关（`internal/aigateway`）
收口 `opsagent`（Single Agent + MCP）、`planner`（Deterministic Workflow + 自动自修复）、`companion`（Zero-Tool 沙盒上下文），统一经过 `LLMInput -> ToolPreInvoke -> ToolPostInvoke -> LLMOutput` 四个确定性拦截钩子，所有拦截与降级事件持久化至 `ai_gateway_audits` 并暴露 Prometheus 指标。

## 3. 后果与边界

- **正面影响**：在保留单体开发与调试效率的前提下，系统同时具备高并发缓存抗穿透/击穿能力、万人异步任务吞吐、故障隔离与可观测性、以及可量化评测的 AI 工程安全护栏。
- **代价与运维约束**：Compose 完整形态新增 `redis` 与 `redpanda` 容器；API `/ready` 探针仅检查 MySQL 与 Schema 版本，**不因 Redpanda 短暂不可用而摘除 API 流量**（Outbox 表天然缓冲未投递消息），体现正确的依赖隔离原则。
