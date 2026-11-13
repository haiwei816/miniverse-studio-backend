extends SceneTree
func _initialize() -> void:
	call_deferred("run")
func run() -> void:
	var client := MiniVerseClient.new()
	root.add_child(client)
	var login := await client.login()
	assert(login.ok, str(login))
	var profile := await client.profile()
	assert(profile.ok)
	client.cache_save({"offline_progress": 42})
	assert(client.cache_load().offline_progress == 42)
	var original := client.base_url
	client.base_url = "http://127.0.0.1:1"
	var offline := await client.profile()
	assert(not offline.ok)
	client.base_url = original
	assert(client.cache_load().offline_progress == 42)
	assert((await client.profile()).ok)
	assert((await client.logout()).ok)
	assert(client.access_token.is_empty())
	print("S2 SDK login, profile, cache, offline/reconnect, logout: PASS")
	quit(0)
