package server

import (
	"database/sql"
	"encoding/json"
	"testing"

	"agentmud/internal/db"
)

func TestOverburdenedMovementRejection(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	// Eldon starts at town_mill (unassisted capacity 20kg, overburden limit 30kg)
	// Add 35 kg of coal (35 * 1.0kg) to Eldon's inventory
	_, err = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'coal', 35)")
	if err != nil {
		t.Fatalf("Failed to add coal: %v", err)
	}

	// Try to move to town_square
	movePayload, _ := json.Marshal(MovePayload{TargetLocation: "town_square"})
	srv.handleMove("eldon_millwright", "req_test_1", movePayload)

	// Verify Eldon is STILL at town_mill because MOVE was rejected
	var currentLoc string
	err = database.QueryRow("SELECT current_location FROM characters WHERE id = 'eldon_millwright'").Scan(&currentLoc)
	if err != nil || currentLoc != "town_mill" {
		t.Errorf("Expected Eldon to remain at town_mill, got %s", currentLoc)
	}

	// Now reduce weight below capacity
	_, err = database.Exec("DELETE FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'coal'")
	if err != nil {
		t.Fatalf("Failed to remove coal: %v", err)
	}

	srv.handleMove("eldon_millwright", "req_test_2", movePayload)
	err = database.QueryRow("SELECT current_location FROM characters WHERE id = 'eldon_millwright'").Scan(&currentLoc)
	if err != nil || currentLoc != "town_square" {
		t.Errorf("Expected Eldon to move to town_square after relieving excess weight, got %s", currentLoc)
	}
}

func TestEncumberedEnergyDecayOnMove(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	// Eldon starts with 100 energy
	// Add 20 kg of coal: total weight = 5kg initial + 20kg coal = 25kg (ENCUMBERED: 20 < weight <= 30)
	_, err = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'coal', 20)")
	if err != nil {
		t.Fatalf("Failed to add coal: %v", err)
	}

	// Move to market while encumbered
	movePayload, _ := json.Marshal(MovePayload{TargetLocation: "market"})
	srv.handleMove("eldon_millwright", "req_enc_1", movePayload)

	var energy int
	err = database.QueryRow("SELECT energy FROM characters WHERE id = 'eldon_millwright'").Scan(&energy)
	if err != nil {
		t.Fatalf("Failed to query energy: %v", err)
	}
	if energy != 98 {
		t.Errorf("Expected energy to decrease by 2 (double decay while encumbered), got %d", energy)
	}

	// Remove coal to become UNENCUMBERED
	_, _ = database.Exec("DELETE FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'coal'")

	// Move to church while unencumbered
	movePayload, _ = json.Marshal(MovePayload{TargetLocation: "church"})
	srv.handleMove("eldon_millwright", "req_enc_2", movePayload)

	err = database.QueryRow("SELECT energy FROM characters WHERE id = 'eldon_millwright'").Scan(&energy)
	if err != nil {
		t.Fatalf("Failed to query energy: %v", err)
	}
	if energy != 97 {
		t.Errorf("Expected energy to decrease by 1 while unencumbered, got %d", energy)
	}
}

func TestVehicleAccessibilityOnMove(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	// Gideon the Blacksmith starts with wheelbarrow equipped
	// 1. Wheelbarrow attempting to enter deep_forest (FOOT_ONLY)
	movePayload, _ := json.Marshal(MovePayload{TargetLocation: "deep_forest"})
	srv.handleMove("gideon_forge", "req_veh_1", movePayload)

	var loc string
	_ = database.QueryRow("SELECT current_location FROM characters WHERE id = 'gideon_forge'").Scan(&loc)
	if loc != "blacksmith" {
		t.Errorf("Wheelbarrow should be blocked from FOOT_ONLY deep_forest, but moved to %s", loc)
	}

	// 2. Equip Draft Horse & Cart
	_, _ = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('gideon_forge', 'draft_horse_cart', 1)")
	_ = db.EquipContainer(database, "gideon_forge", "draft_horse_cart")

	// Draft Horse & Cart attempting to enter tavern (indoor)
	movePayload, _ = json.Marshal(MovePayload{TargetLocation: "tavern"})
	srv.handleMove("gideon_forge", "req_veh_2", movePayload)

	_ = database.QueryRow("SELECT current_location FROM characters WHERE id = 'gideon_forge'").Scan(&loc)
	if loc != "blacksmith" {
		t.Errorf("Draft horse & cart should be blocked from indoor tavern, but moved to %s", loc)
	}

	// Draft Horse & Cart entering stables (ALL, outdoor)
	movePayload, _ = json.Marshal(MovePayload{TargetLocation: "stables"})
	srv.handleMove("gideon_forge", "req_veh_3", movePayload)

	_ = database.QueryRow("SELECT current_location FROM characters WHERE id = 'gideon_forge'").Scan(&loc)
	if loc != "stables" {
		t.Errorf("Draft horse & cart should be permitted into stables, got %s", loc)
	}
}

func TestGroundItemsDropAndPickupIntents(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	// Eldon starts at town_mill with 4 wheat
	dropPayload, _ := json.Marshal(ItemActionPayload{ItemID: "wheat", Quantity: 2})
	srv.handleDrop("eldon_millwright", "req_drop_1", dropPayload)

	var invQty, groundQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&invQty)
	_ = database.QueryRow("SELECT quantity FROM location_items WHERE location_id = 'town_mill' AND item_id = 'wheat'").Scan(&groundQty)

	if invQty != 2 {
		t.Errorf("Expected 2 wheat in inventory, got %d", invQty)
	}
	if groundQty != 2 {
		t.Errorf("Expected 2 wheat on ground, got %d", groundQty)
	}

	// Pickup 1 wheat
	pickupPayload, _ := json.Marshal(ItemActionPayload{ItemID: "wheat", Quantity: 1})
	srv.handlePickup("eldon_millwright", "req_pick_1", pickupPayload)

	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&invQty)
	_ = database.QueryRow("SELECT quantity FROM location_items WHERE location_id = 'town_mill' AND item_id = 'wheat'").Scan(&groundQty)

	if invQty != 3 {
		t.Errorf("Expected 3 wheat in inventory after pickup, got %d", invQty)
	}
	if groundQty != 1 {
		t.Errorf("Expected 1 wheat on ground after pickup, got %d", groundQty)
	}
}

func TestTemporalForagingAndActionLock(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	// Move Eldon to deep_forest
	_, _ = database.Exec("UPDATE characters SET current_location = 'deep_forest' WHERE id = 'eldon_millwright'")

	// 1. Submit FORAGE intent for firewood, duration 60 ticks
	foragePayload, _ := json.Marshal(ForagePayload{
		TargetResource: "firewood",
		DurationTicks:  60,
	})
	srv.handleForage("eldon_millwright", "req_forage_1001", foragePayload)

	// Check character state transitioned to FORAGING
	var status string
	var startTick, endTick uint64
	err = database.QueryRow("SELECT status, action_start_tick, action_end_tick FROM characters WHERE id = 'eldon_millwright'").Scan(&status, &startTick, &endTick)
	if err != nil || status != "FORAGING" {
		t.Fatalf("Expected character status FORAGING, got %s (err: %v)", status, err)
	}
	if endTick != startTick+60 {
		t.Errorf("Expected endTick to be startTick+60, got %d vs %d", endTick, startTick+60)
	}

	// 2. Concurrency Protection (RFC 007 Section 6: Action Locks)
	// Try to MOVE while busy
	movePayload, _ := json.Marshal(MovePayload{TargetLocation: "town_square"})
	srv.handleMove("eldon_millwright", "req_busy_move", movePayload)

	var loc string
	_ = database.QueryRow("SELECT current_location FROM characters WHERE id = 'eldon_millwright'").Scan(&loc)
	if loc != "deep_forest" {
		t.Errorf("Character should NOT move while busy foraging, moved to %s", loc)
	}

	// Try to TRANSFER while busy
	transferPayload, _ := json.Marshal(TransferPayload{RecipientID: "martha_barkeep", Amount: 2})
	srv.handleTransfer("eldon_millwright", "req_busy_transfer", transferPayload)

	var eldonGold int
	_ = database.QueryRow("SELECT gold FROM characters WHERE id = 'eldon_millwright'").Scan(&eldonGold)
	if eldonGold != 15 {
		t.Errorf("Character should NOT transfer gold while busy foraging, gold changed to %d", eldonGold)
	}

	// 3. Fast-forward clock to action_end_tick and process tick
	srv.SetTickCount(endTick)
	srv.ProcessTick()

	// 4. Verify resolution
	err = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if err != nil || status != "IDLE" {
		t.Errorf("Expected character status to return to IDLE, got %s", status)
	}

	var firewoodQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'firewood'").Scan(&firewoodQty)
	if firewoodQty < 1 {
		t.Errorf("Expected at least 1 firewood in inventory after 60 ticks of foraging, got %d", firewoodQty)
	}

	var woodXP int
	_ = database.QueryRow("SELECT experience FROM character_skills WHERE character_id = 'eldon_millwright' AND skill_id = 'woodcutting'").Scan(&woodXP)
	if woodXP < 15 {
		t.Errorf("Expected at least 15 woodcutting XP gained, got %d", woodXP)
	}
}

func TestEarlyCancellationProratedReturns(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	// Move Eldon to prairie
	_, _ = database.Exec("UPDATE characters SET current_location = 'prairie' WHERE id = 'eldon_millwright'")

	// Start 60 tick forage for animal_fodder
	foragePayload, _ := json.Marshal(ForagePayload{
		TargetResource: "animal_fodder",
		DurationTicks:  60,
	})
	srv.handleForage("eldon_millwright", "req_cancel_forage", foragePayload)

	// Advance clock by 24 ticks
	srv.SetTickCount(srv.GetTickCount() + 24)

	// Cancel action
	cancelPayload, _ := json.Marshal(map[string]interface{}{})
	srv.handleCancelAction("eldon_millwright", "req_cancel_early", cancelPayload)

	// Verify status is IDLE
	var status string
	err = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if err != nil || status != "IDLE" {
		t.Errorf("Expected character status IDLE after cancellation, got %s", status)
	}

	// Verify prorated XP: 24 ticks * 0.25 = 6 XP
	var forageXP int
	_ = database.QueryRow("SELECT experience FROM character_skills WHERE character_id = 'eldon_millwright' AND skill_id = 'foraging'").Scan(&forageXP)
	if forageXP != 6 {
		t.Errorf("Expected prorated 6 foraging XP for 24 ticks, got %d", forageXP)
	}
}

func TestEquipAndUnequipIntents(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	// Give Eldon a leather_backpack
	_, _ = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'leather_backpack', 1)")

	equipPayload, _ := json.Marshal(EquipPayload{ItemID: "leather_backpack"})
	srv.handleEquip("eldon_millwright", "req_equip_1", equipPayload)

	var equipped sql.NullString
	_ = database.QueryRow("SELECT equipped_container_id FROM characters WHERE id = 'eldon_millwright'").Scan(&equipped)
	if !equipped.Valid || equipped.String != "leather_backpack" {
		t.Errorf("Expected equipped_container_id to be leather_backpack, got %v", equipped)
	}

	// Unequip container
	unequipPayload, _ := json.Marshal(map[string]interface{}{})
	srv.handleUnequip("eldon_millwright", "req_unequip_1", unequipPayload)

	_ = database.QueryRow("SELECT equipped_container_id FROM characters WHERE id = 'eldon_millwright'").Scan(&equipped)
	if equipped.Valid && equipped.String != "" {
		t.Errorf("Expected container to be unequipped, got %v", equipped)
	}
}
