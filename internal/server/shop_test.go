package server

import (
	"encoding/json"
	"testing"
	"time"

	"agentmud/internal/db"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

func TestSellIntentPaysSellerAndReturnsStateSync(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		UPDATE characters SET current_location = 'town_mill', gold = 0
		WHERE id = 'luke_farmer'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity)
		VALUES ('luke_farmer', 'wheat', 1)
		ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = excluded.quantity
	`); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatal(err)
	}
	directMessages := make(chan []byte, 1)
	if err := srv.mqttServer.Subscribe("world/agents/luke_farmer/direct", 0, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		directMessages <- pk.Payload
	}); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(SellPayload{ItemID: "wheat", Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	srv.handleSell("luke_farmer", "req_sell_wheat", payload)
	select {
	case response := <-directMessages:
		assertCharacterStatusSync(t, response, "IDLE")
	case <-time.After(time.Second):
		t.Fatal("expected sale state sync")
	}

	var farmerGold, millwrightGold int
	if err := database.QueryRow(`SELECT gold FROM characters WHERE id = 'luke_farmer'`).Scan(&farmerGold); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT gold FROM characters WHERE id = 'eldon_millwright'`).Scan(&millwrightGold); err != nil {
		t.Fatal(err)
	}
	if farmerGold != 1 || millwrightGold != 14 {
		t.Errorf("sale balances: farmer=%d millwright=%d, want farmer=1 millwright=14", farmerGold, millwrightGold)
	}
}
