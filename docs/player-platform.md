# MiniVerse 玩家平台：运行与接入

当前软件版提供独立玩家网站、账号密码注册登录、游戏目录与详情、收藏/最近游玩、隔离的网页游戏启动、云存档接入示例、伙伴设置/文字对话/显式记忆，以及开发者发行资料和独立审核。硬件仅预留接口。它与开发者控制台是两个独立前端。

## 运行

在仓库根目录执行：

```sh
docker compose -f deploy/compose.yaml --profile player --profile gm up -d --build
```

- 玩家平台：`http://127.0.0.1:18082`，使用页面上的注册入口创建自己的玩家账号。
- 开发者控制台：`http://127.0.0.1:18081`，使用预先分配的管理账号；sandbox 账号见 README。
- API：`http://127.0.0.1:18080`。
- `PLAYER_PORT`、`GM_PORT`、`API_PORT` 可以修改映射端口。已有独立测试环境时应使用一致的 Compose 项目名和端口。

数据库迁移必须先完成，不能将旧数据库直接连到新 API 而跳过迁移。当前最高迁移为 017；保留以前的数据卷及已应用迁移文件。

开发前端：

```sh
cd player/web
npm ci
npm run dev
```

Vite 将 `/v1` 请求代理到本地 API，具体目标见 `vite.config.js`。开发者控制台仍在 `admin/web`，不要把玩家账号用于 GM 登录。

## 玩家功能

账号格式为 3–32 个小写英文字母、数字、下划线或连字符，首字符为字母或数字，输入会规范化为小写；密码长度 8–128 字节。PBKDF2 哈希保存在后端；客户端不保存密码，Bearer 会话仅保存在页面内存，刷新需要重新登录。忘记密码、邮件/短信验证和支付不包含在本软件版中。

玩家可以未登录浏览目录；收藏、启动、伙伴和设备接口需要登录。游戏库包含已收藏或已经启动的已发布游戏。示例为两款轻量浏览器游戏，用于演示登录交接、玩法、云存档和伙伴上下文；不代表用户原有 Godot 游戏已自动完成移植。

游戏资料由开发者控制台编辑，保存草稿不改变玩家已看到的已发布版本。提交后，另一个有项目审核权限的人才能发布。下架后不能新启动；已存在会话的处理由当前会话和权限规则控制。外部开发者自助入驻、组织邀请和原生 Windows/APK 包体安装发行没有在此版实现，当前开发者权限沿用已分配的游戏级 GM 授权。

## 启动协议与信任边界

关闭游戏时，平台向 `POST /v1/platform/companion/context` 提交 `{ "game_id": "", "expected_epoch": 当前游戏代次 }`。后端在事务内核对代次，已经切入新世界时返回 `409 companion_context_changed`，不会清掉新游戏。新旧平台退出接口都在同一事务中清空伙伴世界并撤销当前会话，失败整体回滚。

1. 玩家前端用平台会话请求 `POST /v1/platform/games/{game}/launch`，附随机 verifier 的 SHA-256 base64url `code_challenge`。
2. 后端为当前世界切换 context epoch，返回一分钟有效的一次性 `launch_code`、游戏入口和 `epoch`。
3. 可信平台前端调用 `POST /v1/platform/launch/exchange`，提交 code、game_id 和 `code_verifier`，得到仅能访问对应游戏的短期会话。
4. 网页游戏放在 `sandbox="allow-scripts"` 的 opaque iframe 中；不允许同源父页面访问。游戏凭证仍由平台前端持有，iframe 通过受限消息桥请求固定的游戏接口。
5. 消息必须匹配当前 iframe 的 `source`、opaque origin 和随机 channel；桥只允许约定的方法及路径，限制消息大小和并发。平台长期 token 不发送给游戏。对 opaque iframe 使用 `targetOrigin:"*"` 仅用于该已绑定 window 的游戏数据消息。
6. 切换或重新进入游戏递增 epoch；旧游戏会话和未兑换的旧票据不能继续使用。平台会话登出或到期后，派生会话失效。

Godot 接入可以使用 `platform_login`（仅可信平台客户端）与 `exchange_launch`（游戏客户端）。独立游戏应接收短期票据，不要求玩家重新在每个游戏中输入平台密码。设备、GM 和游戏会话不能互换。

当前只接受 `/games/` 下的已发布网页入口，不从开发者输入执行任意本地命令。公开接入外部内容前，仍需独立内容审核、原生分发策略及生产部署验证。

## 伙伴与模型配置

游戏聊天入口当前不查询、不发送玩家全局长期记忆；可信平台聊天可使用玩家明确保存的全局记忆。尚未提供逐游戏的记忆共享授权，因此游戏聊天默认不共享，不能靠提示词约束代替访问控制。

伙伴 ID 与玩家绑定，名字和相处风格可修改。世界状态按前台游戏/epoch切换，游戏资产不跨游戏共享。长期记忆只包含玩家明确保存的条目，可随时删除；对话历史为有界的短期上下文，不自动写入长期记忆。

服务端可配置完整的 chat-completions 兼容地址：

```text
COMPANION_CHAT_URL=https://your-provider.example/v1/chat/completions
COMPANION_API_KEY=配置在服务端，不放入前端代码
COMPANION_MODEL=实际可用模型名
```

将变量通过运行环境传入 Compose；如使用 `.env`，遵循项目启动位置和 Docker Compose 读取规则，不提交真实密钥。没有配置时，sandbox 明确返回 `mode:demo` 的规则演示文字；非 sandbox 返回 503，不伪装成真实 AI。真实外部模型的质量、成本和延迟需要单独验证。配置外部服务后，当前消息、获准记忆与当前世界上下文会发送至该服务，部署方应向玩家明确说明处理范围。

模型只返回文字，`actions` 当前为空，不执行发奖、改档、支付或硬件控制。每玩家每分钟最多 30 次对话、单进程最多 16 个并发模型请求。网络/模型失败不应阻断游戏存档。

## 主要 API

发行资料 `GET /gm/games/{game}/listing` 返回 `draft_hash`。提交和发布必须带 `{ "draft_hash": "读取到的指纹" }`，缺失或格式错误返回 400，草稿已变化返回 `409 listing_draft_changed`，应刷新并重新核对。接口一次查询读取资料与状态，后端锁定草稿后再核对指纹。网页存在未保存修改时不允许提交或发布；下架不需要指纹。

| 路径 | 用途 |
| --- | --- |
| `POST /v1/platform/auth/register`、`/login`、`/logout` | 玩家账号与会话 |
| `GET /v1/platform/me` | 当前账号 |
| `GET /v1/platform/games`、`/games/{game}` | 已发布目录、查询和详情 |
| `GET /v1/platform/library`、`PUT /library/{game}` | 最近游玩与收藏 |
| `POST /v1/platform/games/{game}/launch`、`POST /launch/exchange` | PKCE 单次交接 |
| `POST /v1/games/{game}/session/logout` | 撤销当前游戏会话 |
| `GET/PUT /v1/platform/companion` | 伙伴身份与设置 |
| `GET/POST /v1/platform/companion/memories`、`DELETE /memories/{id}` | 显式记忆 |
| `GET/POST /v1/platform/companion/context` | 当前世界/切换或清空世界 |
| `GET/PUT /v1/games/{game}/companion/context` | 当前游戏可提供的场景和任务 |
| `POST /v1/platform/companion/chat`、`POST /v1/games/{game}/companion/chat` | 平台或游戏内文字对话 |
| `GET/PUT /gm/games/{game}/listing` | 发行资料草稿 |
| `POST /gm/games/{game}/listing/submit`、`/publish`、`/unpublish` | 提交、独立审核、下架 |

硬件 API 与协议见 [设备预留接口](device-interface-v1.md)。

## 验证

```sh
node tests/player-platform.mjs
node tests/companion.mjs
node tests/e2e.mjs
node tests/mail-inventory.mjs
```

Godot 新的游戏交接检查为 `godot --headless --path sdk/godot --script test_platform.gd`。本地验收结果见 [玩家平台验收](verification/PLAYER_PLATFORM_V1.md)。

可通过 `API_URL` 指向隔离 API，通过 `COMPOSE_PROJECT_NAME` 指向相同数据库项目。测试创建独立账号；平台测试会在隔离环境中暂时修改样例发行资料并尝试恢复，因此不要对生产环境运行。完整 e2e 的规则检查需要启用 `rules` profile。

生产部署仍需 TLS、正式管理账号/项目授权、备份、可信代理的精确配置和运行监控。当前 Compose 只绑定本机接口，未执行公网部署。
