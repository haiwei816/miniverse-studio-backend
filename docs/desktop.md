# 桌面客户端（Tauri）

GM 控制台有两种运行形态，**共用同一套 Vue 代码**：

| 形态 | 入口 | 适合 |
| --- | --- | --- |
| 网页 | `admin/web`，由 nginx 提供 `/gm` 代理 | 随手打开、给别人看、免安装 |
| 桌面 | `admin/web/src-tauri`（Tauri 2） | 常驻、全局热键、系统通知、离线打开界面 |

## 现在的桌面能力

- **托盘**：左键显示/隐藏窗口；菜单含「打开控制台 / 补发奖励… / 刷新数据 / 退出」。
- **全局热键**：`Ctrl+Shift+G`（macOS 为 `⌘⇧G`）显示或收起窗口。
- **系统通知**：登录后每 60 秒轮询一次待审申请数，数量上升时发一条原生通知；窗口隐藏时也会提醒。
- **请求走 Rust**：前端用 `tauri-plugin-http` 发请求，因此没有 CORS 预检，API 也不需要对外开放。
- **主题**：跟随系统，也可在侧栏切换浅色/深色。

## 构建前提

`npx tauri info` 会逐项检查。Windows 需要：

1. **Rust**：`winget install Rustlang.Rustup`（装完重开终端，`rustc --version` 能出版本）。
2. **MSVC 生成工具**：Visual Studio Build Tools 的「使用 C++ 的桌面开发」工作负载（约 3–6 GB）。
   `winget install Microsoft.VisualStudio.2022.BuildTools --override "--quiet --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"`
3. WebView2 运行时（Win10/11 一般已自带）。

macOS 只需要 Xcode Command Line Tools：`xcode-select --install`。

## 命令

```bash
cd admin/web
npm install
npm run desktop        # tauri dev：起 Vite 5173 + 原生窗口
npm run desktop:build  # 产出安装包：src-tauri/target/release/bundle/
```

产物位置：

- Windows：`src-tauri/target/release/bundle/nsis/*.exe`
- macOS：`src-tauri/target/release/bundle/dmg/*.dmg`

## 接口地址

桌面端默认请求 `http://127.0.0.1:18080`（`compose` 里 API 的端口），可在**系统状态 → 接入信息**里改成别的地址，保存在 `localStorage.miniverse.api`。

对应地在 `src-tauri/capabilities/default.json` 里放行了这些地址，新增域名要同步加白名单：

```json
{ "url": "http://127.0.0.1:18080/*" }
```

## 安全边界

- GM token 只存在内存里，关掉窗口即失效（后端会话本身 30 分钟过期），不写入磁盘。
- 桌面端直连 API，绕过 nginx 的限流与审计前缀；因此**只在本机使用**，不要把 18080 暴露到公网。
- 托盘/热键/通知都在 Rust 侧，前端只监听 `gm://action` 事件；网页模式下这些桥接全部不生效（`window.__TAURI__` 不存在）。
- 通知轮询只读总览接口，不触发任何写操作。

## 目录

```
admin/web/src-tauri/
├── Cargo.toml
├── tauri.conf.json          # 窗口、bundle、withGlobalTauri
├── capabilities/default.json# http/notification 白名单
├── icons/                   # 由 npx tauri icon 生成
└── src/
    ├── main.rs              # 入口
    └── lib.rs               # 托盘、热键、事件
```
