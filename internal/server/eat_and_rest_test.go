package server

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"agentmud/internal/db"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
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
	directChan := make(chan []byte, 2)
	if err := srv.mqttServer.Subscribe("world/agents/eldon_millwright/direct", 0, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		directChan <- pk.Payload
	}); err != nil {
		t.Fatal(err)
	}

	// Set Eldon's energy low
	_, err = database.Exec("UPDATE characters SET energy = 40 WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to set energy: %v", err)
	}

	// Submit REST intent for 30 ticks
	restPayload, _ := json.Marshal(map[string]int{"duration_ticks": 30})
	srv.handleRest("eldon_millwright", "req_rest_1", restPayload)
	assertCharacterStatusSync(t, <-directChan, "RESTING")

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
	assertCharacterStatusSync(t, <-directChan, "IDLE")

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

func assertCharacterStatusSync(t *testing.T, payload []byte, wantStatus string) {
	t.Helper()
	var response struct {
		Type          string `json:"type"`
		PrivateStatus struct {
			Status string `json:"status"`
		} `json:"private_status"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("failed to decode rest state sync: %v", err)
	}
	if response.Type != "STATE_SYNC" || response.PrivateStatus.Status != wantStatus {
		t.Fatalf("response sync = type %q, status %q; want STATE_SYNC / %s", response.Type, response.PrivateStatus.Status, wantStatus)
	}
}

func TestHerbalMedicineConsumableAndRestHealthRecovery(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Reduce Eldon's health to 60
	_, err = database.Exec("UPDATE characters SET health = 60 WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to set health: %v", err)
	}
	_, err = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'willowbark_tea', 1)")
	if err != nil {
		t.Fatalf("Failed to insert willowbark_tea: %v", err)
	}

	// Test consuming willowbark_tea (restores 20 health)
	teaPayload, _ := json.Marshal(ItemActionPayload{ItemID: "willowbark_tea", Quantity: 1})
	srv.handleEat("eldon_millwright", "req_eat_tea", teaPayload)

	var health int
	_ = database.QueryRow("SELECT health FROM characters WHERE id = 'eldon_millwright'").Scan(&health)
	if health != 80 { // 60 + 20
		t.Errorf("Expected health to be 80 after drinking willowbark tea, got %d", health)
	}

	// Test resting health recovery (+15 health)
	_, err = database.Exec("UPDATE characters SET energy = 50, health = 70 WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to reset stats: %v", err)
	}

	restPayload, _ := json.Marshal(map[string]int{"duration_ticks": 10})
	srv.handleRest("eldon_millwright", "req_rest_health", restPayload)

	var startTick, endTick uint64
	_ = database.QueryRow("SELECT action_start_tick, action_end_tick FROM characters WHERE id = 'eldon_millwright'").Scan(&startTick, &endTick)
	srv.SetTickCount(endTick)
	srv.ProcessTick()

	_ = database.QueryRow("SELECT health FROM characters WHERE id = 'eldon_millwright'").Scan(&health)
	if health != 85 { // 70 + 15
		t.Errorf("Expected health to be 85 after resting, got %d", health)
	}
}

func TestEatReportsActualCappedRecovery(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE characters SET hunger = 98, health = 95 WHERE id = 'eldon_millwright';
		INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'willowbark_tea', 1)
	`); err != nil {
		t.Fatal(err)
	}

	directChan := make(chan []byte, 1)
	if err := srv.mqttServer.Subscribe("world/agents/eldon_millwright/direct", 0, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		directChan <- pk.Payload
	}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(ItemActionPayload{ItemID: "willowbark_tea", Quantity: 1})
	srv.handleEat("eldon_millwright", "req_eat_capped", payload)

	var response struct {
		HungerRestored int    `json:"hunger_restored"`
		HealthRestored int    `json:"health_restored"`
		Narrative      string `json:"narrative_log"`
	}
	if err := json.Unmarshal(<-directChan, &response); err != nil {
		t.Fatal(err)
	}
	if response.HungerRestored != 2 || response.HealthRestored != 5 {
		t.Fatalf("reported recovery = hunger %d, health %d; want 2 and 5", response.HungerRestored, response.HealthRestored)
	}
	if !strings.Contains(response.Narrative, "Hunger +2, Health +5") {
		t.Fatalf("narrative does not report actual capped recovery: %q", response.Narrative)
	}
}

func TestEatConcurrentRequestsCannotConsumeSameMedicineTwice(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE characters SET health = 10 WHERE id = 'eldon_millwright';
		INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'willowbark_tea', 1)
	`); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(ItemActionPayload{ItemID: "willowbark_tea", Quantity: 1})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.handleEat("eldon_millwright", "req_eat_race", payload)
		}()
	}
	wg.Wait()

	var health int
	if err := database.QueryRow("SELECT health FROM characters WHERE id = 'eldon_millwright'").Scan(&health); err != nil {
		t.Fatal(err)
	}
	if health != 30 {
		t.Fatalf("health after two requests for one medicine = %d, want 30", health)
	}
	var inventoryQuantity int
	if err := database.QueryRow("SELECT COALESCE((SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'willowbark_tea'), 0)").Scan(&inventoryQuantity); err != nil {
		t.Fatal(err)
	}
	if inventoryQuantity != 0 {
		t.Fatalf("medicine inventory quantity = %d, want 0 after one consumption", inventoryQuantity)
	}
}

func TestRestEnforcesMinimumDuration(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]int{"duration_ticks": 1})
	srv.handleRest("eldon_millwright", "req_rest_minimum", payload)

	var startTick, endTick uint64
	if err := database.QueryRow("SELECT action_start_tick, action_end_tick FROM characters WHERE id = 'eldon_millwright'").Scan(&startTick, &endTick); err != nil {
		t.Fatal(err)
	}
	if duration := endTick - startTick; duration < minimumRestDurationTicks {
		t.Fatalf("rest duration = %d ticks, want at least %d", duration, minimumRestDurationTicks)
	}
}
