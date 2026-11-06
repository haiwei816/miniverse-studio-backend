# MiniVerse Studio Backend

当前实现：S0.1，最小 Go HTTP 服务、配置加载与校验、`GET /health`。

## 环境

- Go 1.27.3；`go.mod` 声明最低版本。使用较旧 Go 且 `GOTOOLCHAIN=auto` 时，首次构建可能联网下载工具链。
- 当前无第三方 Go 依赖，因此没有 `go.sum`。
- S0.1 在本机直接运行；容器化在 S0.5 实现。

## 构建与启动

在仓库根目录执行：

```powershell
cd backend
go build ./...
go run ./cmd/api
```

Linux 也使用相同的 Go 命令。在另一个终端访问：

```sh
curl -i http://127.0.0.1:8080/health
```

预期：HTTP 200，JSON 正文 `{"status":"ok"}`。在运行终端按 Ctrl+C 结束进程；此阶段没有优雅退出等待机制，S0.5 实现。

## 配置

| 环境变量 | 默认值 | 校验 |
| --- | --- | --- |
| HTTP_HOST | 127.0.0.1 | 必须为 IPv4 或 IPv6 地址；不接受空串及主机名 |
| HTTP_PORT | 8080 | 整数，范围 1–65535 |

显式传空值会校验失败。默认仅监听本机；需要容器访问时后续显式配置 `0.0.0.0`。`.env.example` 是示例，程序不自动读取该文件。

PowerShell 自定义端口：

```powershell
$env:HTTP_PORT = '9090'
go run ./cmd/api
```

Linux：

```sh
HTTP_PORT=9090 go run ./cmd/api
```

非法配置或端口占用会向标准错误输出原因并以非零状态退出。

## 检查

在 `backend` 目录执行：

```sh
go test ./...
go vet ./...
```

配置边界与 HTTP 路由通过单元测试验证；实际启动与失败场景见 [S0.1 验收记录](docs/s0-1验收.md)。

## 包职责与请求流程

`cmd/api` 调用配置模块，再调用应用组装；`internal/app` 创建监听器、路由和 HTTP Server；`internal/httpserver` 将请求分发给 Handler。`internal/config` 独立读取环境变量并提供类型化配置，HTTP 处理代码不读取环境变量。

`go.mod` 定义模块路径 `miniverse/backend`，这是本地导入路径，不要求事先创建同名远程仓库。`package main` 的 `main()` 是进程入口；`internal` 限制这些实现被模块树以外的代码导入。

服务启动时先完成配置验证，再绑定端口。`Server.Serve` 持续接受连接并由标准库并发处理 HTTP 请求；`ServeMux` 根据方法和路径调用 `/health` Handler，后者返回 JSON。绑定端口失败会沿 `app.Run → run → main` 返回错误；仅 `main` 决定进程退出码。

后续顺序：S0.2 日志、中间件与超时 → S0.3 数据库和就绪检查 → S0.4 独立迁移 → S0.5 容器与优雅退出 → S0.6 整体工程交付。
