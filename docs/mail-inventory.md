# 邮件与背包库存

当前提供按游戏和玩家隔离的奖励邮件、堆叠道具数量、消耗及增减流水。普通客户端只能查询自己的资源、领取邮件和扣除自己的道具；发奖继续通过活动、赛季或 GM 审批，客户端不能直接增加库存。

## 邮件

以下路径前缀为 `/v1/games/{game}`，使用玩家 Bearer 会话。

| 接口 | 行为 |
| --- | --- |
| `GET /mail?filter=all&limit=200` | 默认返回最近 200 封未删除邮件；filter 可选 all、unread、unclaimed；limit 为 1–200 |
| `GET /mail/page?filter=all&limit=200&cursor=...` | ?? `items` ? `next_cursor`?????????????????????? ID ???? |
| `POST /mail/{id}/read` | 标记已读，重复调用成功，不领取附件 |
| `POST /mail/{id}/claim` | 领取附件，同时标记已读；重复领取不重复入账 |
| `POST /mail/claim-all` | 同一事务最多领取 100 封未过期、未删除邮件，返回 claimed ID 数组、count、has_more |
| `DELETE /mail/{id}` | 软删除已领取或已过期邮件；有效且未领取的附件返回 409，避免误删奖励 |

?????????????????????????????????/??????????????????????????????? `GET /mail` ????????????????????????????????????

列表继续返回数组，保留原字段并增加 `read`。删除不移除奖励流水及领取凭证，已删除邮件不会重新出现在玩家列表，之后读/领取返回 404；重复删除成功。GM 仍可查到删除状态。领取过期附件返回 410；越权读取或操作其他玩家/游戏邮件返回 404。

一键领取的 `has_more=true` 表示还有下一批，由客户端再次调用；不在一个请求里无限领取。过期邮件会跳过。批内出现库存溢出、数据库失败等问题时整批回滚。单封领取、一键领取和道具消耗并发时通过数据库行锁和事务维护一致性。

## 库存与流水

| 接口 | 行为 |
| --- | --- |
| `GET /inventory` | 保留原来的 `{道具ID:数量}` 格式 |
| `GET /items?after=&limit=100` | 道具目录与自己的数量，返回 items 和 next_cursor；下一页传 after=next_cursor，limit 为 1–200 |
| `POST /inventory/consume` | 请求 `{request_id,item,amount}`，扣除自己的道具，返回原请求及扣除后的 balance |
| `GET /inventory/ledger?before=&limit=50` | 最新增减流水，返回 entries 和 next_cursor；下一页传 before=next_cursor，limit 为 1–200 |

消耗数量为 1–1000000 的整数，request_id 非空且最多 128 字节，道具 ID 最多 64 字节。未知道具返回 400，余额不足返回 409 `insufficient_items`；请求中的额外 player_id 等字段会被拒绝。余额、请求结果和负向流水在同一事务内写入。

同一玩家、同一游戏的同一 request_id：

- 相同 item/amount：返回首次成功结果，不再次扣款，即使之后余额已改变。
- 不同 item/amount：返回 409 `idempotency_conflict`。
- 首次事务失败：不留下已完成标记，修正余额或输入后可重试。

每次实际消耗生成新请求 ID；网络超时重试必须沿用原 ID 和参数。消耗失败时客户端不能先行授予收益。本接口只负责数量扣除，不会自动治疗、增加经验、装备道具或修改客户端存档；这些行为需由具体游戏定义并在服务端事务内实现。

流水包含 id、item、delta、balance、reason、business_key、created_at。邮件入账为 `mail_claim`，主动消耗为 `client_consume`，升级时原有余额为 `migration_opening`。这覆盖本次升级后的增减，不能还原升级前每一次消耗。

GM 的 `GET /gm/games/{game}/players/{player}` 增加 inventory、最近 50 条 inventory_ledger 和最近 100 封 mail（包括删除状态）。现有管理网页的玩家详情直接展示这些信息，沿用管理员角色和授权游戏校验，没有新增绕过审批的任意加道具入口。

## Godot 接入与演示

运行 `sdk/godot/mail_inventory_demo.tscn`。登录后点击三次示例建筑任务可生成奖励邮件，再体验已读、单封/一键领取、删除、道具消耗与流水。恢复凭据仅保留在内存，可手动复制用于本地测试。

示例在一次消耗成功后仍保留 request_id，重复点击只重放结果；点击“开始一笔新的消耗操作”才会再次扣除。正式游戏应把 ID 与具体业务动作绑定，并按需要安全持久化未决请求。

SDK 新方法：`mail_list`、`read_mail`、`delete_mail`、`claim_mail`、`claim_all_mail`、`inventory`、`item_catalog`、`consume_item`、`inventory_ledger`。可设置 client.base_url，或用 `MINIVERSE_API_URL` 环境变量覆盖默认测试地址。

## 升级与验证

新增 `013_mail_inventory.sql`，不修改已应用的旧迁移文件。readiness 现在要求第 13 版迁移已完成。升级前备份并停止旧 API 的库存写入，执行迁移后启动新 API，再切换流量；不能让旧 API 和新 API 并行修改库存，否则旧 API 不会生成新流水。

迁移新增邮件状态、消耗幂等请求和库存流水表，为已有正余额生成唯一的期初记录。保留原 mail/items/inventory 表结构，所以已有发奖与种子数据继续兼容。迁移失败可按既有方式修复后重跑；不要通过删除数据卷修复。

启动独立 sandbox 后执行：

```powershell
$env:COMPOSE_PROJECT_NAME = 'miniverse-mail-check'
$env:API_PORT = '18180'
$env:API_URL = 'http://127.0.0.1:18180'
docker compose -p miniverse-mail-check -f deploy/compose.yaml up -d --build
node tests/mail-inventory.mjs
node tests/mail-inventory-upgrade.mjs
$env:MINIVERSE_API_URL = $env:API_URL
godot --headless --path sdk/godot --editor --import --quit
godot --headless --path sdk/godot --script test_mail_inventory.gd
```

Node 测试直接写入该 sandbox 的随机邮件夹具，不得对生产环境运行。升级测试使用独立随机数据库，成功后只删除自己创建的数据库，失败则保留诊断。验证结果见 [本轮验收](verification/MAIL_INVENTORY.md)。

当前仍不包含完整装备实例、背包格子/容量、道具有效期、合成交易、多附件邮件、邮件模板和大规模群发队列。它们应按首款游戏的实际规则继续扩展。
