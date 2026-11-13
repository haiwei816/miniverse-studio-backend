extends SceneTree

# Protocol fixture, not a MySQL integration test. The production SDK's import,
# revision/conflict logic and on-disk caches run unchanged.
class FakeClient extends MiniVerseClient:
	var remote := {}
	var writes := 0
	var offline := false
	var race_import := false
	var change_account := false
	var read_error := {}
	var stage_newer := false

	func request(_path: String, method := HTTPClient.METHOD_GET, body: Dictionary = {}, _retry := true) -> Dictionary:
		await get_tree().process_frame
		if change_account:
			change_account = false
			use_session("player-b", "token-b")
		if offline:
			return {"ok": false, "error": "offline"}
		if method == HTTPClient.METHOD_GET:
			if not read_error.is_empty():
				return read_error
			if remote.is_empty():
				return {"ok": false, "status": 404, "data": {"error": "save_not_found"}}
			return {"ok": true, "status": 200, "data": remote.duplicate(true)}
		writes += 1
		if stage_newer:
			stage_newer = false
			var cache := cache_load()
			var text := '{"level":123}'
			cache[game_id + "/later"] = {"pending": {"data": text, "checksum": text.sha256_text(), "revision": 0, "schema_version": 1}}
			cache_save(cache)
		if race_import:
			race_import = false
			remote = {"revision": 1, "data": '{"level":99}', "deleted": false}
		if int(body.revision) != int(remote.get("revision", 0)):
			return {"ok": false, "status": 409, "data": {"error": "revision_conflict"}}
		remote = body.duplicate(true)
		remote.revision = int(body.revision) + 1
		return {"ok": true, "status": 200, "data": remote.duplicate(true)}

func _initialize() -> void:
	call_deferred("run")

func run() -> void:
	var client := FakeClient.new()
	root.add_child(client)
	var nonce := str(Time.get_ticks_usec())
	client.cache_path = "user://bridge_test_" + nonce
	client.use_session("player-a", "token-a")
	var bridge := MiniVerseLocalSaveBridge.new(client)
	var path := "user://legacy_test_" + nonce + ".json"
	var original := '{"level":7,"coins":30}'
	var file := FileAccess.open(path, FileAccess.WRITE)
	file.store_string(original)
	file.close()
	assert(not bridge.read_local("res://project.godot").ok)
	assert(not bridge.read_local(path + ".missing").ok)
	file = FileAccess.open(path + ".invalid", FileAccess.WRITE)
	file.store_string("not-json")
	file.close()
	assert(bridge.read_local(path + ".invalid").error == "invalid_local_save")
	assert((await bridge.inspect("main", path)).action == "can_import")
	assert(client.writes == 0)
	assert((await bridge.import_local("main", path)).ok)
	assert(client.remote.revision == 1)
	assert(not client.cache_load()[client.game_id + "/main"].has("pending"))
	assert(client.cache_load()[client.game_id + "/main"].cloud.revision == 1)
	assert(JSON.parse_string(client.remote.data).level == 7)
	assert((await bridge.import_local("main", path)).error == "cloud_exists")
	assert(client.writes == 1)
	client.remote.deleted = true
	assert((await bridge.import_local("main", path)).error == "cloud_exists", "tombstone silently restored")
	client.remote = {}
	client.race_import = true
	var raced := await bridge.import_local("race", path)
	assert(raced.status == 409)
	assert(JSON.parse_string(client.remote.data).level == 99)
	assert(client.cache_load()[client.game_id + "/race"].conflict)
	var writes_before := client.writes
	client.read_error = {"ok": false, "status": 404, "data": {"error": "game_not_found"}}
	assert((await bridge.import_local("main", path)).error == "cloud_unavailable")
	client.read_error = {"ok": false, "status": 401, "data": {"error": "unauthorized"}}
	assert((await bridge.import_local("main", path)).error == "cloud_unavailable")
	assert(client.writes == writes_before, "failed reads caused uploads")
	client.read_error = {}
	client.remote = {}
	client.stage_newer = true
	assert((await client.cloud_save("later", 0, {"level": 8})).ok)
	assert(JSON.parse_string(client.cache_load()[client.game_id + "/later"].pending.data).level == 123, "older response erased newer local progress")
	client.offline = true
	assert((await bridge.inspect("main", path)).error == "cloud_unavailable")
	assert(FileAccess.get_file_as_string(path) == original)
	client.cache_save({"owner": "a"})
	var a_path := client.scoped_cache_path()
	client.use_session("player-b", "token-b")
	assert(client.cache_load().is_empty(), "account B sees account A cache")
	assert((await client.sync_pending()).is_empty())
	client.use_session("player-a", "new-token-a")
	assert(client.cache_load().owner == "a")
	var old_url := client.base_url
	client.base_url = "http://other-server"
	assert(client.cache_load().is_empty(), "server cache isolation failed")
	client.base_url = old_url
	client.game_id = "other-game"
	assert(client.cache_load().is_empty(), "game cache isolation failed")
	client.game_id = "sky-island"
	client.offline = false
	client.remote = {}
	client.change_account = true
	var changed := await client.cloud_save("main", 0, {"level": 8})
	assert(changed.error == "session_changed")
	assert(client.cache_load().is_empty(), "in-flight save contaminated new account")
	client.use_session("", "")
	assert((await bridge.import_local("main", path)).error == "login_required")
	assert(FileAccess.get_file_as_string(path) == original, "legacy file changed")
	DirAccess.remove_absolute(path)
	DirAccess.remove_absolute(path + ".invalid")
	DirAccess.remove_absolute(a_path)
	client.queue_free()
	await process_frame
	print("Local bridge: import, conflict, tombstone, offline, account/server/game isolation, in-flight switch: PASS")
	quit(0)
