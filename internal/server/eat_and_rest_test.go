package server

import (
	"encoding/json"
	"testing"

	"agentmud/internal/db"
)

func TestEatIntent(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Set Eldon's hunger to 50 and give him bread
	_, err = database.Exec("UPDATE characters SET hunger = 50 WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to set hunger: %v", err)
	}
	_, err = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'bread', 2)")
	if err != nil {
		t.Fatalf("Failed to insert bread: %v", err)
	}

	// Test eating non-consumable item
	wrenchPayload, _ := json.Marshal(ItemActionPayload{ItemID: "iron_wrench", Quantity: 1})
	srv.handleEat("eldon_millwright", "req_eat_wrench", wrenchPayload)

	var hunger int
	_ = database.QueryRow("SELECT hunger FROM characters WHERE id = 'eldon_millwright'").Scan(&hunger)
	if hunger != 50 {
		t.Errorf("Expected hunger to remain 50 after trying to eat wrench, got %d", hunger)
	}

	// Test eating bread (bread restores 25 hunger)
	breadPayload, _ := json.Marshal(ItemActionPayload{ItemID: "bread", Quantity: 1})
	srv.handleEat("eldon_millwright", "req_eat_bread", breadPayload)

	_ = database.QueryRow("SELECT hunger FROM characters WHERE id = 'eldon_millwright'").Scan(&hunger)
	if hunger != 75 { // 50 + 25
		t.Errorf("Expected hunger to be 75 after eating bread, got %d", hunger)
	}

	var breadQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'bread'").Scan(&breadQty)
	if breadQty != 1 { // 2 - 1
		t.Errorf("Expected 1 bread remaining in inventory, got %d", breadQty)
	}
}

func TestRestIntentAndCompletion(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Set Eldon's energy low
	_, err = database.Exec("UPDATE characters SET energy = 40 WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to set energy: %v", err)
	}

	// Submit REST intent for 30 ticks
	restPayload, _ := json.Marshal(map[string]int{"duration_ticks": 30})
	srv.handleRest("eldon_millwright", "req_rest_1", restPayload)

	var status string
	var startTick, endTick uint64
	err = database.QueryRow("SELECT status, action_start_tick, action_end_tick FROM characters WHERE id = 'eldon_millwright'").Scan(&status, &startTick, &endTick)
	if err != nil || status != "RESTING" {
		t.Fatalf("Expected status RESTING, got %s (err: %v)", status, err)
	}
	if endTick != startTick+30 {
		t.Errorf("Expected endTick to be startTick+30, got %d vs %d", endTick, startTick+30)
	}

	// Fast-forward clock and process tick
	srv.SetTickCount(endTick)
	srv.ProcessTick()

	// Verify resolution
	_ = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if status != "IDLE" {
		t.Errorf("Expected status IDLE after rest completion, got %s", status)
	}

	var energy int
	_ = database.QueryRow("SELECT energy FROM characters WHERE id = 'eldon_millwright'").Scan(&energy)
	if energy != 90 { // 40 + 50 restored
		t.Errorf("Expected 90 energy after resting, got %d", energy)
	}
}
