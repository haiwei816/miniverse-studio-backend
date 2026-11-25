# 迁移、备份与恢复

迁移由 cmd/migrate 独立执行，Compose 等待 MySQL 健康、迁移成功后启动 API。migration_files 保存每个文件的 SHA256；schema_migrations 保存已应用数字版本。文件发生变化立即报错，不自动接受新 checksum。禁止修改已应用 SQL，新增迁移并更新 readiness 的目标版本。

MySQL DDL 不能通过普通事务完整回滚。本项目迁移以幂等 DDL/种子插入支持失败重跑；删除列、数据回填和破坏性升级需专门设计可恢复步骤。当前没有自动 down 命令，避免把破坏性回滚作为常规操作。

备份示例（Linux shell；密码在容器环境读取）：

```sh
docker compose -f deploy/compose.yaml exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" mysqldump -uminiverse --single-transaction --routines --triggers miniverse' > miniverse-backup.sql
```

恢复先建立独立测试数据库，检查备份结构、版本和条数；不要直接覆盖正在使用的数据库。将备份导入新库并以独立端口启动 API，执行只读核查与端到端测试，再计划切换。本地可通过 `node scripts/backup-verify.mjs` 自动执行 mysqldump 导出、导入临时库 `miniverse_backup_verify`、校验 `schema_migrations`/`migration_files` 与核心表行数并清理临时库的全链路恢复演练。

日志通过 `docker compose -f deploy/compose.yaml logs api` 查看，请求 ID 由服务端生成。不打印请求正文、令牌、凭证或数据库密码。健康检查和业务错误均记录状态及耗时。

`tests/recovery.mjs` 验证 API 重启保存会话/存档，MySQL 停止时 health=200/ready=503，恢复后持久数据不变；RuleCore 停止时验证失败关闭而普通存档继续可用。该测试只针对命名的本地 Compose 工程。

过期记录清理：API 启动时及之后每 5 分钟，分别从 `sessions`、`gm_sessions` 和 `platform_nonces` 按过期时间最多删除 1000 条过期超过一小时的记录，单表操作超时 10 秒。保留一小时缓冲有助于降低服务器时钟轻微偏差导致防重放记录被提前清理的风险；仍须保证各实例时钟同步。每个实例独立执行且数据库行级锁保证并发安全。日志 `expired_records_cleanup_batch_full` 表示该表本轮达到批次上限，应持续监控并按实际规模调整维护能力；`expired_records_cleanup_failed` 表示清理失败，应排查数据库。停止服务时清理任务随服务退出。迁移 014 为各表过期字段添加索引，支持中断后重跑。

已知容量取舍：榜单通过有序 LIMIT/OFFSET 返回 Top50/附近5名/结算前3名，Go 端不加载整榜；深位次附近查询和个人名次计数仍需数据库容量评估。GM 列表有固定上限；没有异步大规模广播，补发每批最多 100 人。需要生产容量时先依据观测数据优化和分页，而不是提前引入分布式中间件。

玩家平台的 `launch_grants`、`game_sessions` 以及防刷种子的 `match_tickets` 同样参与过期清理，每表遵循上述批次、缓冲和超时；同时周期清理 30 天前的 `ops_agent_runs`/`ops_agent_steps` 并为 `save_history` 保留最近 50 个版本。自动构建、检查、镜像交付与运行诊断见 [DevOps](devops.md)。
