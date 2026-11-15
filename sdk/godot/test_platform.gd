extends SceneTree

func _initialize() -> void:
	call_deferred("run")

func check(value: bool, label: String) -> bool:
	if not value:
		push_error(label)
		quit(1)
	return value

func run() -> void:
	var platform := MiniVerseClient.new()
	root.add_child(platform)
	# Sandbox-only fixture; platform_login is used for real username accounts.
	var fixture := await platform.login()
	if not check(fixture.ok, "platform fixture login failed"):
		return
	var observer := MiniVerseClient.new()
	root.add_child(observer)
	if not check((await observer.login(fixture.data.secret)).ok, "observer fixture login failed"):
		return
	var verifier := "godot-platform-fixture-" + str(Time.get_ticks_usec()).sha256_text()
	var challenge := Marshalls.raw_to_base64(verifier.sha256_buffer()).replace("+", "-").replace("/", "_").trim_suffix("=")
	var launch := await platform.request("/v1/platform/games/sky-island/launch", HTTPClient.METHOD_POST, {"code_challenge": challenge}, false)
	if not check(launch.ok, "launch failed: " + str(launch)):
		return
	var game := MiniVerseClient.new()
	root.add_child(game)
	if not check((await game.exchange_launch("sky-island", launch.data.launch_code, verifier)).ok, "launch exchange failed"):
		return
	if not check(game.companion_epoch == int(launch.data.epoch), "context epoch not installed"):
		return
	if not check((await game.profile()).ok, "scoped profile failed"):
		return
	if not check((await game.companion_context({"scene_id": "sdk-test", "quest_stage": "explore", "visible_targets": []})).ok, "scoped companion context failed"):
		return
	game.game_id = "card-game"
	if not check((await game.profile()).status == 403, "cross-game access accepted"):
		return
	game.game_id = "sky-island"
	if not check((await game.logout()).ok, "game-scoped logout failed"):
		return
	if not check((await platform.logout()).ok, "platform logout failed"):
		return
	var cleared := await observer.request("/v1/platform/companion/context", HTTPClient.METHOD_GET, {}, false)
	if not check(cleared.ok and cleared.data.game_id == "" and int(cleared.data.epoch) > int(launch.data.epoch), "platform logout retained the old companion world"):
		return
	await observer.logout()
	print("Player SDK PKCE exchange, game scope, companion epoch and clearing logout: PASS")
	quit(0)
