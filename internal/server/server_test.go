package server

import (
	"os"
	"testing"

	"agentmud/internal/db"
)

func TestServerTickResumption(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize DB: %v", err)
	}
	defer database.Close()

	// Pre-save tick 50 in db
	if err := db.SaveServerTick(database, 50); err != nil {
		t.Fatalf("Failed to save server tick: %v", err)
	}

	// Create server - should resume from tick 50
	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	defer srv.Shutdown()

	if srv.GetTickCount() != 50 {
		t.Errorf("Expected server tick to resume at 50, got %d", srv.GetTickCount())
	}

	// Call ProcessTick - should increment tick to 51 and save 51 to DB
	srv.ProcessTick()

	if srv.GetTickCount() != 51 {
		t.Errorf("Expected server tick after ProcessTick to be 51, got %d", srv.GetTickCount())
	}

	savedTick, err := db.GetServerTick(database)
	if err != nil {
		t.Fatalf("Failed to get saved server tick: %v", err)
	}
	if savedTick != 51 {
		t.Errorf("Expected saved tick in DB to be 51, got %d", savedTick)
	}
}

func TestTemperatureHealthDecay(t *testing.T) {
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

	srv.SetTemperature(0)
	if got := srv.GetTemperature(); got != 0 {
		t.Fatalf("GetTemperature() = %v after setting zero, want 0", got)
	}

	// Set character health to 100 and online
	_, err = database.Exec("UPDATE characters SET health = 100, is_online = 1, current_location = 'town_square'")
	if err != nil {
		t.Fatalf("Failed to update character health: %v", err)
	}

	// Make town_square outdoor (is_indoor = 0)
	_, _ = database.Exec("UPDATE locations SET is_indoor = 0 WHERE id = 'town_square'")

	// Set temperature to 5.0 (too cold < 10)
	srv.SetTemperature(5.0)

	// Process tick -> outdoor character health should decay from 100 to 99
	srv.ProcessTick()

	var health int
	_ = database.QueryRow("SELECT health FROM characters LIMIT 1").Scan(&health)
	if health != 99 {
		t.Errorf("Expected outdoor character health to decay to 99 in cold temperature, got %d", health)
	}

	// Now move character to an indoor location (is_indoor = 1)
	_, _ = database.Exec("UPDATE locations SET is_indoor = 1 WHERE id = 'tavern'")
	_, _ = database.Exec("UPDATE characters SET current_location = 'tavern', health = 100")

	// Process tick -> indoor character health should remain 100
	srv.ProcessTick()

	_ = database.QueryRow("SELECT health FROM characters LIMIT 1").Scan(&health)
	if health != 100 {
		t.Errorf("Expected indoor character health to remain 100 in cold temperature, got %d", health)
	}
}

func TestTemperatureHealthDecayThresholds(t *testing.T) {
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

	if _, err := database.Exec(`
		UPDATE locations SET is_indoor = 0 WHERE id = 'town_square';
		UPDATE characters SET health = 100, hunger = 100, is_online = 1, current_location = 'town_square'
		WHERE id = 'eldon_millwright';
	`); err != nil {
		t.Fatalf("Failed to set outdoor character state: %v", err)
	}

	for _, tt := range []struct {
		temperature float64
		wantHealth  int
	}{
		{temperature: 9.9, wantHealth: 99},
		{temperature: 10, wantHealth: 100},
		{temperature: 30, wantHealth: 100},
		{temperature: 30.1, wantHealth: 99},
	} {
		srv.SetTemperature(tt.temperature)
		if _, err := database.Exec("UPDATE characters SET health = 100 WHERE id = 'eldon_millwright'"); err != nil {
			t.Fatalf("Failed to reset health: %v", err)
		}
		srv.ProcessTick()

		var health int
		if err := database.QueryRow("SELECT health FROM characters WHERE id = 'eldon_millwright'").Scan(&health); err != nil {
			t.Fatalf("Failed to read health: %v", err)
		}
		if health != tt.wantHealth {
			t.Errorf("health at %.1f°C = %d, want %d", tt.temperature, health, tt.wantHealth)
		}
	}
}

func TestDepletedCharactersAreForcedToRest(t *testing.T) {
	tests := []struct {
		name         string
		energy       int
		health       int
		wantDuration uint64
	}{
		{name: "zero energy", energy: 0, health: 100, wantDuration: 30},
		{name: "zero health", energy: 100, health: 0, wantDuration: 60},
		{name: "zero energy and health uses health duration", energy: 0, health: 0, wantDuration: 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			if _, err := database.Exec(`
				UPDATE characters
				SET hunger = 100, energy = ?, health = ?, is_online = 1
				WHERE id = 'eldon_millwright'
			`, tt.energy, tt.health); err != nil {
				t.Fatalf("Failed to set character stats: %v", err)
			}

			srv.SetTickCount(10)
			srv.ProcessTick()

			var status string
			var startTick, endTick uint64
			err = database.QueryRow(`
				SELECT status, action_start_tick, action_end_tick
				FROM characters WHERE id = 'eldon_millwright'
			`).Scan(&status, &startTick, &endTick)
			if err != nil {
				t.Fatalf("Failed to read character action: %v", err)
			}
			if status != "RESTING" {
				t.Fatalf("status = %q, want RESTING", status)
			}
			if duration := endTick - startTick; duration != tt.wantDuration {
				t.Errorf("rest duration = %d ticks, want %d", duration, tt.wantDuration)
			}
			if startTick != 11 {
				t.Errorf("rest start tick = %d, want 11", startTick)
			}
		})
	}
}

func TestZeroHungerCollapseTriggersEmergencyRecovery(t *testing.T) {
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

	if _, err := database.Exec(`
		UPDATE characters
		SET hunger = 0, energy = 100, health = 1, is_online = 1
		WHERE id = 'eldon_millwright'
	`); err != nil {
		t.Fatalf("Failed to set character stats: %v", err)
	}

	srv.SetTickCount(10)
	srv.ProcessTick()

	var health, hunger int
	var location string
	var status string
	var startTick, endTick uint64
	err = database.QueryRow(`
		SELECT health, hunger, current_location, status, action_start_tick, action_end_tick
		FROM characters WHERE id = 'eldon_millwright'
	`).Scan(&health, &hunger, &location, &status, &startTick, &endTick)
	if err != nil {
		t.Fatalf("Failed to read character state: %v", err)
	}
	if health != 100 || hunger != 100 {
		t.Errorf("health/hunger after collapse recovery = %d/%d, want 100/100", health, hunger)
	}
	if status != "RESTING" {
		t.Fatalf("status = %q after hunger depletion, want RESTING", status)
	}
	if location != "eldons_room" {
		t.Errorf("location after collapse recovery = %q, want eldons_room", location)
	}
	if duration := endTick - startTick; duration != collapseRecoveryRestDurationTicks {
		t.Errorf("rest duration = %d ticks, want %d", duration, collapseRecoveryRestDurationTicks)
	}
}

func TestEnsureDefaultCertificateAndTLS(t *testing.T) {
	certFile := "test_server.crt"
	keyFile := "test_server.key"
	defer os.Remove(certFile)
	defer os.Remove(keyFile)

	if err := EnsureDefaultCertificate(certFile, keyFile); err != nil {
		t.Fatalf("EnsureDefaultCertificate failed: %v", err)
	}

	if _, err := os.Stat(certFile); err != nil {
		t.Errorf("Expected cert file %s to exist: %v", certFile, err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Errorf("Expected key file %s to exist: %v", keyFile, err)
	}

	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize DB: %v", err)
	}
	defer database.Close()

	cfg := DefaultConfig()
	cfg.Server.MQTTAddr = ""
	cfg.Server.MQTTTLSAddr = "127.0.0.1:0"
	cfg.Server.TLSCert = certFile
	cfg.Server.TLSKey = keyFile

	srv, err := NewServerWithConfig(database, cfg, "")
	if err != nil {
		t.Fatalf("Failed to create server with TLS config: %v", err)
	}
	defer srv.Shutdown()
}

func TestOfflineCharactersDoNotDecay(t *testing.T) {
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

	// Set up two characters: one online at home, one offline
	_, err = database.Exec(`
		UPDATE characters SET hunger = 50, energy = 50, status = 'IDLE', current_location = home_location_id WHERE id = 'eldon_millwright';
		UPDATE characters SET hunger = 50, energy = 50, status = 'IDLE' WHERE id = 'martha_barkeep';
		UPDATE characters SET is_online = 1 WHERE id = 'eldon_millwright';
		UPDATE characters SET is_online = 0 WHERE id = 'martha_barkeep';
	`)
	if err != nil {
		t.Fatalf("Failed to setup test character states: %v", err)
	}

	srv.ProcessTick()

	var eldonHunger, eldonEnergy int
	var marthaHunger, marthaEnergy int

	if err := database.QueryRow("SELECT hunger, energy FROM characters WHERE id = 'eldon_millwright'").Scan(&eldonHunger, &eldonEnergy); err != nil {
		t.Fatalf("Failed to query eldon: %v", err)
	}
	if err := database.QueryRow("SELECT hunger, energy FROM characters WHERE id = 'martha_barkeep'").Scan(&marthaHunger, &marthaEnergy); err != nil {
		t.Fatalf("Failed to query martha: %v", err)
	}

	// Online character at home (Eldon) should decay hunger to 49, but retain energy at 50
	if eldonHunger != 49 || eldonEnergy != 50 {
		t.Errorf("Expected online character Eldon at home to decay hunger to 49 and retain energy 50, got %d/%d", eldonHunger, eldonEnergy)
	}

	// Offline character (Martha) should NOT decay (remain 50/50)
	if marthaHunger != 50 || marthaEnergy != 50 {
		t.Errorf("Expected offline character Martha to retain 50/50 hunger/energy, got %d/%d", marthaHunger, marthaEnergy)
	}
}

func TestAwayFromHomeEnergyDecay(t *testing.T) {
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

	_, err = database.Exec(`
		UPDATE characters SET hunger = 50, energy = 50, status = 'IDLE', current_location = 'tavern' WHERE id = 'eldon_millwright';
		UPDATE characters SET is_online = 1 WHERE id = 'eldon_millwright';
	`)
	if err != nil {
		t.Fatalf("Failed to setup test character states: %v", err)
	}

	srv.ProcessTick()

	var hunger, energy int
	if err := database.QueryRow("SELECT hunger, energy FROM characters WHERE id = 'eldon_millwright'").Scan(&hunger, &energy); err != nil {
		t.Fatalf("Failed to query eldon: %v", err)
	}

	if hunger != 49 || energy != 49 {
		t.Errorf("Expected online character away from home to decay hunger and energy to 49/49, got %d/%d", hunger, energy)
	}
}
