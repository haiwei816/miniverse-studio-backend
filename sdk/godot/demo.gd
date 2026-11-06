extends Control
var client := MiniVerseClient.new()
func _ready() -> void:
	add_child(client)
	var label := Label.new()
	label.position = Vector2(24, 24)
	add_child(label)
	label.text = "MiniVerse: connecting..."
	var result := await client.login()
	if result.ok:
		var profile := await client.profile()
		label.text = "Game: " + client.game_id + "\n" + JSON.stringify(profile)
	else:
		label.text = "Offline: local play available\n" + JSON.stringify(client.cache_load())
