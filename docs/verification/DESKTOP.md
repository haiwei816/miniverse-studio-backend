# 桌面客户端验收

日期：2026-11-14。基线：`0bcd31c`（看板重写 + Tauri 外壳）。

## 环境

| 组件 | 版本 |
| --- | --- |
| rustc / cargo | 1.100.0（stable-x86_64-pc-windows-msvc） |
| MSVC 生成工具 | Visual Studio Build Tools 2022，MSVC 14.44.35207 |
| Windows SDK | 10.0.26100.0 |
| WebView2 Runtime | 157.0.4341.46 |
| Node / npm | 24.18.1 / 11.16.0 |

工具链是本轮新装的：`winget install Rustlang.Rustup` 与 VS Build Tools 的 `VCTools` 工作负载（+Recommended）。`npx tauri info` 现已全部通过。

## 已执行

| 检查 | 结果 |
| --- | --- |
| `npm ci` | 通过，安装后无 element-plus |
| `npm run build`（Web，base `/`） | 通过；JS 247.61 kB（gzip 89.06 kB）、CSS 23.69 kB（gzip 5.85 kB）、字体 106 kB |
| `npm run build:desktop`（base `./`） | 通过，产物引用 `./assets/...`，适配 Tauri 协议 |
| `cargo check` | 通过，1m05s |
| `npx tauri build` | 通过，release 编译 2m15s，产出 2 个安装包 |
| `go build / go vet / go test ./...` | 通过 |

产物：

```
src-tauri/target/release/miniverse-desktop.exe
src-tauri/target/release/bundle/msi/MiniVerse GM_0.1.0_x64_en-US.msi     5.47 MiB
src-tauri/target/release/bundle/nsis/MiniVerse GM_0.1.0_x64-setup.exe    3.79 MiB
```

## 运行时验证

Windows 会话当时处于锁屏状态，无法截取窗口，改用 UI Automation 读取真实控件树：

| 检查 | 结果 |
| --- | --- |
| 进程启动 | 存活，窗口标题 `MiniVerse GM`，类名 `Tauri Window`，WebView2 子进程已拉起 |
| 登录页渲染 | UIA 读到 2 个编辑框（`管理员账号` / `密码`）与 1 个按钮（`登录`） |
| 登录链路 | 通过 UIA 填入 sandbox 账号并触发登录，请求经 **Rust http 插件**发送到 `127.0.0.1:18080`（capability 白名单生效，无 CORS 预检） |
| 登录后界面 | UIA 读到游戏切换器（`天空岛物语 sky-island`）、导航（概览/玩家/运营/审计/系统）、`跟随系统`、`退出`、`搜索 ⌘K`、`刷新`、`查看玩家 →`、`运营台 →` |

登录用的是一个只读 mock API（fixture 数据，覆盖登录、游戏列表、概览、玩家、审计、申请），不是真实 MySQL。

## 未验证

- **托盘菜单与全局热键**：锁屏下无法目视或按键验证。间接证据是托盘构建与 `global_shortcut().register()` 都在 `setup()` 里，任何一步失败都会让进程退出，实际进程保持存活。
- **系统通知**：需要 60 秒轮询并在待审数上升时触发，且要有可见桌面才能看到 toast。
- **真实 API / MySQL**：Docker Desktop 未运行，未做完整 Compose 联调。
- **macOS 构建与签名**、Windows 代码签名 / SmartScreen、自动更新。
- Web 形态在 nginx 下的实际部署（只验证了构建产物的相对路径与本地 Vite）。

## 复现命令

```powershell
cd admin/web
npm ci
npm run build          # Web
npm run build:desktop  # 桌面资源
npx tauri build        # 安装包
```

开发模式：`npm run desktop`（自动起 Vite 5173）。
