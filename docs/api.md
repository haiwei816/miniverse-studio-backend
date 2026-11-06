# HTTP v1 接口契约

JSON 请求使用 Content-Type: application/json。会话通过 Authorization: Bearer TOKEN 传递。未知字段/多段 JSON 拒绝。请求正文最多 2 MiB；存档解压后最多 1 MiB。业务错误为 `{"error":"code"}`；400 输入、401 身份、403 权限、404 资源、409 版本/状态/幂等冲突、410 邮件过期、422 非法回放、429 限流、503 依赖不可用。服务端返回 X-Request-ID。

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
| GET /v1/games/{game}/mail | 附件、过期、领取状态与历史；最近 200 封 |
| POST /v1/games/{game}/mail/{mail}/claim | 邮件领取与权威库存入账同一事务；重复返回成功 |
| GET /v1/games/{game}/inventory | 分游戏的服务端权威道具余额 |
| PUT /v1/games/{game}/boards | sandbox 榜单配置：id、season、mode=max/sum/min、时间窗、奖励 |
| GET /v1/games/{game}/boards/{board}/{season} | Top50、自己附近五名和 trust 标记 |
| POST /v1/games/{game}/boards/{board}/{season}/scores | request_id、score；普通休闲榜，可信榜拒绝该入口 |
| POST /v1/games/{game}/boards/{board}/{season}/settle | sandbox，截止后冻结并给 Top3 发配置奖励；幂等 |
| GET /v1/games/{game}/announcements | 未过期公告 |
| POST /v1/sandbox/tickets/{channel} | sandbox 凭证：game_id、subject；channel=steam/taptap |
| POST /v1/platform/{channel}/login、bind、unbind | game_id、ticket；绑定/解绑还需已有玩家会话 |
| POST /v1/games/card-game/replays/validate | version、seed、actions；可选 claimed_score、request_id、board、season；见 rulecore.md |

## GM 会话与授权

GM token 和玩家 token 完全分离。POST /gm/login 接受 username、password。所有 `/gm/games/{game}` 接口核对管理员角色与 gm_access；support 只读，operator 创建申请，reviewer 审批，admin 可做两类操作但不能批准自己的申请。会话三十分钟过期。

| GM 路径（前缀 /gm/games/{game}） | 行为 |
|---|---|
| GET /players?q= | 按 UID/名称查询，最多 100 条 |
| GET /players/{player} | 档案、存档槽版本、奖励流水 |
| GET /audit | 最近 200 条管理写操作审计 |
| GET /failures | 授权游戏的失败管理操作和非法分数/回放异常；不记录凭据或请求正文 |
| POST /requests | id、kind=reward/activity、reason、payload；稳定 id 防重复，变更 payload 返回409 |
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
