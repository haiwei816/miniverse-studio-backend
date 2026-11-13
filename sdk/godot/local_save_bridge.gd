class_name MiniVerseLocalSaveBridge
extends RefCounted

# The game's existing JSON file is only read. Cloud data is never applied to it.
# The game owns schema conversion and the player's local/cloud choice.
var client: MiniVerseClient

func _init(sdk: MiniVerseClient) -> void:
	client = sdk

func read_local(path: String) -> Dictionary:
	if not path.begins_with("user://"):
		return {"ok": false, "error": "local_path_required"}
	var file := FileAccess.open(path, FileAccess.READ)
	if file == null:
		return {"ok": false, "error": "local_save_unavailable"}
	if file.get_length() > 1048576:
		return {"ok": false, "error": "local_save_too_large"}
	var parser := JSON.new()
	if parser.parse(file.get_as_text()) != OK or not parser.data is Dictionary:
		return {"ok": false, "error": "invalid_local_save"}
	return {"ok": true, "local_data": parser.data}

# Safe to call before asking the player to import; performs no writes.
func inspect(slot: String, path: String) -> Dictionary:
	var local := read_local(path)
	if not local.ok:
		return local
	if client.player_id.is_empty() or client.access_token.is_empty():
		return {"ok": false, "error": "login_required", "local_data": local.local_data}
	var scope := client.session_scope()
	var game := client.game_id
	var remote := await client.cloud_load(slot)
	if scope != client.session_scope() or game != client.game_id:
		return {"ok": false, "error": "session_changed"}
	if remote.ok:
		return {"ok": true, "action": "choose_version", "local_data": local.local_data, "cloud": remote.data}
	if remote.get("status", 0) == 404 and remote.get("data", {}) is Dictionary and remote.data.get("error", "") == "save_not_found":
		return {"ok": true, "action": "can_import", "local_data": local.local_data}
	return {"ok": false, "error": "cloud_unavailable", "remote": remote, "local_data": local.local_data}

# Call only after the player elects to upload local progress. Rechecks the cloud
# and uses revision zero, so concurrent imports cannot overwrite one another.
func import_local(slot: String, path: String, schema_version := 1) -> Dictionary:
	var state := await inspect(slot, path)
	if not state.ok:
		return state
	if state.action != "can_import":
		return {"ok": false, "error": "cloud_exists", "local_data": state.local_data, "cloud": state.cloud}
	return await client.cloud_save(slot, 0, state.local_data, schema_version)
