package server

import (
	"encoding/json"
	"testing"

	"agentmud/internal/db"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

func TestLookIntentAndMoveStateSync(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to init in-memory db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	directChan := make(chan []byte, 10)
	_ = srv.mqttServer.Subscribe("world/agents/eldon_millwright/direct", 0, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		directChan <- pk.Payload
	})

	// 1. Dispatch LOOK
	srv.handleLook("eldon_millwright", "req_look_101")

	select {
	case payload := <-directChan:
		var resp struct {
			IntentID      string `json:"intent_id"`
			Status        string `json:"status"`
			Type          string `json:"type"`
			PrivateStatus struct {
				ID              string `json:"id"`
				Gold            int    `json:"gold"`
				CurrentLocation string `json:"current_location"`
			} `json:"private_status"`
			Location struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"location"`
		}
		if err := json.Unmarshal(payload, &resp); err != nil {
			t.Fatalf("failed to decode LOOK response: %v", err)
		}
		if resp.IntentID != "req_look_101" {
			t.Errorf("expected intent_id req_look_101, got %s", resp.IntentID)
		}
		if resp.Status != "SUCCESS" || resp.Type != "STATE_SYNC" {
			t.Errorf("expected SUCCESS and STATE_SYNC, got %s / %s", resp.Status, resp.Type)
		}
		if resp.PrivateStatus.ID != "eldon_millwright" {
			t.Errorf("expected eldon_millwright, got %s", resp.PrivateStatus.ID)
		}
		if resp.PrivateStatus.CurrentLocation != "town_mill" {
			t.Errorf("expected town_mill, got %s", resp.PrivateStatus.CurrentLocation)
		}
		if resp.Location.ID != "town_mill" {
			t.Errorf("expected town_mill, got %s", resp.Location.ID)
		}
		if resp.Location.Name != "The Town Mill" {
			t.Errorf("expected The Town Mill, got %s", resp.Location.Name)
		}
	default:
		t.Fatal("expected message on directChan for LOOK")
	}

	// 2. Dispatch MOVE (should transition to tavern and receive state sync of The Boar's Tusk Tavern)
	movePayload, _ := json.Marshal(MovePayload{TargetLocation: "tavern"})
	srv.handleMove("eldon_millwright", "req_move_202", movePayload)

	select {
	case payload := <-directChan:
		var resp struct {
			IntentID      string `json:"intent_id"`
			Status        string `json:"status"`
			Type          string `json:"type"`
			PrivateStatus struct {
				CurrentLocation string `json:"current_location"`
			} `json:"private_status"`
			Location struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"location"`
		}
		if err := json.Unmarshal(payload, &resp); err != nil {
			t.Fatalf("failed to decode MOVE response: %v", err)
		}
		if resp.IntentID != "req_move_202" {
			t.Errorf("expected intent_id req_move_202, got %s", resp.IntentID)
		}
		if resp.Status != "SUCCESS" || resp.Type != "STATE_SYNC" {
			t.Errorf("expected SUCCESS and STATE_SYNC, got %s / %s", resp.Status, resp.Type)
		}
		if resp.PrivateStatus.CurrentLocation != "tavern" {
			t.Errorf("expected new location tavern, got %s", resp.PrivateStatus.CurrentLocation)
		}
		if resp.Location.ID != "tavern" {
			t.Errorf("expected location ID tavern, got %s", resp.Location.ID)
		}
		if resp.Location.Name != "The Boar's Tusk Tavern" {
			t.Errorf("expected The Boar's Tusk Tavern, got %s", resp.Location.Name)
		}
	default:
		t.Fatal("expected message on directChan for MOVE")
	}
}

func TestTransferNotification(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to init in-memory db: %v", err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	srv.SetTickCount(48)

	_, _ = database.Exec("UPDATE characters SET is_online = 1, current_location = 'town_mill' WHERE id IN ('eldon_millwright', 'martha_barkeep')")

	marthaDirectChan := make(chan []byte, 10)
	_ = srv.mqttServer.Subscribe("world/agents/martha_barkeep/direct", 0, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		marthaDirectChan <- pk.Payload
	})

	sysChan := make(chan []byte, 10)
	_ = srv.mqttServer.Subscribe("world/locations/town_mill/system", 0, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		sysChan <- pk.Payload
	})

	transferPayload, _ := json.Marshal(TransferPayload{
		RecipientID: "martha_barkeep",
		Amount:      5,
	})

	srv.handleTransfer("eldon_millwright", "req_transfer_1", transferPayload)

	select {
	case payload := <-marthaDirectChan:
		var resp struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(payload, &resp); err != nil {
			t.Fatalf("failed to decode martha direct response: %v", err)
		}
		if resp.Message == "" {
			t.Errorf("expected non-empty notification message for martha")
		}
	default:
		t.Fatal("expected message on marthaDirectChan for transfer")
	}

	select {
	case payload := <-sysChan:
		sysMsg := string(payload)
		if sysMsg == "" {
			t.Errorf("expected system message broadcast")
		}
	default:
		t.Fatal("expected message on sysChan for room system broadcast")
	}
}
