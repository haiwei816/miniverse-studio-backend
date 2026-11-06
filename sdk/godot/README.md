# Godot 4 SDK

用 Godot 4.5.1 打开本目录 project.godot，运行 Demo。后端默认 http://127.0.0.1:18080。可设置 client.base_url 与 client.game_id。登录成功后 token 仅保存在内存；expires_at 提供给调用者，401 清理 token 并由游戏提示重新登录。游客恢复凭据由游戏交给平台安全存储，不能与普通离线进度缓存混存。

每次请求创建独立 HTTPRequest，超时五秒；只对 GET 网络失败做最多三次尝试，写操作由稳定业务 ID 明确重试。cache_save 先写临时文件再替换，断网不清除本地进度。网络变化通过 network_changed 通知。

自动验收：先启动 Compose，再运行：

```sh
godot --headless --path sdk/godot --editor --import --quit
godot --headless --path sdk/godot --script test_sdk.gd
```

缓存是游戏的离线副本，云同步冲突应由 UI 展示并由用户选择，不能静默覆盖。正式移动端与 Steam 的凭据持久化需结合平台安全存储实现；本示例不将令牌写入本地 JSON。
