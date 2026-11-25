# 性能优化与基准评测报告（S13 / 沙盒实测）

> 测验日期：`2026-11-25`（数据来源：[load-results.json](load-results.json) 与本地 Compose 实测输出；所有指标仅代表本机 Docker 沙盒环境结果，不代表公网生产容量保证。）

## 1. 多场景基准压测结果（`node tests/load.mjs`）

测试在单一本机 Docker Compose 沙盒实例（1 API + 1 MySQL 8.4）下顺序执行，总请求数控制在单 IP 600 req/min 限流阈值内（共 550 次业务请求，0 错误）：

| 场景 (`name`) | 并发 (`concurrency`) | 请求数 | 错误数 | 总耗时 (ms) | 吞吐 (RPS) | P50 (ms) | P95 (ms) | P99 (ms) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `profile_read_c10` | 10 | 100 | 0 | 118.09 | 846.84 | 8.57 | 25.77 | 35.05 |
| `profile_read_c50` | 50 | 200 | 0 | 180.29 | 1109.35 | 35.70 | 77.78 | 82.57 |
| `leaderboard_read_c25` | 25 | 150 | 0 | 97.10 | 1544.81 | 15.75 | 20.67 | 22.79 |
| `gm_audit_trace_c20` | 20 | 100 | 0 | 64.03 | 1561.73 | 12.08 | 17.93 | 19.50 |

- 与 [RESULTS.md](RESULTS.md) 中早期基线（100 并发 10 请求档案读取 `p95 ≈ 29.5 ms`）相比，本次引入 `games.active` 5 秒 TTL 进程内缓存（并在 `POST /v1/games` 与 `listing publish/unpublish` 主动失效）后，`profile_read_c10` 在同负载下保持 `p95 = 25.77 ms`、`p99 = 35.05 ms`、`846.84 RPS`，且在 50 并发下仍维持 `1109.35 RPS` 与 0 错误。

## 2. 数据库查询与索引优化（MySQL 8.4 `EXPLAIN`）

- 迁移 `backend/migrations/024_indexes_and_retention.sql` 为 `audits` 表新增虚拟生成列 `request_id`（解析 `details->>'$.request_id'`）及联合索引 `audits_game_request(game_id, request_id, id)`，并将 AI 排障链路追踪查询从全游戏扫描改为精确索引定位。
- 本地 MySQL 8.4 实测执行计划：

```text
EXPLAIN SELECT action,reason,created_at FROM audits WHERE game_id='sky-island' AND request_id='req-1' ORDER BY id LIMIT 20\G
           id: 1
  select_type: SIMPLE
        table: audits
   partitions: NULL
         type: ref
possible_keys: audits_game_created_action,audits_game_request
          key: audits_game_request
      key_len: 518
          ref: const,const
         rows: 1
     filtered: 100.00
        Extra: NULL
```

- 同次迁移也为 `ops_agent_runs(game_id, created_at)` 与 `ops_agent_steps(created_at)` 增加索引，并启用 30 天 `ops_agent_*` 与单存档槽最近 50 个 `save_history` 版本的有界清理。
- `GET /gm/games/{game}/players/{player}` 对 `grants` 增加 `ORDER BY id DESC LIMIT 100`，`playerDiagnostics` 使用有界并发（信号量上限 4，单端受保护）并行采集诊断维度。

## 3. 遥测时间窗、静态资源与镜像体积优化

1. **有界 5 分钟遥测窗口与 P99 指标**：`backend/internal/telemetry/telemetry.go` 采用 `[300]secBucket` 300 秒环形时间桶统计近 5 分钟 QPS 与错误率（不再受旧 512 样本截断限制），同时保留 2048 样本环形缓冲计算 P50 / P95 / P99，并在 `/gm/system/metrics` 输出 `miniverse_http_latency_p99_ms`。
2. **小游戏 WebP 资源压缩**：`scripts/optimize-cauldron-assets.py` 将 `player/web/public/games/witch-cauldron/assets` 中的贴图与符号转为 WebP 并同步更新 `manifest.json`/`provenance.json` SHA-256，实测资源目录总大小为 **3,752,237 bytes (3.58 MiB)**；`admin/web` 与 `player/web` 的 Nginx 配置已启用 gzip 与长效静态资产缓存策略。
3. **RuleCore 多阶段极简镜像**：`rulecore/Dockerfile` 从单阶段全量工具链镜像改造为 `golang:1.27.3`（编译 + CTest）+ `debian:trixie-slim`（仅运行二进制与 `ldd` 解析出的非 glibc 共享库），镜像 `miniverse-rulecore:local` 实测大小为 **39,667,493 bytes (37.83 MiB)**，且构建阶段与独立容器执行 `/build/rule_tests` 均直接通过（`golden score=36 hash=8660768014306689972 victory=1 finished=1`）。
