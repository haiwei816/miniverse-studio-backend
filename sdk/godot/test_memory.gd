extends SceneTree
func _initialize() -> void:
	call_deferred("run")
func run() -> void:
	var login_client := MiniVerseClient.new()
	root.add_child(login_client)
	var login := await login_client.login()
	assert(login.ok)
	var token: String = login.data.access_token
	login_client.queue_free()
	await process_frame
	var orphans := int(Performance.get_monitor(Performance.OBJECT_ORPHAN_NODE_COUNT))
	var nodes := int(Performance.get_monitor(Performance.OBJECT_NODE_COUNT))
	for i in range(100):
		var client := MiniVerseClient.new()
		root.add_child(client)
		client.access_token = token
		assert((await client.profile()).ok)
		await process_frame
		assert(client.get_child_count() == 0, "HTTPRequest node retained")
		client.queue_free()
		await process_frame
	assert(int(Performance.get_monitor(Performance.OBJECT_ORPHAN_NODE_COUNT)) == orphans)
	assert(int(Performance.get_monitor(Performance.OBJECT_NODE_COUNT)) == nodes)
	print("100 SDK request/create/free cycles: node and orphan counts returned to baseline")
	quit(0)
