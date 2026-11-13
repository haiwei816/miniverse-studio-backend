extends Control

var client := MiniVerseClient.new()
var secret := LineEdit.new()
var inbox := ItemList.new()
var item := LineEdit.new()
var amount := SpinBox.new()
var status := Label.new()
var detail := Label.new()
var mails: Array = []
var busy := false
var consume_id := ""

func _ready() -> void:
	add_child(client)
	var scroll := ScrollContainer.new()
	scroll.set_anchors_and_offsets_preset(Control.PRESET_FULL_RECT)
	add_child(scroll)
	var column := VBoxContainer.new()
	column.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	scroll.add_child(column)
	status.text = "邮件与服务端库存演示 · 需要本地 sandbox 后端"
	status.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	column.add_child(status)
	secret.secret = true
	secret.placeholder_text = "账号恢复凭据；留空创建新的测试玩家"
	column.add_child(secret)
	button(column, "登录 / 恢复测试账号", connect_backend)
	button(column, "复制测试恢复凭据", func(): DisplayServer.clipboard_set(secret.text))
	button(column, "完成一次示例建筑任务（累计三次产生奖励邮件）", play)
	button(column, "刷新邮件、道具目录和流水", refresh)
	inbox.custom_minimum_size = Vector2(0, 150)
	column.add_child(inbox)
	button(column, "标记选中邮件已读", func(): await mail_action("read"))
	button(column, "领取选中邮件", func(): await mail_action("claim"))
	button(column, "删除选中邮件（未领取有效附件时拒绝）", func(): await mail_action("delete"))
	button(column, "一键领取（本批最多 100 封）", claim_all)
	item.text = "wood"
	item.placeholder_text = "道具 ID"
	column.add_child(item)
	amount.min_value = 1
	amount.max_value = 1000000
	amount.value = 1
	column.add_child(amount)
	new_consumption()
	button(column, "消耗道具 / 重试当前操作", consume)
	button(column, "开始一笔新的消耗操作", new_consumption)
	detail.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	column.add_child(detail)

func button(parent: Node, text: String, action: Callable) -> void:
	var control := Button.new()
	control.text = text
	control.pressed.connect(action)
	parent.add_child(control)

func new_id() -> String:
	return Crypto.new().generate_random_bytes(16).hex_encode()

func new_consumption() -> void:
	if not busy:
		consume_id = new_id()
		status.text = "已准备新的消耗操作；网络失败时重试会沿用同一请求 ID"

func connect_backend() -> void:
	if busy:
		return
	busy = true
	var result := await client.login(secret.text)
	if result.ok:
		secret.text = result.data.secret
		consume_id = new_id()
		await client.profile()
	status.text = "登录成功；恢复凭据仅在内存，请妥善保管" if result.ok else "登录失败，请启动 sandbox 后端"
	busy = false
	if result.ok:
		await refresh()

func refresh() -> void:
	if busy:
		return
	busy = true
	var result := await client.mail_list()
	inbox.clear()
	mails = []
	if result.ok:
		mails = result.data
		for mail in mails:
			inbox.add_item(("已领 " if mail.claimed else "待领 ") + ("已读 " if mail.read else "未读 ") + mail.title + " · " + mail.item + " × " + str(mail.amount))
	var catalog := await client.item_catalog()
	var ledger := await client.inventory_ledger()
	detail.text = "道具目录与余额：\n" + JSON.stringify(catalog.get("data", {}), "  ") + "\n最近流水：\n" + JSON.stringify(ledger.get("data", {}), "  ")
	busy = false

func play() -> void:
	if busy:
		return
	busy = true
	var result := await client.event(new_id(), "building_upgraded")
	status.text = "示例事件结果：" + JSON.stringify(result)
	busy = false
	await refresh()

func mail_action(action: String) -> void:
	if busy or inbox.get_selected_items().is_empty():
		return
	var id: String = mails[inbox.get_selected_items()[0]].id
	busy = true
	var result: Dictionary
	match action:
		"read": result = await client.read_mail(id)
		"claim": result = await client.claim_mail(id)
		"delete": result = await client.delete_mail(id)
	status.text = "邮件操作结果：" + JSON.stringify(result)
	busy = false
	await refresh()

func claim_all() -> void:
	if busy:
		return
	busy = true
	var result := await client.claim_all_mail()
	status.text = "本批领取结果：" + JSON.stringify(result)
	busy = false
	await refresh()

func consume() -> void:
	if busy:
		return
	busy = true
	var result := await client.consume_item(consume_id, item.text, int(amount.value))
	status.text = "消耗结果：" + JSON.stringify(result)
	# Keep the ID until the player explicitly starts another operation. Even a
	# double click after success replays the original result instead of spending twice.
	busy = false
	await refresh()
