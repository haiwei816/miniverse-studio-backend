# 邮件与背包增强验收

日期：2026-11-07。基线：`40aef70` 加本地客户端接入改动。改动提交于 `feat/client-only-onboarding` 分支，随后合并入 `main`。

## 交付与环境

- 新增邮件已读、软删除、筛选、一键领取；道具目录、幂等消耗、分页流水；GM 玩家详情可查看库存、流水及邮件状态。
- 新增第 13 版迁移与 Godot SDK 方法、示例场景和联网测试。原 API 返回格式保持兼容，邮件只增加 read 字段。
- Windows + Go 1.27.3 + Godot 4.7.2；Docker Desktop 已启动，Docker Engine 29.6.1、MySQL 8.4。
- 隔离 Compose 项目 `miniverse-mail-check`，API 使用本机 18180，MySQL 不暴露宿主机端口；使用新建测试数据卷。

## 已执行

| 检查 | 结果 |
| --- | --- |
| `go test ./...`、`go vet ./...`、`go build ./...` | 通过 |
| Linux Go 1.27.3 容器内 `go test -race ./...` | 通过；未配置的既有 memory DB 集成测试按原规则跳过 |
| Compose 首次构建、初始化 MySQL、执行 13 个迁移、启动 API | 通过 |
| `tests/mail-inventory.mjs` | 通过，真实 API/MySQL；新增 GM 查询断言后重新通过 |
| `tests/mail-inventory-upgrade.mjs` | 通过，独立旧库从 12→13；库存与已领邮件保留，期初流水唯一，重复迁移不重复记账 |
| `tests/migrations.mjs` | 通过，13 个文件受跟踪，重复执行无变化，旧迁移篡改被拒绝 |
| 原 HTTP 回归 s0、s1、s1-isolation、s1-expiry、s3、s4、s5、s6、s7、s8、s9、s10、s11 | 全部通过；新增 GM 查询后另重跑 s7 通过 |
| `tests/production-boundary.mjs` | 通过，生产环境仍拒绝 sandbox 会话/管理夹具/模拟渠道 |
| Godot `test_sdk.gd`、`test_saves.gd`、`test_local_bridge.gd`、`test_mail_inventory.gd` | 全部通过；邮件库存测试实际连接本轮 API/MySQL |
| Godot editor import、邮件库存示例 headless 启动 | 通过，无脚本解析错误；不等于人工 UI 体验验收 |

新 HTTP 测试覆盖：

- 标记已读与重复操作；禁止删除有效未领附件；已领/过期可删除，删除后不可读/领取。
- 六个一键领取与单封领取并发，不重复入账；101 封邮件按 100+1 两批领取。
- 八次相同消耗请求只扣一次；请求 ID 改参数冲突；八笔独立消耗在不足余额下仅允许足额的三笔成功。
- 失败事务不留下幂等占位；重试首次成功结果不受后续余额影响。
- 批量领取第二次入账溢出时，首封邮件、余额和流水整批回滚。
- 未知道具、负数、零、额外身份字段、错误分页参数被拒绝。
- 流水增减合计与余额一致，游标分页不重复，跨玩家/跨游戏隔离；GM 授权查询包含删除状态。

## 边界与后续

本轮未启动未修改的 C++ RuleCore，未重跑 s12、完整 recovery 或长时间容量测试；不宣称完成全部系统生产验收。远端 CI 已加入新测试，但尚未推送或运行。真实平台认证仍未接入。

邮件目前单附件，库存为可堆叠数量；消耗不自动施加游戏效果。旧库存只有期初余额记录，不能重建旧历史。升级需要旧 API 停写，不能混跑新旧库存写入实现。

测试容器在验收后停止并保留本轮测试数据卷，复现命令和升级步骤见 [邮件库存说明](../mail-inventory.md)。
