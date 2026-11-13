extends Control

# A tiny existing offline game: this file belongs to the game, not to the SDK.
const LOCAL_PATH := "user://client_only_demo.json"
var client := MiniVerseClient.new()
var bridge: MiniVerseLocalSaveBridge
var status := Label.new()
var preview := Label.new()
var secret_input := LineEdit.new()
var progress := 0
var recovery_secret := ""
var busy := false
var cloud_preview := {}
var preview_scope := ""
var preview_local_hash := ""

func _ready() -> void:
	add_child(client)
	bridge = MiniVerseLocalSaveBridge.new(client)
	var column := VBoxContainer.new()
	column.set_anchors_and_offsets_preset(Control.PRESET_FULL_RECT)
	column.offset_left = 20
	column.offset_top = 20
	column.offset_right = -20
	column.offset_bottom = -20
	add_child(column)
	var title := Label.new()
	title.text = "纯客户端游戏 → 可选云存档"
	column.add_child(title)
	status.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	column.add_child(status)
	button(column, "本地游玩：增加一级并保存", play_local)
	secret_input.placeholder_text = "测试账号恢复凭据（首次连接可留空）"
	secret_input.secret = true
	column.add_child(secret_input)
	button(column, "连接测试后端 / 恢复测试账号", connect_backend)
	button(column, "复制本次测试账号恢复凭据", copy_recovery)
	button(column, "检查本地与云端存档", inspect_saves)
	button(column, "首次上传本地存档（云端存在则拒绝）", import_save)
	button(column, "采用已预览的云端版本（备份本地文件）", adopt_cloud)
	preview.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	column.add_child(preview)
	var local := bridge.read_local(LOCAL_PATH)
	if local.ok:
		progress = int(local.local_data.get("level", 0))
	status.text = "可离线游玩。当前等级：" + str(progress)

func button(parent: Node, text: String, action: Callable) -> void:
	var control := Button.new()
	control.text = text
	control.pressed.connect(action)
	parent.add_child(control)

func write_local(data: Dictionary) -> bool:
	var file := FileAccess.open(LOCAL_PATH + ".tmp", FileAccess.WRITE)
	if file == null:
		return false
	file.store_string(JSON.stringify(data))
	file.close()
	return DirAccess.rename_absolute(LOCAL_PATH + ".tmp", LOCAL_PATH) == OK

func play_local() -> void:
	if write_local({"level": progress + 1}):
		progress += 1
		status.text = "本地等级：" + str(progress) + "；无需联网"
	else:
		status.text = "本地保存失败，请检查可用空间与目录权限"

func connect_backend() -> void:
	if busy:
		return
	busy = true
	var result := await client.login(secret_input.text)
	busy = false
	if result.ok:
		recovery_secret = result.data.secret
		secret_input.text = recovery_secret
		status.text = "已连接测试账号。恢复凭据仅在内存，请复制保管；正式发行需安全存储。"
	else:
		status.text = "后端连接失败，本地玩法仍可使用：" + JSON.stringify(result)

func copy_recovery() -> void:
	if not recovery_secret.is_empty():
		DisplayServer.clipboard_set(recovery_secret)
		status.text = "测试恢复凭据已复制；请勿放进普通存档或日志"

func inspect_saves() -> void:
	if busy:
		return
	busy = true
	cloud_preview = {}
	var result := await bridge.inspect("main", LOCAL_PATH)
	busy = false
	if result.ok and result.action == "choose_version":
		cloud_preview = result.cloud
		preview_scope = client.session_scope()
		preview_local_hash = FileAccess.get_file_as_string(LOCAL_PATH).sha256_text()
	preview.text = JSON.stringify(result, "  ")

func import_save() -> void:
	if busy:
		return
	busy = true
	var result := await bridge.import_local("main", LOCAL_PATH)
	busy = false
	preview.text = JSON.stringify(result, "  ")
	status.text = "上传成功；原本地文件保留" if result.ok else "未覆盖已有云存档，请检查结果并选择版本"

func adopt_cloud() -> void:
	if busy or cloud_preview.is_empty() or preview_scope != client.session_scope():
		status.text = "请先在当前账号下检查存档"
		return
	if FileAccess.get_file_as_string(LOCAL_PATH).sha256_text() != preview_local_hash:
		status.text = "本地进度已变化，请重新检查后再选择"
		return
	if cloud_preview.get("deleted", false):
		status.text = "云端是删除记录，本示例不会删除本地进度"
		return
	var data = JSON.parse_string(cloud_preview.get("data", ""))
	if not data is Dictionary or not data.has("level") or int(cloud_preview.get("schema_version", 0)) != 1:
		status.text = "云端格式与本示例不兼容，请由游戏执行格式升级"
		return
	var backup := LOCAL_PATH + ".backup-" + str(Time.get_ticks_usec())
	if DirAccess.copy_absolute(LOCAL_PATH, backup) != OK:
		status.text = "本地备份失败，未应用云端进度"
		return
	if not write_local(data):
		status.text = "保存失败，本地备份位于：" + backup
		return
	progress = int(data.level)
	cloud_preview = {}
	status.text = "已采用云端等级 " + str(progress) + "，原存档备份：" + backup
