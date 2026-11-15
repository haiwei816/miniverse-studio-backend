# DevOps：自动检查、交付与运行

当前落地 GitHub Actions 持续集成、可下载构建产物、本地一致的验收入口，以及 Compose 健康检查和有限日志。流水线不连接生产服务器、不自动发布游戏目录。代码推送后才会运行云端检查；仓库分支保护需要在 GitHub 设置中启用。

## 从修改到交付

`.github/workflows/ci.yml` 在 push、PR 和手动运行时执行，默认只读仓库权限，不需要部署密钥。

| 检查 | 实际工作 | 交付 |
| --- | --- | --- |
| backend | 工作流语法、Go race 测试、vet、Linux 构建 | API、迁移程序、SQL、提交号及 SHA256 清单 |
| frontend (admin/player) | 锁文件安装、依赖审计、玩家测试、两个网站构建 | 两份网站 dist |
| integration | 独立 Compose、HTTP/权限/迁移/恢复测试、Godot SDK | 成功或失败的容器状态与日志 |
| desktop | Windows MSVC Rust 工具链、锁文件构建 Tauri | Windows x64 NSIS 安装程序 |

产物名包含本次 Git SHA。普通产物保留 14 天；诊断日志与可选镜像包保留 7 天。PR 产物用于测试，全部检查通过的同一提交才作为交付候选。Windows 安装包没有配置代码签名。

建议保护 main，要求 `backend`、`frontend (admin)`、`frontend (player)`、`integration`、`desktop` 检查通过后才合并。这里只提供配置与建议，没有修改远端分支规则。

在 Actions 的 verify 页面手动运行并勾选 `package_images`，可在集成和 Godot 验收通过后导出本轮实际测试过的四个 Linux 镜像。它不重新构建、不推镜像仓库。包内有 `images.tar`、`SHA256SUMS`、`manifest.json` 和 `images.override.json`；源代码工作区不干净时拒绝打包，避免把未提交代码错误标成 Git SHA。

在隔离验收机上解压后：

```sh
sha256sum -c SHA256SUMS
docker load --input images.tar
# 在对应 Git SHA 的源码目录中运行；保持相同 COMPOSE_PROJECT_NAME。
docker compose -f deploy/compose.yaml -f /absolute/path/images.override.json --profile rules --profile gm --profile player up -d --no-build --wait --wait-timeout 180
```

该 Compose 使用 sandbox 账号、演示登录与开发数据库密码，镜像包用于隔离验收。正式环境的 TLS、数据库凭据、APP_ENV、管理账号和模型凭据要单独配置，不能把此启动命令直接当成公网生产部署。

## 本地复验

PowerShell，在仓库根目录选择独立项目及端口：

```powershell
$env:COMPOSE_PROJECT_NAME = 'miniverse-devops-check'
$env:API_PORT = '28080'
$env:GM_PORT = '28081'
$env:PLAYER_PORT = '28082'
$env:API_URL = 'http://127.0.0.1:28080'
$env:GM_URL = 'http://127.0.0.1:28081'
$env:PLAYER_URL = 'http://127.0.0.1:28082'
$env:MINIVERSE_API_URL = $env:API_URL
.\scripts\verify.ps1 -GodotPath 'C:\path\to\Godot_console.exe'
```

需要 Docker Desktop、Node.js、Godot。脚本会启动所选 sandbox，执行 Go race/vet/build、前端测试与构建、完整集成和五项 Godot 检查，完成后保留环境供检查。`-LoadTest` 另外执行压测，默认不跑。所有 URL 必须与 Compose 端口对应，避免测错实例。这些测试会写数据、重启 API、短暂停止数据库和规则服务，仅限隔离测试环境。

已有环境可直接运行 `node scripts/integration.mjs`，本地与 CI 使用同一清单。脚本会先核对三个地址都是本机来源，且端口属于所选 Compose 项目，全部匹配后才运行会修改数据的测试；每个子测试最多 5 分钟。迁移测试根据 SQL 文件列表核对实际应用记录；旧库存升级测试从 012 及以前的结构开始，然后升级到当前版本，不依赖写死的版本数。

## 运行与故障定位

Compose 等 MySQL 健康且独立迁移成功后才启动 API；API `/ready` 通过后才启动两个网站。API 关闭宽限为 15 秒，给内部 10 秒优雅退出留出余量。每个容器 JSON 日志最多 3 个文件、每个 10 MB；这是本机日志轮转，不是集中式监控。

```sh
docker compose -f deploy/compose.yaml --profile rules --profile gm --profile player ps -a
docker compose -f deploy/compose.yaml logs --tail 200 api
curl --fail http://127.0.0.1:18080/health
curl --fail http://127.0.0.1:18080/ready
```

`/health` 表示进程存活；`/ready` 还检查数据库连接与 schema。网站容器健康检查只确认静态页面能提供，业务可用性仍以 API readiness 和实际请求为准。Docker 不会仅因容器 unhealthy 自动重启它，应先查日志和数据库。

经 nginx 的客户端 IP 识别需要精确配置 `TRUSTED_PROXIES`。缺省为空时，经同一代理的玩家共用该代理认证额度，可能互相挤占。仅信任受控代理固定地址或专用最小网段，不能把整个默认内网设为可信。

## 发布与回退顺序

1. 选全部检查通过的提交，记录 Git SHA、镜像 ID、SQL 版本和前一版镜像，先在隔离环境验收。
2. 数据库变更前做备份，按 [备份与恢复](operations.md) 在独立库演练恢复，逐项审查新增 SQL 的前后兼容性。
3. 独立迁移成功后更新 API 与网站，检查 `/ready` 和实际登录、读取流程；配置生产监控告警后再开放流量。
4. 应用异常时，仅在旧程序兼容当前 schema 的前提下切回前一版镜像。禁止删除数据卷或修改已应用 SQL 回退；不兼容迁移需要专门的数据恢复或向前修复方案。

尚未配置自动生产部署、远端环境、集中监控与告警接收人。确定目标服务器与部署环境后，再接环境审批、密钥及发布执行。

实现依据：[GitHub 工作流语法](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)、[Compose 就绪依赖](https://docs.docker.com/compose/how-tos/startup-order/)、[Docker 镜像交付](https://docs.docker.com/build/ci/github-actions/share-image-jobs/)。
