class_name MiniVerseClient
extends Node

signal network_changed(online: bool)
var base_url := "http://127.0.0.1:18080"
var game_id := "sky-island"
var access_token := ""
var expires_at := 0
var online := true
var cache_path := "user://miniverse_cache.json"

func request(path: String, method := HTTPClient.METHOD_GET, body: Dictionary = {}, retry := true) -> Dictionary:
	var attempts := 3 if retry and method == HTTPClient.METHOD_GET else 1
	for attempt in range(attempts):
		var http := HTTPRequest.new()
		add_child(http)
		http.timeout = 5.0
		var headers := PackedStringArray(["Content-Type: application/json"])
		if not access_token.is_empty():
			headers.append("Authorization: Bearer " + access_token)
		var err := http.request(base_url + path, headers, method, JSON.stringify(body) if method != HTTPClient.METHOD_GET else "")
		if err != OK:
			http.queue_free()
			return {"ok": false, "error": "request_start_failed"}
		var result: Array = await http.request_completed
		http.queue_free()
		online = result[0] == HTTPRequest.RESULT_SUCCESS
		network_changed.emit(online)
		if online:
			var parsed = JSON.parse_string(result[3].get_string_from_utf8())
			if result[1] == 401:
				access_token = ""
			return {"ok": result[1] >= 200 and result[1] < 300, "status": result[1], "data": parsed}
		if attempt + 1 < attempts:
			await get_tree().create_timer(0.2 * (attempt + 1)).timeout
	return {"ok": false, "error": "offline"}

func login(secret := "") -> Dictionary:
	var result := await request("/v1/auth/guest", HTTPClient.METHOD_POST, {"secret": secret}, false)
	if result.ok:
		access_token = result.data.access_token
		expires_at = int(result.data.expires_at)
	return result

func profile() -> Dictionary:
	return await request("/v1/games/" + game_id.uri_encode() + "/profile")

func logout() -> Dictionary:
	var result := await request("/v1/auth/logout", HTTPClient.METHOD_POST, {}, false)
	access_token = ""
	return result

func cache_save(data: Dictionary) -> void:
	var file := FileAccess.open(cache_path + ".tmp", FileAccess.WRITE)
	file.store_string(JSON.stringify(data))
	file.close()
	DirAccess.rename_absolute(cache_path + ".tmp", cache_path)

func cache_load() -> Dictionary:
	if not FileAccess.file_exists(cache_path):
		return {}
	var data = JSON.parse_string(FileAccess.get_file_as_string(cache_path))
	return data if data is Dictionary else {}

func cloud_save(slot: String, revision: int, data: Dictionary, schema_version := 1) -> Dictionary:
	var text := JSON.stringify(data)
	var pending := {"revision": revision, "schema_version": schema_version, "data": text, "checksum": text.sha256_text()}
	var cache := cache_load()
	var key := game_id + "/" + slot
	cache[key] = {"pending": pending, "conflict": false}
	cache_save(cache)
	var result := await request("/v1/games/" + game_id.uri_encode() + "/saves/" + slot.uri_encode(), HTTPClient.METHOD_PUT, pending, false)
	if result.ok:
		cache[key] = {"cloud": result.data, "conflict": false}
	elif result.get("status", 0) == 409:
		var remote := await cloud_load(slot)
		cache[key] = {"pending": pending, "cloud": remote.get("data", {}), "conflict": true}
	cache_save(cache)
	return result

func cloud_load(slot: String) -> Dictionary:
	return await request("/v1/games/" + game_id.uri_encode() + "/saves/" + slot.uri_encode())

func game_request(path: String, method := HTTPClient.METHOD_GET, body: Dictionary = {}) -> Dictionary:
	return await request("/v1/games/" + game_id.uri_encode() + path, method, body)

func event(event_id: String, event_type: String, amount := 1) -> Dictionary:
	return await game_request("/events", HTTPClient.METHOD_POST, {"event_id": event_id, "event_type": event_type, "amount": amount})

func sync_pending() -> Dictionary:
	var cache := cache_load()
	var results := {}
	for key in cache.keys():
		if not str(key).begins_with(game_id + "/") or not cache[key] is Dictionary:
			continue
		if cache[key].has("pending"):
			var pending: Dictionary = cache[key].pending
			var slot := str(key).substr(game_id.length() + 1)
			var data = JSON.parse_string(pending.data)
			if data is Dictionary:
				results[slot] = await cloud_save(slot, int(pending.revision), data, int(pending.schema_version))
	return results
