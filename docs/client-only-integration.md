# 现有纯客户端游戏接入

适用情况：玩法与进度都在客户端，目前没有游戏专属服务端。先保留原来的离线循环，用现有 Go 后端增加身份、备份、活动、邮件和运营；逐步迁移真正需要权威校验的数据。

## 1. 职责与接入顺序

| 内容 | 第一阶段的归属 | 接入方式 |
| --- | --- | --- |
| 战斗、关卡、表现、本地进度 | 游戏客户端 | 不依赖后端即可继续玩 |
| 本地 JSON 存档 | 游戏客户端 | 读入后显式导入云端；原文件不删除 |
| 云存档、版本和历史 | Go/MySQL | revision 比较，冲突让玩家选择 |
| 账号与恢复凭据 | 后端发行身份，客户端负责安全存储 | 本地测试使用 sandbox，发行时接真实渠道 |
| 活动事件与休闲成绩 | 客户端上报，服务端标注信任范围 | 只能用于明确允许客户端来源的规则 |
| 邮件、奖励流水、线上库存 | 服务端 | 只通过服务端幂等领取改变，不从存档余额直接充值 |
| 贵重奖励、竞技成绩、付费权益 | 待接入可信来源 | 不能以客户端声明或合法回放作为交易/对局真实性证明 |

第一步接账号与云备份，第二步接活动/邮件/公告，第三步按实际游戏决定哪些经济和规则需要迁移。当前仓库没有用户实际游戏源码，本轮提供可运行适配层和示例，不宣称已接通用户的具体游戏。

## 2. 先体验离线接入示例

在 Godot 中打开 `sdk/godot/project.godot`，运行 `local_bridge_demo.tscn`，或执行：

```sh
godot --path sdk/godot res://local_bridge_demo.tscn
```

“本地游玩”将等级保存到 `user://client_only_demo.json`，不需要服务端。若要体验云端部分，按 README 启动本地 Compose，再点击连接后端。首次留空创建测试账号；后续用同一恢复凭据登录才能看到同一份云存档。“复制恢复凭据”仅供本地测试保管，SDK 不写入普通缓存，正式游戏应接平台安全存储。

点击检查会预览本地与云端。云端不存在时可以首次上传；存在时上传被拒绝，可选择采用已预览的云端版本。采用前会备份原文件，若本地进度或账号发生变化则要求重新检查。云端删除记录不会自动复活或删除本地存档。

## 3. 适配已有存档

Godot 接入代码：

```gdscript
var sdk := MiniVerseClient.new()
add_child(sdk)
sdk.game_id = "your-game-id" # 需先注册游戏及授权，见 new-game.md

# 本地开发专用；真实渠道拿到会话后使用 sdk.use_session(player, token, expiry)。
var login := await sdk.login(recovery_secret)
if not login.ok:
    return # 原来的本地玩法继续运行

var bridge := MiniVerseLocalSaveBridge.new(sdk)
var preview := await bridge.inspect("main", "user://existing_save.json")
# 将 preview 交给游戏 UI，只有玩家选择首次上传后才调用：
var result := await bridge.import_local("main", "user://existing_save.json", 1)
```

`inspect` 只读取本地 JSON 对象并查询云端；`import_local` 再次查询并使用 `revision=0` 写入，防止检查后被另一设备抢先上传。只有明确的 `save_not_found` 才会尝试首次写入；游戏不存在、鉴权失败、网络失败均不能当成空云存档。

| 结果 | 游戏应如何处理 |
| --- | --- |
| `action=can_import` | 可以展示首次上传按钮 |
| `action=choose_version` / `error=cloud_exists` | 展示双方进度；保留云端或让玩家明确选择覆盖 |
| `status=409` | 发生并发冲突；SDK 保留 pending 和远端快照，再次让玩家选择 |
| `error=cloud_unavailable` | 继续本地游玩，不自动创建或覆盖云端 |
| `error=login_required` | 先恢复账号，本地文件仍保留 |
| `error=session_changed` | 当前操作期间切换了账号、服务器或游戏，请重新检查 |
| `error=cache_write_failed` | 检查存储空间和权限；若含 remote 字段，先核对服务端结果再重试 |

玩家明确选择覆盖已有云端时，使用当前云端 revision 调用 `cloud_save`；不能强行使用零版本或自行增加 revision。采用云端数据时，由游戏检查 schema、执行转换并备份本地文件，桥接层不擅自改写原文件。

桥接层目前支持 `user://` 下、大小不超过 1 MiB 的 JSON 对象。已有存档是二进制、加密文件、数组或多个文件时，由游戏解析并转换为 JSON 对象后接入 `cloud_save`，保留原始备份。客户端旧金币/道具仅作为存档内容，不能据此给线上权威库存入账。

## 4. 缓存升级与账号恢复

SDK 缓存按服务器地址、玩家 ID 和游戏 ID 分文件隔离，换账号不会同步前一个账号的 pending；同一账号重登可继续使用自己的缓存。令牌更新不改变缓存归属。401 清理令牌，保留已知玩家的本地缓存；注销回到未登录作用域。正式适配器应调用 `use_session`，不要只赋值 `access_token` 后就调用云保存。

旧版无账号归属的 `miniverse_cache.json` 不会自动认领给新账号，也不会删除。确认归属后按本节的显式导入流程迁移。设置 `cache_path` 可指定缓存前缀；原来的游戏存档应使用独立路径，不能覆盖 SDK 缓存。

离线首次启动也不需要创建服务器账号。玩家准备启用云服务时才登录，并选择要迁移的本地进度；卸载前应提示保管恢复方式，不能把设备上的缓存当成可恢复身份。

## 5. 验证与限制

无需启动数据库即可验证桥接逻辑：

```sh
godot --headless --path sdk/godot --editor --import --quit
godot --headless --path sdk/godot --script test_local_bridge.gd
```

测试使用协议夹具，覆盖首次导入、已有存档、删除墓碑、并发冲突、失败读取、离线原文件保留、缓存隔离和请求期间换号。真实 HTTP/MySQL 与原有 SDK 测试仍需启动 Compose 后另行执行。本次执行范围见 [客户端接入验证记录](verification/CLIENT_ONLY_INTEGRATION.md)。
