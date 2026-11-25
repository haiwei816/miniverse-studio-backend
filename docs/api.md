# HTTP v1 接口契约

JSON 请求使用 Content-Type: application/json。会话支持 `Authorization: Bearer TOKEN`，Web 端亦使用 `HttpOnly; SameSite=Strict` Cookie（`mv_player_session` / `mv_gm_session`）配合写操作请求头 `X-MiniVerse-CSRF: 1`。未知字段/多段 JSON 拒绝。请求正文最多 2 MiB；存档解压后最多 1 MiB。业务错误为 `{"error":"code"}`；400 输入、401 身份、403 权限/封禁/CSRF、404 资源、409 版本/状态/幂等冲突、410 邮件过期、422 非法回放/种子不匹配、429 限流、503 依赖不可用。服务端返回 X-Request-ID。

下表中的 `{game}` 是 sky-island 或 card-game。普通玩家接口不会接受请求正文中的 player_id 替换会话身份。

| 接口 | 行为 / 主要字段 |
|---|---|
| GET /health、/ready | 进程存活 / MySQL 与当前迁移版本就绪 |
| GET /v1/games | 活跃游戏与配置版本 |
| POST /v1/auth/guest | sandbox；首次 `{}`，恢复 `{secret}`；返回 player_id、secret、access_token、expires_at |
| POST /v1/auth/logout | 注销当前玩家会话 |
| POST /v1/games | sandbox 注册；game_id、name；需要玩家会话 |
| GET/PATCH /v1/games/{game}/profile | 获取自己的分游戏档案 / 修改 name |
| GET /v1/games/{game}/saves/{slot} | 当前 revision/schema_version/data/checksum/deleted |
| PUT /v1/games/{game}/saves/{slot} | revision、schema_version、JSON 字符串 data、SHA256 checksum；可选 encoding=gzip（data 为 base64） |
| DELETE /v1/games/{game}/saves/{slot} | 请求 `{revision}`；写删除墓碑并增加版本 |
| GET /v1/games/{game}/saves/{slot}/history | 最近 50 个历史版本元数据 |
| PUT /v1/games/{game}/saves/{slot} | 恢复请求 `{revision:当前版本,restore_revision:历史版本}`，产生新版本 |
| PUT /v1/games/{game}/activities | sandbox 配置；生产需 GM 申请和审核 |
| GET /v1/games/{game}/activities | 活动配置、自己的进度和完成状态 |
| POST /v1/games/{game}/events | event_id、event_type、amount；来源由服务端固定为 client |
| POST /v1/games/{game}/activities/{activity}/reward | 对已完成活动补建幂等奖励；正常事件完成会自动生成 |
| GET /v1/games/{game}/mail | 附件、过期、领取、read 状态；未删除的最近 200 封，支持 filter=all/unread/unclaimed 与 limit |
| GET /v1/games/{game}/mail/page | 游标分页邮件，支持 filter、limit、cursor，返回 items 与 next_cursor |
| POST /v1/games/{game}/mail/{mail}/claim | 邮件领取与权威库存入账同一事务；重复返回成功 |
| POST /v1/games/{game}/mail/{mail}/read | 标记已读，不领取附件 |
| DELETE /v1/games/{game}/mail/{mail} | 软删除；有效未领附件返回409，过期或已领可删除 |
| POST /v1/games/{game}/mail/claim-all | 原子领取最多100封，返回 claimed、count、has_more |
| GET /v1/games/{game}/inventory | 分游戏的服务端权威道具余额 |
| GET /v1/games/{game}/items | 道具目录和自身数量，after/limit 游标分页 |
| POST /v1/games/{game}/inventory/consume | request_id、item、amount；幂等扣除自身库存，参数冲突/余额不足返回409 |
| GET /v1/games/{game}/inventory/ledger | 增减流水，before/limit 游标分页 |
| PUT /v1/games/{game}/boards | sandbox 榜单配置：id、season、mode=max/sum/min、时间窗、奖励 |
| GET /v1/games/{game}/boards/{board}/{season} | Top50、自己附近五名和 trust 标记 |
| POST /v1/games/{game}/boards/{board}/{season}/scores | request_id、score；普通休闲榜，可信榜拒绝该入口 |
| POST /v1/games/{game}/boards/{board}/{season}/settle | sandbox，截止后冻结并给 Top3 发配置奖励；幂等 |
| GET /v1/games/{game}/announcements | 未过期公告 |
| POST /v1/sandbox/tickets/{channel} | sandbox 凭证：game_id、subject；channel=steam/taptap |
| POST /v1/platform/{channel}/login、bind、unbind | game_id、ticket；绑定/解绑还需已有玩家会话 |
| POST /v1/games/card-game/matches/start | 签发服务端随机 seed 与一次性开局票据 match_id（15 分钟有效） |
| POST /v1/games/card-game/replays/validate | version、seed、actions；可选 claimed_score、request_id、board、season、match_id（ranked 榜强制要求票据，返回 trust=server_seeded_replay） |

## GM 会话与授权

GM token 和玩家 token 完全分离。POST /gm/login 接受 username、password；GET /gm/games 返回当前管理员已授权的活跃游戏列表。所有 `/gm/games/{game}` 接口核对管理员角色与 gm_access；support 只读，operator 创建申请，reviewer 审批，admin 可做两类操作但不能批准自己的申请。会话三十分钟过期。

| GM 路径（前缀 /gm/games/{game}） | 行为 |
|---|---|
| GET /overview | 看板统计：玩家、有效会话玩家、存档、待领邮件、活跃活动、有余额道具、待审申请、24h 审计/失败/发奖，以及 7 天（UTC 分桶）审计与发奖序列；全部为服务端计数。为兼容已有客户端，`totals.online` 字段名保持不变，表示具有未过期会话且拥有该游戏资料的不同玩家数，并非实时在线人数。 |
| GET /players?q= | 按 UID/名称查询，最多 100 条 |
| GET /players/{player} | 档案、封禁状态（ban）、存档槽版本、最近100条奖励流水、库存、最近50条库存流水和最近100封邮件 |
| POST /players/{player}/ban、unban | admin/reviewer 执行封禁或解封；封禁时原子吊销 sessions/game_sessions/launch_grants 并写审计 |
| GET /audit | 最近 200 条管理写操作审计 |
| GET /failures | 授权游戏的失败管理操作和非法分数/回放异常；不记录凭据或请求正文 |
| GET /boards | 查看当前游戏的排行榜配置、规则校验版本、require_ticket 与结算状态 |
| POST /requests | id、kind=reward/activity/activity_stop/board_upsert/season_settle/player_ban、reason、payload；稳定 id 防重复，变更 payload 返回409 |
| GET /requests | 申请状态与执行记录 |
| POST /requests/{id}/approve、cancel | reason 必填；独立审核通过时原子发奖/发布并审计 |
| POST /activities/{id}/stop | reason；停止活动 |
| GET /activities/{id}/versions | 读取历史，复制为更大版本号申请以回滚 |
| PUT /announcements/{id} | body、expires_at、reason |
| GET /planner/catalog | 合法道具、规则枚举、JSON Schema |
| POST /planner/generate | brief；确定性 mock 输出经过验证的草稿和模拟信息，不直接发布 |
| POST /planner/validate | config、events=[{amount,time}]；Schema + 业务校验 + 模拟 |
| GET /planner/runs | mock 评测统计，真实 AI 未验证 |

奖励 payload：`{"players":["UID"],"item":"wood","amount":10}`，每批最多100人。活动结构参考 backend/internal/service/activity.schema.json。每日任务/签到按服务端 UTC 日历分桶，签到目标固定1；活动新版采用独立进度空间。S8 发布与回滚均需原因和另一位审核人。

## 保证边界

活动事件、进度、自动发奖在同一事务内，不存在另一个不可靠的异步投递步骤。稳定业务键约束邮件唯一，领取锁保护服务器库存。云存档不能成为权威货币来源。重放校验只证明规则一致性；mock 渠道不证明外部用户身份；mock 生成率不代表真实 AI 能力。

测试脚本既是可执行用例，也提供请求示例：tests/s1.mjs 到 tests/s12.mjs。

邮件/背包的重试约定、期初流水、停写升级和 Godot 示例详见 [邮件与背包库存](mail-inventory.md)。
