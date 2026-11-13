# 纯客户端接入验证记录

日期：2026-11-06。开发基线：`40aef70`。本次改动提交于 `feat/client-only-onboarding` 分支，随后合并入 `main`；本记录不替代基线中的历史验收。

2026-11-07 补验：Docker 已启动，原 SDK/存档测试和新增邮件库存 SDK 测试已连接真实 API/MySQL 通过，Linux 容器内 Go race 也通过。详见 [后续验收](MAIL_INVENTORY.md)。下文保留本次初始检查时的环境和未执行项。

## 本次交付

- 新增 Godot `MiniVerseLocalSaveBridge`，只读原本地 JSON，导入前检查云端，使用零版本条件写入。已有存档和删除墓碑不自动覆盖，并发导入仍由服务端 revision 机制拒绝。
- 新增 `local_bridge_demo.tscn`：离线增加等级并保存，手动连接 sandbox、预览、首次导入和备份后采用云端版本。实际云端按钮流程本轮未连接 MySQL 验收。
- SDK 缓存按服务器、玩家、游戏隔离；网络返回后检查当前归属；旧响应不得覆盖更新的 pending。缓存文件写入失败返回错误。
- 玩家会话发行、验证、注销统一 `sessionHash`，保留 sandbox 兼容与生产隔离。未开放真实认证，也未增加数据库迁移。
- 新测试加入现有 CI 工作流；远端尚未推送或执行。

## 环境

- Windows，Go 1.27.3（模块工具链），Godot 4.7.2 stable。
- Docker CLI 可用，但 Docker Desktop Linux 引擎管道不可用。
- `CGO_ENABLED=0`，未发现 gcc/clang；本轮未执行 Go race 检查。
- 未读取或改动用户实际游戏项目；示例只在 SDK 工程中运行。

## 已执行结果

| 命令 / 场景 | 结果与边界 |
| --- | --- |
| `go test -count=1 ./...`（backend） | 通过；需要 `MINIVERSE_MEMORY_DSN` 的既有数据库集成测试未配置，按原规则跳过 |
| `go vet ./...`、`go build ./...`（backend） | 通过 |
| `go test ./internal/service -run TestSessionLogoutRevokesOnlyCurrentEnvironmentSession -count=1 -v` | sandbox 与 production 均通过：注销后 401，其他会话保留，过期/非法/其他环境令牌拒绝 |
| Godot headless editor import | 通过，无脚本解析错误 |
| `godot --headless --path sdk/godot --script test_local_bridge.gd` | 通过，使用协议夹具；原 SDK 云保存与文件缓存逻辑实际运行 |
| `godot --headless --path sdk/godot res://local_bridge_demo.tscn --quit-after 3` | 无界面启动冒烟检查通过；不表示人工 UI 或联网旅程已验收 |
| `git diff --check` | 通过 |

Godot 桥接回归覆盖：首次导入后清除 pending、重复导入拒绝、删除墓碑保护、检查后并发写入返回冲突、错误 404/401 不触发导入、离线保留原文件、已有云数据保留、账号/服务器/游戏缓存隔离、同账号换令牌仍读取原缓存、请求期间换账号不污染新账号，以及旧响应不覆盖更新的本地 pending。

会话测试通过 `database/sql` 驱动夹具执行真实的 actor/logout HTTP handler，不需要外部依赖。它证明环境哈希及撤销行为一致，不能代替真实 MySQL 或真实渠道认证验收。

## 未执行与下一步

Docker 引擎当前未运行，因此本轮未执行 Compose、MySQL、原有联网 Godot 测试和完整端到端回归。启动 Docker Desktop 后，应在隔离的 Compose 项目中执行 `tests/e2e.mjs`、迁移/生产边界测试，以及 `test_sdk.gd`、`test_saves.gd`，再人工体验新场景的联网操作。

CI 固定的 Godot 4.5.1 与本机 4.7.2 不同，仍需由远端 workflow 验证固定版本兼容性。真实 Steam/TapTap 登录、平台安全存储、生产会话发行、生产备份和长时运行仍属后续验收。

上线前需将具体客户端的存档 schema、账号恢复方式与数据权威边界映射到接入指南。不能将导入的本地金币直接兑换为服务端库存，也不能因本测试通过而认定已有反作弊或支付校验。
