extends Control
@export var game_id := "sky-island"
var sdk := MiniVerseClient.new()
var progress := 0
var revision := 0
var status := Label.new()
func _ready() -> void:
	add_child(sdk)
	sdk.game_id = game_id
	var column := VBoxContainer.new()
	column.position = Vector2(30, 30)
	column.custom_minimum_size = Vector2(650, 400)
	add_child(column)
	column.add_child(status)
	var play := Button.new()
	play.text = "升级建筑" if game_id == "sky-island" else "打出卡牌，完成挑战"
	column.add_child(play)
	play.pressed.connect(play_turn)
	var mail := Button.new()
	mail.text = "查看并领取邮件奖励"
	column.add_child(mail)
	mail.pressed.connect(claim_rewards)
	var sync := Button.new()
	sync.text = "重连并同步待上传存档"
	column.add_child(sync)
	sync.pressed.connect(func(): status.text = JSON.stringify(await sdk.sync_pending()))
	var keep_local := Button.new()
	keep_local.text = "冲突处理：保留本地版本"
	column.add_child(keep_local)
	keep_local.pressed.connect(func(): await resolve_conflict(true))
	var keep_cloud := Button.new()
	keep_cloud.text = "冲突处理：采用云端版本"
	column.add_child(keep_cloud)
	keep_cloud.pressed.connect(func(): await resolve_conflict(false))
	var result := await sdk.login()
	status.text = "已连接：" + game_id if result.ok else "离线游玩，进度保留在本地"
	if result.ok:
		await sdk.profile()
	var local := sdk.cache_load()
	progress = int(local.get(game_id + "_progress", 0))
func play_turn() -> void:
	progress += 1
	var local := sdk.cache_load()
	local[game_id + "_progress"] = progress
	sdk.cache_save(local)
	status.text = "本地进度：" + str(progress)
	if sdk.access_token.is_empty():
		return
	var event_type := "building_upgraded" if game_id == "sky-island" else "match_completed"
	await sdk.event(str(Time.get_unix_time_from_system()) + "-" + str(progress), event_type)
	var save := await sdk.cloud_save("main", revision, {"progress": progress})
	if save.ok:
		revision = int(save.data.revision)
	elif save.get("status", 0) == 409:
		status.text += " · 云存档冲突，本地与云端版本已保留"
	var board := "prosperity" if game_id == "sky-island" else "weekly"
	await sdk.game_request("/boards/" + board + "/sandbox/scores", HTTPClient.METHOD_POST, {"request_id": str(progress), "score": progress})
func claim_rewards() -> void:
	var result := await sdk.game_request("/mail")
	if not result.ok:
		status.text = "邮件暂时不可用"
		return
	for mail in result.data:
		await sdk.game_request("/mail/" + mail.id + "/claim", HTTPClient.METHOD_POST)
	var inventory := await sdk.game_request("/inventory")
	status.text = "权威库存：" + JSON.stringify(inventory.get("data", {}))

func resolve_conflict(keep_local: bool) -> void:
	var cache := sdk.cache_load()
	var key := game_id + "/main"
	if not cache.has(key) or not cache[key].get("conflict", false):
		status.text = "当前没有存档冲突"
		return
	var cloud: Dictionary = cache[key].cloud
	revision = int(cloud.revision)
	if keep_local:
		var data: Dictionary = JSON.parse_string(cache[key].pending.data)
		var result := await sdk.cloud_save("main", revision, data)
		if result.ok:
			revision = int(result.data.revision)
		status.text = "本地版本提交结果：" + JSON.stringify(result)
	else:
		var data: Dictionary = JSON.parse_string(cloud.data)
		progress = int(data.get("progress", 0))
		cache[key] = {"cloud": cloud, "conflict": false}
		cache[game_id + "_progress"] = progress
		sdk.cache_save(cache)
		status.text = "已采用云端进度：" + str(progress)
