# Local commands

Start: `docker compose -f deploy/compose.yaml up -d --build`

Stop without deleting data: `docker compose -f deploy/compose.yaml down`

Go checks without host Go installation:

```powershell
docker run --rm -v "${PWD}/backend:/src" -v miniverse_go_cache:/go -v miniverse_build_cache:/root/.cache/go-build -w /src golang:1.27.3 sh -c 'go test -race ./... && go vet ./...'
```

Compose uses a dedicated `miniverse-mvp` project and database volume. Development-only credentials are visible in Compose for reproducibility; do not expose this sandbox publicly. API listens on localhost:18080. No database port is published.

Windows managed environments may require an approved process to access Docker and localhost. Permission errors are environment failures and must not be recorded as successful tests.
# 反向代理与来源地址

完整的本地复验、GitHub Actions、构建产物与隔离项目配置见 [DevOps](devops.md)。新增 `scripts/integration.mjs` 会先校验本机 API/GM/玩家地址与所选 Compose 项目的发布端口相符，再执行测试。

默认不信任任何代理，API 会忽略 `X-Forwarded-For` 和 `X-Real-IP`，按连接的对端地址限流。仅在确实使用受控反向代理时，将 `TRUSTED_PROXIES` 设置为代理的精确 IP 或最小必要 CIDR，多个地址以逗号分隔，例如 `TRUSTED_PROXIES=10.0.0.5/32`。不要直接信任所有内网、回环地址或整个公网；确保其他客户端无法冒充所配置的代理地址。配置错误会使服务启动失败。

面向客户端的第一层代理必须丢弃客户端提供的转发头并覆盖为实际连接来源。仓库内 GM nginx 已覆盖 `X-Forwarded-For` 和 `X-Real-IP`；如它前面还有代理，需独立配置并确认真实来源。服务按可信代理链从右向左识别第一个不可信地址；未配置 GM 代理地址时，经 GM 转发的请求将共用该代理 IP 的限流配额。容器的 IP 可能变化，生产环境应使用固定地址或专门隔离的最小网段，而不是默认信任整个 Docker 网络。
