extends SceneTree

func _initialize() -> void:
	call_deferred("run")

func run() -> void:
	var client := MiniVerseClient.new()
	root.add_child(client)
	assert((await client.login()).ok)
	assert((await client.profile()).ok)
	var nonce := Crypto.new().generate_random_bytes(16).hex_encode()
	for i in range(3):
		assert((await client.event(nonce + "-" + str(i), "building_upgraded")).ok)
	var mails := await client.mail_list()
	assert(mails.ok and mails.data.size() > 0)
	var id: String = mails.data[0].id
	assert((await client.delete_mail(id)).status == 409)
	assert((await client.read_mail(id)).ok)
	assert((await client.claim_all_mail()).ok)
	var inventory := await client.inventory()
	assert(inventory.ok and inventory.data.wood > 0)
	var initial := int(inventory.data.wood)
	assert((await client.item_catalog()).ok)
	var spent := await client.consume_item(nonce, "wood", 1)
	assert(spent.ok and int(spent.data.balance) == initial - 1)
	var replay := await client.consume_item(nonce, "wood", 1)
	assert(replay.ok and int(replay.data.balance) == initial - 1)
	var ledger := await client.inventory_ledger()
	assert(ledger.ok and ledger.data.entries.size() >= 2)
	assert((await client.delete_mail(id)).ok)
	assert((await client.claim_mail(id)).status == 404)
	var other := MiniVerseClient.new()
	root.add_child(other)
	assert((await other.login()).ok)
	assert((await other.read_mail(id)).status == 404)
	assert((await other.inventory_ledger()).data.entries.is_empty())
	assert((await client.logout()).ok)
	client.queue_free()
	other.queue_free()
	await process_frame
	print("Godot real API: mail read/delete/claim-all, catalog, idempotent consumption, ledger and account isolation: PASS")
	quit(0)
