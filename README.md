# MiniVerse Studio Backend

Go 模块化单体 + MySQL 的多游戏工作室后端 MVP。基于原仓库 S0.1 增量实现，按 S0→S12 顺序执行并记录验收。包含 Godot 4 SDK、两游戏接入示例、Vue3 + Element Plus GM、审批审计、sandbox 渠道身份、mock 活动策划和可选 C++20/gRPC 规则服务。

**本地 MVP，不代表已上线。Steam/TapTap 真实认证与真实 AI 模型均未验证。** 详细结果以 [验收记录](docs/verification/RESULTS.md) 为准，不能把 sandbox 通过当作平台正式验收。

## 启动

前置：Docker Desktop / Docker Engine + Compose v2。默认仅发布到本机，不需要在主机安装 Go、MySQL 或 C++。

```sh
# 完整演示：后端、MySQL、GM 网页及可选规则服务
docker compose -f deploy/compose.yaml --profile gm --profile rules up -d --build

# 普通业务只需要 Go + MySQL
docker compose -f deploy/compose.yaml up -d --build

# 检查
curl http://127.0.0.1:18080/health
curl http://127.0.0.1:18080/ready

# 停止并保留数据库
docker compose -f deploy/compose.yaml --profile gm --profile rules down
```

GM 网页：[http://127.0.0.1:18081](http://127.0.0.1:18081)。API：`http://127.0.0.1:18080`。`API_PORT`、`GM_PORT` 可以覆盖主机端口。MySQL 与 gRPC 不发布主机端口。首次构建需要下载依赖。

sandbox 管理员账号：

| 账号 | 密码 | 权限 |
|---|---|---|
| admin | sandbox-admin | 授权游戏管理、申请、审核，不能自审 |
| operator | sandbox-operator | 查看、创建申请、停止活动、公告 |
| reviewer | sandbox-reviewer | 查看、独立审批 |
| support | sandbox-support | 只读天空岛，不能访问卡牌游戏 |

这些是本地测试夹具，APP_ENV 非 sandbox 时禁止使用。生产管理员需要在独立数据库中配置 PBKDF2 哈希与显式游戏授权；本交付未执行生产部署。

## 一次完整体验

已有纯客户端游戏可先运行 `sdk/godot/local_bridge_demo.tscn`：离线游玩并保留原存档，需要时再登录、预览及首次导入云端。已有云存档会要求选择版本。详见 [纯客户端接入指南](docs/client-only-integration.md)。

1. 用 Godot **4.5.1** 打开 `sdk/godot/project.godot`；运行 `sky_island.tscn` 或 `card_game.tscn`。
2. 点击“升级建筑”或“完成挑战”，使用统一 SDK 保存进度、上报事件和成绩；达到目标后收到奖励邮件。
3. 点击领取，显示服务端权威库存；重复领取不会重复入账。断网进度保存在本地，重连可同步，冲突时可选择本地或云端版本。
4. 登录 GM，在“玩家与资产”按 UID/角色名查询。到“运营与审批”填写原因、玩家和道具，创建补发申请。
5. 退出后用 reviewer 登录，批准该申请。玩家领取邮件后库存增加；“操作审计”能看到申请与审批链。
6. 活动策划面板生成 mock 配置并校验/模拟，提交为活动申请，再由另一人审核发布。历史配置回滚会创建新的待审核版本。

Demo 是接入示例，不是完整游戏。账号恢复凭据由调用游戏接入平台安全存储；SDK 不把令牌混存到普通 JSON 缓存。

## 验证

安装 Node.js 24 后，在仓库根目录：

```sh
node tests/e2e.mjs
node tests/recovery.mjs
node tests/load.mjs
```

运行全套测试前需要启用 rules profile。测试会创建独立随机玩家/申请，保留测试数据。恢复测试会暂时重启本工程 API、停止/恢复 MySQL 与 RuleCore；不要对生产实例运行。压测是 100 次、10 并发的本地已认证档案读取，结果在 `docs/verification/load-results.json`，不代表生产容量。

Go 检查（主机有 Go 1.27.3 时）：

```sh
cd backend
go test -race ./...
go vet ./...
go build ./...
```

Godot 检查：

```sh
godot --headless --path sdk/godot --editor --import --quit
godot --headless --path sdk/godot --script test_sdk.gd
godot --headless --path sdk/godot --script test_saves.gd
```

C++ CMake/CTest 在 RuleCore 镜像构建时运行；可再次执行：

```sh
docker run --rm --entrypoint /build/rule_tests miniverse-rulecore:local
```

GM 开发：`cd admin/web && npm ci && npm run dev`，访问 localhost:5173；生产资源检查用 `npm run build`。CI 已配置 Go、前端审计/构建、Compose、HTTP、恢复和 Godot 测试；**远程 GitHub Actions 尚未运行**。

## 目录与关键约束

`backend/cmd` 负责 API/独立迁移；`internal/service` 按身份、存档、活动、奖励、排行、GM、渠道与策划划分文件；`internal/platformauth` 封装凭证验证；`internal/rulepb` 是生成的协议。普通模块同进程共享事务，C++ 仅按需调用。

所有玩家资源都由会话中的 player_id 与路径 game_id 共同定位。存档使用 revision 与删除墓碑；事件、发奖、领取、审批和回放通过事务与唯一键防重复。活动不会直接修改存档 JSON。没有跨游戏道具和经济互通。

迁移进程持有 MySQL advisory lock，逐文件记录版本和 SHA256；已经应用的文件禁止修改。MySQL DDL 隐式提交，迁移采用可安全重跑的 SQL，失败后先修复故障再重试；禁止把删除数据卷当作迁移修复。API /ready 同时检查数据库与当前 schema 版本。服务接收退出信号后最多等待十秒。

## 文档

- [架构与数据流程](docs/architecture.md)
- [接口契约](docs/api.md)
- [邮件与背包库存、消耗和流水](docs/mail-inventory.md)
- [本地开发](docs/local-development.md)
- [第二游戏接入](docs/new-game.md)
- [纯客户端接入指南与存档迁移](docs/client-only-integration.md)
- [渠道适配状态](docs/platform-adapters.md)
- [C++ 规则与限制](docs/rulecore.md)
- [备份、恢复与迁移](docs/operations.md)
- [真实验收与限制](docs/verification/RESULTS.md)
- [MVP 后续补充与验收清单](docs/mvp-follow-up.md)
- [内存检查与参考后台功能评估](docs/verification/MEMORY_ARCHITECTURE_REVIEW.md)
- [交给其他 AI Coding Agent 的总控 Prompt](docs/MASTER_IMPLEMENTATION_PROMPT.md)

首版未包含多人 3D 大厅、实时 PvP、通用分发、支付或跨游戏经济。正式发布前仍需要真实渠道/AI 适配、服务器签发对局上下文、生产凭据和权限管理、备份恢复演练、TLS 与部署验证。
