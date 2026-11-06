extends SceneTree
func _initialize() -> void:
	call_deferred("run")
func run() -> void:
	var client := MiniVerseClient.new()
	root.add_child(client)
	assert((await client.login()).ok)
	assert((await client.cloud_save("sdk", 0, {"level": 1})).ok)
	var conflict := await client.cloud_save("sdk", 0, {"level": 2})
	assert(conflict.status == 409)
	var cache := client.cache_load()
	assert(cache[client.game_id + "/sdk"].conflict)
	assert(cache[client.game_id + "/sdk"].pending.data.contains("2"))
	assert((await client.cloud_load("sdk")).data.revision == 1)
	print("S3 Godot cloud save/conflict preserves pending and remote: PASS")
	quit(0)
