package server

import (
	"encoding/json"
	"testing"

	"agentmud/internal/db"
)

func TestCraftIntentAndConcurrencyLock(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Eldon is at town_mill with 4 wheat and 2 flour
	craftPayload, _ := json.Marshal(CraftPayload{RecipeID: "mill_flour"})
	srv.handleCraft("eldon_millwright", "req_craft_1", craftPayload)

	// Verify character state transitioned to CRAFTING
	var status string
	var startTick, endTick uint64
	err = database.QueryRow("SELECT status, action_start_tick, action_end_tick FROM characters WHERE id = 'eldon_millwright'").Scan(&status, &startTick, &endTick)
	if err != nil || status != "CRAFTING" {
		t.Fatalf("Expected status CRAFTING, got %s (err: %v)", status, err)
	}
	if endTick != startTick+4 {
		t.Errorf("Expected endTick to be startTick+4, got %d vs %d", endTick, startTick+4)
	}

	// Concurrency Lock: Check that MOVE is rejected with CHARACTER_BUSY
	movePayload, _ := json.Marshal(MovePayload{TargetLocation: "town_square"})
	srv.handleMove("eldon_millwright", "req_busy_move", movePayload)

	var loc string
	_ = database.QueryRow("SELECT current_location FROM characters WHERE id = 'eldon_millwright'").Scan(&loc)
	if loc != "town_mill" {
		t.Errorf("Character should NOT move while busy crafting, moved to %s", loc)
	}

	// Concurrency Lock: Check that TRANSFER is rejected
	transferPayload, _ := json.Marshal(TransferPayload{RecipientID: "martha_barkeep", Amount: 2})
	srv.handleTransfer("eldon_millwright", "req_busy_transfer", transferPayload)

	var gold int
	_ = database.QueryRow("SELECT gold FROM characters WHERE id = 'eldon_millwright'").Scan(&gold)
	if gold != 15 {
		t.Errorf("Character should NOT transfer gold while busy crafting, gold changed to %d", gold)
	}

	// Fast-forward clock and process tick
	srv.SetTickCount(endTick)
	srv.ProcessTick()

	// Verify resolution
	_ = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if status != "IDLE" {
		t.Errorf("Expected status IDLE after crafting completion, got %s", status)
	}

	var flourQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'flour'").Scan(&flourQty)
	if flourQty != 4 { // 2 initial + 2 produced
		t.Errorf("Expected 4 flour in inventory after milling, got %d", flourQty)
	}

	var energy int
	_ = database.QueryRow("SELECT energy FROM characters WHERE id = 'eldon_millwright'").Scan(&energy)
	if energy != 95 { // 100 - 5
		t.Errorf("Expected 95 energy after milling, got %d", energy)
	}
}

func TestCraftSpatialWorkstationValidation(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Eldon is at town_mill, tries to bake bread (requires stone_hearth at bakery)
	craftPayload, _ := json.Marshal(CraftPayload{RecipeID: "bake_bread"})
	srv.handleCraft("eldon_millwright", "req_invalid_ws", craftPayload)

	// Verify character remains IDLE and did not lock
	var status string
	_ = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if status != "IDLE" {
		t.Errorf("Expected character to remain IDLE after rejected craft, got %s", status)
	}
}

func TestCraftEarlyCancellation(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Eldon starts crafting mill_flour (consumes 2 wheat)
	craftPayload, _ := json.Marshal(CraftPayload{RecipeID: "mill_flour"})
	srv.handleCraft("eldon_millwright", "req_craft_cancel", craftPayload)

	var wheatQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&wheatQty)
	if wheatQty != 2 {
		t.Errorf("Expected 2 wheat remaining after starting craft, got %d", wheatQty)
	}

	// Cancel crafting
	cancelPayload, _ := json.Marshal(map[string]interface{}{})
	srv.handleCancelAction("eldon_millwright", "req_cancel_craft", cancelPayload)

	// Verify character status is back to IDLE
	var status string
	_ = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if status != "IDLE" {
		t.Errorf("Expected status IDLE after cancellation, got %s", status)
	}

	// Verify wheat refunded
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&wheatQty)
	if wheatQty != 4 {
		t.Errorf("Expected 4 wheat after cancellation refund, got %d", wheatQty)
	}
}
