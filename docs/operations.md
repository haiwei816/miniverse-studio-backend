# 迁移、备份与恢复

迁移由 cmd/migrate 独立执行，Compose 等待 MySQL 健康、迁移成功后启动 API。migration_files 保存每个文件的 SHA256；schema_migrations 保存已应用数字版本。文件发生变化立即报错，不自动接受新 checksum。禁止修改已应用 SQL，新增迁移并更新 readiness 的目标版本。

MySQL DDL 不能通过普通事务完整回滚。本项目迁移以幂等 DDL/种子插入支持失败重跑；删除列、数据回填和破坏性升级需专门设计可恢复步骤。当前没有自动 down 命令，避免把破坏性回滚作为常规操作。

备份示例（Linux shell；密码在容器环境读取）：

```sh
docker compose -f deploy/compose.yaml exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" mysqldump -uminiverse --single-transaction --routines --triggers miniverse' > miniverse-backup.sql
```

恢复先建立独立测试数据库，检查备份结构、版本和条数；不要直接覆盖正在使用的数据库。将备份导入新库并以独立端口启动 API，执行只读核查与端到端测试，再计划切换。当前仅执行了持久卷重启与依赖故障恢复，没有执行真实生产备份恢复。

日志通过 `docker compose -f deploy/compose.yaml logs api` 查看，请求 ID 由服务端生成。不打印请求正文、令牌、凭证或数据库密码。健康检查和业务错误均记录状态及耗时。

`tests/recovery.mjs` 验证 API 重启保存会话/存档，MySQL 停止时 health=200/ready=503，恢复后持久数据不变；RuleCore 停止时验证失败关闭而普通存档继续可用。该测试只针对命名的本地 Compose 工程。

已知容量取舍：榜单使用数据库窗口排名，Go 端只接收 Top50/附近5名/结算前3名；数据库排序仍需容量评估，GM 列表有固定上限；没有异步大规模广播，补发每批最多 100 人。限流与用户会话过期清理当前适用于小型单体。需要生产容量时先依据观测数据优化和分页，而不是提前引入分布式中间件。
