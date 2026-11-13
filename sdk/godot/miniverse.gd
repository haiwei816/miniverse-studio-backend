class_name MiniVerseClient
extends Node

signal network_changed(online: bool)
var base_url := OS.get_environment("MINIVERSE_API_URL") if OS.has_environment("MINIVERSE_API_URL") else "http://127.0.0.1:18080"
var game_id := "sky-island"
var access_token := ""
var player_id := ""
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
		var request_token := access_token
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
			if result[1] == 401 and access_token == request_token:
				access_token = ""
				expires_at = 0
			return {"ok": result[1] >= 200 and result[1] < 300, "status": result[1], "data": parsed}
		if attempt + 1 < attempts:
			await get_tree().create_timer(0.2 * (attempt + 1)).timeout
	return {"ok": false, "error": "offline"}

func login(secret := "") -> Dictionary:
	var result := await request("/v1/auth/guest", HTTPClient.METHOD_POST, {"secret": secret}, false)
	if result.ok:
		use_session(result.data.player_id, result.data.access_token, int(result.data.expires_at))
	return result

# Real channel adapters can install a verified session without using guest login.
# Recovery credentials belong in platform secure storage, never in this cache.
func use_session(player: String, token: String, expiry := 0) -> void:
	player_id = player
	access_token = token
	expires_at = expiry

func session_scope() -> String:
	return base_url.trim_suffix("/") + "/" + player_id + "/" + game_id

func scoped_cache_path() -> String:
	return cache_path + "." + session_scope().sha256_text()

func profile() -> Dictionary:
	return await request("/v1/games/" + game_id.uri_encode() + "/profile")

func logout() -> Dictionary:
	var token := access_token
	var result := await request("/v1/auth/logout", HTTPClient.METHOD_POST, {}, false)
	if access_token == token or access_token.is_empty():
		access_token = ""
		player_id = ""
		expires_at = 0
	return result

func cache_save(data: Dictionary) -> Error:
	var path := scoped_cache_path()
	var file := FileAccess.open(path + ".tmp", FileAccess.WRITE)
	if file == null:
		push_error("MiniVerse cache could not be opened")
		return FileAccess.get_open_error()
	file.store_string(JSON.stringify(data))
	file.close()
	var error := DirAccess.rename_absolute(path + ".tmp", path)
	if error != OK:
		push_error("MiniVerse cache could not be replaced")
	return error

func cache_load() -> Dictionary:
	var path := scoped_cache_path()
	if not FileAccess.file_exists(path):
		return {}
	var data = JSON.parse_string(FileAccess.get_file_as_string(path))
	return data if data is Dictionary else {}

func same_pending(cache: Dictionary, key: String, pending: Dictionary) -> bool:
	var current: Dictionary = cache.get(key, {}).get("pending", {})
	# JSON parses numbers as floats, so compare version fields explicitly.
	return current.get("checksum", "") == pending.checksum and int(current.get("revision", -1)) == int(pending.revision) and int(current.get("schema_version", -1)) == int(pending.schema_version)

func cloud_save(slot: String, revision: int, data: Dictionary, schema_version := 1) -> Dictionary:
	if player_id.is_empty() or access_token.is_empty():
		return {"ok": false, "error": "login_required"}
	var scope := session_scope()
	var text := JSON.stringify(data)
	var pending := {"revision": revision, "schema_version": schema_version, "data": text, "checksum": text.sha256_text()}
	var cache := cache_load()
	var key := game_id + "/" + slot
	cache[key] = {"pending": pending, "conflict": false}
	if cache_save(cache) != OK:
		return {"ok": false, "error": "cache_write_failed"}
	var result := await request("/v1/games/" + game_id.uri_encode() + "/saves/" + slot.uri_encode(), HTTPClient.METHOD_PUT, pending, false)
	if scope != session_scope():
		return {"ok": false, "error": "session_changed"}
	# Another save may have staged newer local progress while HTTP was in flight.
	cache = cache_load()
	if not same_pending(cache, key, pending):
		return result
	if result.ok:
		cache[key] = {"cloud": result.data, "conflict": false}
	elif result.get("status", 0) == 409:
		var remote := await cloud_load(slot)
		if scope != session_scope():
			return {"ok": false, "error": "session_changed"}
		cache = cache_load()
		if not same_pending(cache, key, pending):
			return result
		cache[key] = {"pending": pending, "cloud": remote.get("data", {}), "conflict": true}
	if cache_save(cache) != OK:
		return {"ok": false, "error": "cache_write_failed", "remote": result}
	return result

func cloud_load(slot: String) -> Dictionary:
	return await request("/v1/games/" + game_id.uri_encode() + "/saves/" + slot.uri_encode())

func game_request(path: String, method := HTTPClient.METHOD_GET, body: Dictionary = {}) -> Dictionary:
	return await request("/v1/games/" + game_id.uri_encode() + path, method, body)

func event(event_id: String, event_type: String, amount := 1) -> Dictionary:
	return await game_request("/events", HTTPClient.METHOD_POST, {"event_id": event_id, "event_type": event_type, "amount": amount})

func mail_list(filter := "all", limit := 200) -> Dictionary:
	return await game_request("/mail?filter=" + filter.uri_encode() + "&limit=" + str(limit))

func read_mail(id: String) -> Dictionary:
	return await game_request("/mail/" + id.uri_encode() + "/read", HTTPClient.METHOD_POST)

func delete_mail(id: String) -> Dictionary:
	return await game_request("/mail/" + id.uri_encode(), HTTPClient.METHOD_DELETE)

func claim_mail(id: String) -> Dictionary:
	return await game_request("/mail/" + id.uri_encode() + "/claim", HTTPClient.METHOD_POST)

func claim_all_mail() -> Dictionary:
	return await game_request("/mail/claim-all", HTTPClient.METHOD_POST)

func inventory() -> Dictionary:
	return await game_request("/inventory")

func item_catalog(after := "", limit := 100) -> Dictionary:
	return await game_request("/items?after=" + after.uri_encode() + "&limit=" + str(limit))

func consume_item(request_id: String, item: String, amount: int) -> Dictionary:
	return await game_request("/inventory/consume", HTTPClient.METHOD_POST, {"request_id": request_id, "item": item, "amount": amount})

func inventory_ledger(before := "", limit := 50) -> Dictionary:
	var path := "/inventory/ledger?limit=" + str(limit)
	if not before.is_empty():
		path += "&before=" + before.uri_encode()
	return await game_request(path)

func sync_pending() -> Dictionary:
	if player_id.is_empty() or access_token.is_empty():
		return {"ok": false, "error": "login_required"}
	var scope := session_scope()
	var cache := cache_load()
	var results := {}
	for key in cache.keys():
		if scope != session_scope():
			return {"ok": false, "error": "session_changed"}
		if not str(key).begins_with(game_id + "/") or not cache[key] is Dictionary:
			continue
		if cache[key].has("pending"):
			var pending: Dictionary = cache[key].pending
			var slot := str(key).substr(game_id.length() + 1)
			var data = JSON.parse_string(pending.data)
			if data is Dictionary:
				results[slot] = await cloud_save(slot, int(pending.revision), data, int(pending.schema_version))
	return results
