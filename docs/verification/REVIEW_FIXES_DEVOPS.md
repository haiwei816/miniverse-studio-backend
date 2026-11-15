# 未提交代码修复与 DevOps 本地验收

日期：2026-11-15。验收对象为本次工作区文件，未提交、未推送，不以当前 HEAD 代表完整源码。未执行公网部署或真实模型服务调用。

## 修复结果

- 游戏聊天默认不查询或向模型发送全局记忆；平台聊天保留已保存记忆。真实 handler 配合本地回显模型验证两条入口的数据边界。
- 发行 GET 一次读取资料与状态，提交/发布携带 `draft_hash`；旧页面不能提交或发布被替换的草稿，独立审核限制保留，网页未保存修改不能发布。
- 退出游戏携带 `expected_epoch`，迟到请求不能清空新世界；游戏库刷新失败保留已启动会话。
- 新旧平台登出共用事务清理；排队启动或世界切换会再次锁定并验证父会话，不能在登出后恢复旧世界。Godot SDK 使用平台退出。
- 聊天限流改为定期清扫，已有伙伴配置直接读取；发行入口补 256 字节边界。
- 原有迁移不变，新平台依旧使用 014–017。升级测试从 012 及以前结构开始，核对完整迁移列表而非写死 13。测试报告改写 `work/ci`；补齐 Godot 稳定 UID 元数据。

## 已执行并通过

| 检查 | 结果 |
| --- | --- |
| 本机 `go test -count=1 ./...`、`go vet ./...`、`go build ./...` | 通过 |
| Linux Go 1.27.3 容器 `go test -race -count=1 ./...`、vet、build | 通过 |
| 玩家 `npm test` | 13 项通过，含 7 项会话生命周期回归 |
| 镜像交付 `node --test scripts/package-images.test.mjs` | 4 项通过，隔离 Git/Docker 边界，真实临时文件及 checksum |
| 两个前端生产构建、依赖审计 | 通过，均 0 漏洞 |
| GitHub Actions actionlint、JS/PowerShell 语法、Compose config | 通过 |
| Compose API/两个网站健康检查、SQL 迁移 | 通过，17 个迁移 |
| `node scripts/integration.mjs` | 全部通过，最终脚本含项目端口核验 |
| 错误 API 端口检查 | 在执行集成测试前拒绝，符合预期 |
| Godot 5 个 SDK 脚本 | 全部 PASS |
| Tauri `npm run desktop:build -- --ci --bundles nsis -- --locked` | 通过，生成 Windows x64 安装包 |

HTTP 验收含 S0–S12、邮件/库存分页与原子性、旧库存升级、玩家平台 PKCE 与跨游戏限制、草稿版本绑定、伙伴上下文与登出、迁移重复/篡改拒绝、production 边界和依赖故障恢复。最终集成入口重新运行通过，测试前确认三个 URL 属于当前 Compose 工程。

隔离 Compose 工程为 `miniverse-devops-review`，端口 API 28080、GM 28081、玩家 28082，数据库使用该工程独立卷。验收后停止并移除本工程容器，保留数据库卷；未改动其他工程。

Windows 安装包：`admin/web/src-tauri/target/release/bundle/nsis/MiniVerse GM_0.1.0_x64-setup.exe`，约 3.79 MiB，包含本轮发行页修复，未签名。只验证本机编译与打包，本轮没有重新执行桌面 UI 人工验收。

## 限制与后续运行

- 云端 GitHub Actions 尚未运行；推送源码后才能得到远端检查结果，分支保护未配置。
- 镜像导出编排通过隔离回归测试，Compose 的实际裸镜像 ID 已核对；没有从当前未提交工作区生成真实交付镜像包。完整镜像归档由干净 checkout 的手动 CI 在集成与 Godot 测试通过后执行。
- 本机 Godot 4.7.1 的 headless 编辑器导入退出时输出 `Parameter "singleton" is null` 及线程清理警告，退出码为 0；随后五项独立测试全部 PASS。CI 保留原有 Godot 4.5.1，尚未在远端复验。
- 没有接生产服务器、TLS、生产凭据、集中式监控或告警。运行及回退顺序见 [DevOps](../devops.md)。
- 原始日志保存在忽略的 `work/devops-*.log`，不会随源码提交。
