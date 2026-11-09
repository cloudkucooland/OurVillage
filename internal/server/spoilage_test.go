package server

import (
	"encoding/json"
	"testing"

	"agentmud/internal/db"
)

func TestSpoilageTickAndConsumptionPenalties(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize DB: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Shutdown()

	// Insert bread with 10% freshness (near spoilage)
	_, err = database.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity, freshness_percent)
		VALUES ('eldon_millwright', 'bread', 1, 10)
	`)
	if err != nil {
		t.Fatalf("Failed to insert inventory item: %v", err)
	}

	// Set hunger low so we can measure restoration
	_, err = database.Exec(`
		UPDATE characters SET hunger = 50, health = 100 WHERE id = 'eldon_millwright'
	`)
	if err != nil {
		t.Fatalf("Failed to update character stats: %v", err)
	}

	// Eat near-spoilage bread (freshness 10% -> 50% effectiveness = 12 hunger restored, -10 health food poisoning)
	payload, _ := json.Marshal(map[string]any{"item_id": "bread", "quantity": 1})
	srv.handleEat("eldon_millwright", "req_eat_spoilage", payload)

	var hunger, health int
	if err := database.QueryRow("SELECT hunger, health FROM characters WHERE id = 'eldon_millwright'").Scan(&hunger, &health); err != nil {
		t.Fatalf("Failed to query character state: %v", err)
	}

	// Starting hunger 50 + 12 (50% of 25) = 62
	if hunger != 62 {
		t.Errorf("hunger = %d, want 62 (reduced restoration due to near spoilage)", hunger)
	}
	// Starting health 100 - 10 (food poisoning penalty) = 90
	if health != 90 {
		t.Errorf("health = %d, want 90 (food poisoning penalty from near-spoilage food)", health)
	}
}

func TestSpoilageTickTransmutationToRottingWaste(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize DB: %v", err)
	}
	defer database.Close()

	// Insert bread with very low freshness that will drop <= 0 on next tick
	_, err = database.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity, freshness_percent)
		VALUES ('eldon_millwright', 'bread', 2, 1)
	`)
	if err != nil {
		t.Fatalf("Failed to insert inventory item: %v", err)
	}

	// Run spoilage tick
	if err := db.ProcessSpoilageTicks(database, 100); err != nil {
		t.Fatalf("ProcessSpoilageTicks failed: %v", err)
	}

	// Verify bread is gone and rotting_waste is in inventory with quantity 2
	var breadQty, wasteQty int
	_ = database.QueryRow("SELECT COALESCE((SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'bread'), 0)").Scan(&breadQty)
	_ = database.QueryRow("SELECT COALESCE((SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'rotting_waste'), 0)").Scan(&wasteQty)

	if breadQty != 0 {
		t.Errorf("bread quantity = %d, want 0 after spoilage transmutation", breadQty)
	}
	if wasteQty != 2 {
		t.Errorf("rotting_waste quantity = %d, want 2", wasteQty)
	}
}
