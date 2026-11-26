# MiniVerse Studio Backend — 2027 旗舰升级实测战报与消融评测（Verification & Benchmark Report）

> 实测日期：`2026-11-26`  
> 硬件/宿主环境：`12th Gen Intel(R) Core(TM) i3-12100F`（Windows 11 + Docker Desktop Compose 沙盒）  
> 数据来源：全部来自本机真实执行的 [bench-2027-results.json](bench-2027-results.json)、[golden-set-eval-2027.json](golden-set-eval-2027.json) 与 Go Benchmark 原始输出（仅代表本机沙盒实测结果）。

---

## 一、 Go 内存与 GC 微基准对比（`sync.Pool` 云存档编解码）

在 [saves.go](../../backend/internal/service/saves.go) 中，原实现每次压缩/解压云存档都会新建 `gzip.Writer`（`compress/flate` 内部初始化约 800KB 字典结构）与 `bytes.Buffer`。改造后使用 `sync.Pool` 复用对象，并对超过 `64 KB` 的大 Buffer 拒绝回池以防常驻内存钉死。

在本机执行 `go test -bench=BenchmarkSavePackUnpack -benchmem ./internal/service` 实测结果如下：

| 实现版本 | 每次操作耗时 (`ns/op`) | 每次堆分配 (`B/op`) | 每次分配次数 (`allocs/op`) | 提升倍数 |
| --- | ---: | ---: | ---: | --- |
| **优化前（`Unpooled`）** | `254,478 ns/op` | `1,124,196 B/op` (~1.07 MiB) | `30 allocs/op` | 基线 |
| **优化后（`Pooled`）** | **`18,328 ns/op`** | **`54,633 B/op`** (~53.3 KiB) | **`10 allocs/op`** | **耗时降低 13.9×，堆内存分配减少 20.6×** |

---

## 二、 稳态高并发 HTTP 压测与故障隔离实测（`cmd/bench`）

测试方法：使用原生 Go 压测工具 [cmd/bench/main.go](../../backend/cmd/bench/main.go)，在临时开启本机内网 `TRUSTED_PROXIES` 模拟多离散客户端 IP（`X-Forwarded-For`）的条件下持续压测（测试结束后已立即恢复默认安全配置），原始结果见 [bench-2027-results.json](bench-2027-results.json)。

| 压测场景 | 并发度 | 持续时长 | 总请求数 | 2xx 成功数 | 429 限流数 | 5xx 错误数 | 吞吐 (`RPS`) | P50 (`ms`) | P95 (`ms`) | P99 (`ms`) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `http_health_c32` | 32 | 8.00s | 114,659 | 114,659 | 0 | **0** | **14,326.96** | 2.08 | 4.27 | 6.44 |
| `leaderboard_redis_zset_c20` | 20 | 3.00s | 7,843 | 7,843 | 0 | **0** | **2,610.06** | 6.91 | 13.04 | 18.81 |
| `profile_read_c20`（含 429 洪峰） | 20 | 3.00s | 31,435 | 3,465 | 27,970 | **0** | **10,465.17** | 1.56 | 5.83 | 8.15 |

### 核心工程结论（可用于面试“线上故障隔离”复盘）
1. **Redis ZSET 字典序同分榜单吞吐提升**：在 20 并发稳态持续压测 7,843 次请求下，排行榜读取保持 **2,610 RPS**、**0 错误**、**P99 = 18.81 ms**（早期 [load-results.json](load-results.json) 仅短测 150 次请求为 1,544 RPS）。
2. **验证“429 限流写库隔离”修复效果**：在 `profile_read_c20` 场景中，3 秒内触发了 **27,970 次 `429 rate_limited`**。改造前每次 429 同步 `INSERT INTO ops_events` 会瞬间打满 16 个 MySQL 连接池；改造后依靠 [ops_observability.go](../../backend/internal/service/ops_observability.go) 的 **429 按秒采样 + 有界队列 + 异步微批落盘**，全过程 **5xx 错误为 0**，正常 2xx 请求（3,465 次）与整体 P99 稳定在 **8.15 ms**。

---

## 三、 30 用例 Golden Set 离线消融评测（`cmd/eval`）

使用 [cmd/eval/main.go](../../backend/cmd/eval/main.go) 内置的确定性故障注入 Mock LLM Server 对 **30 个合成故障与安全攻防用例**（涵盖：8 例奖励未到账、6 例 RuleCore 503 故障、4 例 PII 隐私泄露测试、3 例 Prompt 注入越权、3 例 OPA 超额补发拦截、3 例死循环工具调用陷阱、3 例编造 `request_id`/谎称直接发奖幻觉）进行三组消融对比，原始输出见 [golden-set-eval-2027.json](golden-set-eval-2027.json)：

| 消融实验组别 (`variant`) | 用例总数 | 通过数 | 综合通过率 | 安全/幻觉拦截率 | 平均工具步数 | 总 Prompt Tokens | 平均耗时 (`ms`) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `1_baseline_no_gateway_full_ctx`（无护栏 + 全量上下文） | 30 | 17 | 56.67% | 25.00% | 1.70 | 49,697 | 0.70 |
| `2_progressive_retrieval_only`（仅渐进式检索） | 30 | 17 | 56.67% | 25.00% | 1.50 | **24,491** (`-50.7%`) | 0.43 |
| `3_progressive_plus_4hook_gateway`（渐进检索 + 完整四钩子网关） | 30 | **30** | **100.00%** | **100.00%** | **1.10** | **17,446** (`-64.9%`) | 0.57 |

### 消融实验发现
- **渐进式检索（`search_runbook` + `inspect_player_summary`）** 独立将 Prompt Token 消耗从 `49,697` 降至 `24,491`（**节省 50.7%**），但不能阻挡 Prompt 注入、超额补发、PII 出网与幻觉。
- **叠加四钩子安全网关（`internal/aigateway`）** 后：
  - `LLMInputHook` 拦截全部 Prompt 注入并脱敏手机号/邮箱/密钥；
  - `ToolPreInvokeHook` 通过 OPA 策略拦下 50 万钻石超额补发，并在第 2 步立即熔断死循环工具调用；
  - `LLMOutputHook` 检出捏造的 24 位 `request_id` 与“已直接发放”谎言，自动降级为确定性规则结论；
  - 综合通过率与安全拦截率均从 56.67% / 25% 跃升至 **100%**，总 Prompt Token 进一步降至 `17,446`（**累计节省 64.9%**）。

---

## 四、 端到端全量自动化回归清单（2026-11-26）

在 `mysql:8.4`、`redis:7.4-alpine`、`redpanda:v24.2.11`、`api`、`rulecore`、`gm`、`player` 全栈启动后实测通过：

| 测试脚本 | 验证范围 | 结果 |
| --- | --- | --- |
| `go test ./...` & `go vet ./...` | 全部 Go 子包并发、缓存、LRU、熔断器、Lua 锁、MQ、AI 网关单测与静态检查 | **PASS** |
| `node tests/migrations.mjs` | 26 个迁移版本（含 `026_distributed_and_aigateway.sql`）、幂等重跑与历史文件篡改拒绝 | **PASS** |
| `node tests/e2e.mjs` | S0–S12 全部游戏核心里程碑（含 C++20 gRPC 确定性回放校验） | **PASS** |
| `node tests/ops-agent-mcp.mjs` | Ops Agent 诊断提案、RuleCore 断网恢复、12 个 MCP 工具与 stdio 桥接 | **PASS** |
| `node tests/domain-loops.mjs` | 服务端挑战种子 `match_tickets`、GM 榜单审批、封禁强踢与 MCP 冻结 | **PASS** |
| `node tests/companion.mjs` | 跨游戏 AI 伙伴记忆隔离、epoch 冲突与安全登出 | **PASS** |
| `node tests/mail-inventory.mjs` | 邮件领取、并发扣减、账本流水与游标分页 | **PASS** |
| `node tests/ranking-memory.mjs` | 20,001 条成绩 Redis ZSET 校验与 Top50 / nearby5 / 并列第 10,001 名次精确返回 | **PASS** |
| `node tests/flagship-2027.mjs` | W3C `traceparent`、Redis ZSET 同分先达、Outbox+Redpanda 异步群发、渐进检索、SSE 流式诊断、RBAC `pprof` | **PASS** |
| `node tests/recovery.mjs` | API 重启、MySQL 断连恢复与 RuleCore 停服 Fail-Closed 恢复 | **PASS** |
