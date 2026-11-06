# 接入新游戏

1. 用新迁移注册 games、items、活动配置与榜单，以及显式的 gm_access 授权；配置样例见 game-configs。
2. 同一个 Godot SDK 设置 game_id。游戏只实现本地玩法与自己的存档 schema；使用统一 event、cloud_save、game_request API。
3. 活动采用该游戏的事件类型与道具 ID，客户端事件只用于明确允许的休闲活动。贵重奖励与可信排行必须验证来源。
4. 按 tests/s9.mjs 执行两个游戏的完整旅程，确认云存档、道具、活动和权限隔离。

运行 Godot 的 sky_island.tscn 或 card_game.tscn：分别用建筑升级与卡牌挑战按钮推进本地玩法并上传存档、事件和成绩，领取邮件后展示服务端库存。这是接入演示，不是完整商业游戏；不复制任何后端业务模块。
