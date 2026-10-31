package server

import (
	"agentmud/internal/db"
	"testing"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

func TestDBAuthHook(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory db: %v", err)
	}
	defer database.Close()

	hook := NewDBAuthHook(database)

	// Test 1: Valid character credentials
	validPacket := packets.Packet{
		Connect: packets.ConnectParams{
			Username: []byte("eldon_millwright"),
			Password: []byte("secret_eldon_123"),
		},
	}
	client := &mqtt.Client{
		ID: "test_client_1",
		Properties: mqtt.ClientProperties{
			Username: []byte("eldon_millwright"),
		},
	}

	if !hook.OnConnectAuthenticate(client, validPacket) {
		t.Errorf("Expected valid credentials for eldon_millwright to pass authentication")
	}

	// Verify character is marked online
	var isOnline int
	err = database.QueryRow("SELECT is_online FROM characters WHERE id = ?", "eldon_millwright").Scan(&isOnline)
	if err != nil || isOnline != 1 {
		t.Errorf("Expected is_online to be 1, got %d (err: %v)", isOnline, err)
	}

	// Test 2: Invalid password
	invalidPassPacket := packets.Packet{
		Connect: packets.ConnectParams{
			Username: []byte("eldon_millwright"),
			Password: []byte("wrong_password"),
		},
	}
	if hook.OnConnectAuthenticate(client, invalidPassPacket) {
		t.Errorf("Expected wrong password to fail authentication")
	}

	// Test 3: Unknown character
	unknownUserPacket := packets.Packet{
		Connect: packets.ConnectParams{
			Username: []byte("nonexistent_user"),
			Password: []byte("some_pass"),
		},
	}
	if hook.OnConnectAuthenticate(client, unknownUserPacket) {
		t.Errorf("Expected unknown character to fail authentication")
	}

	// Test 4: Disconnect resets online status
	hook.OnDisconnect(client, nil, false)
	err = database.QueryRow("SELECT is_online FROM characters WHERE id = ?", "eldon_millwright").Scan(&isOnline)
	if err != nil || isOnline != 0 {
		t.Errorf("Expected is_online to be 0 after disconnect, got %d (err: %v)", isOnline, err)
	}

	// Test 5: ACL checks
	// Can publish to own intent
	if !hook.OnACLCheck(client, "world/agents/eldon_millwright/intent", true) {
		t.Errorf("Expected write access to own intent topic")
	}
	// Cannot publish to another agent's intent
	if hook.OnACLCheck(client, "world/agents/martha_barkeep/intent", true) {
		t.Errorf("Expected write access to another agent's intent topic to be denied")
	}
	// Can read global time
	if !hook.OnACLCheck(client, "world/global/time", false) {
		t.Errorf("Expected read access to world/global/time")
	}
}
