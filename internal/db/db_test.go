package db

import (
	"testing"
)

func TestInitDBAndSeed(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize in-memory DB: %v", err)
	}
	defer database.Close()

	// Verify locations were seeded
	locRows, err := database.Query("SELECT id, name, is_public_property FROM locations")
	if err != nil {
		t.Fatalf("Failed to query locations: %v", err)
	}
	defer locRows.Close()

	locCount := 0
	foundTownSquare := false
	for locRows.Next() {
		var id, name string
		var isPublic int
		if err := locRows.Scan(&id, &name, &isPublic); err != nil {
			t.Fatalf("Failed to scan location row: %v", err)
		}
		locCount++
		if id == "town_square" {
			foundTownSquare = true
		}
	}

	if locCount != 20 {
		t.Errorf("Expected 20 seeded locations, got %d", locCount)
	}
	if !foundTownSquare {
		t.Errorf("Expected to find town_square location")
	}

	// Verify characters were seeded
	charRows, err := database.Query("SELECT id, controller_type, gold FROM characters")
	if err != nil {
		t.Fatalf("Failed to query characters: %v", err)
	}
	defer charRows.Close()

	charCount := 0
	foundEldon := false
	for charRows.Next() {
		var id, controllerType string
		var gold int
		if err := charRows.Scan(&id, &controllerType, &gold); err != nil {
			t.Fatalf("Failed to scan character row: %v", err)
		}
		charCount++
		if id == "eldon_millwright" {
			foundEldon = true
			if controllerType != "agent" {
				t.Errorf("Expected eldon_millwright controller_type to be 'agent', got %s", controllerType)
			}
		}
	}

	if charCount != 5 {
		t.Errorf("Expected 5 seeded characters, got %d", charCount)
	}
	if !foundEldon {
		t.Errorf("Expected to find eldon_millwright character")
	}

	// Verify inventories were seeded
	var invCount int
	err = database.QueryRow("SELECT COUNT(*) FROM inventories").Scan(&invCount)
	if err != nil {
		t.Fatalf("Failed to count inventories: %v", err)
	}
	if invCount == 0 {
		t.Errorf("Expected inventories to be seeded, got 0 items")
	}
}

func TestSpatialGraphRouting(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize in-memory DB: %v", err)
	}
	defer database.Close()

	// Test direct hub connection (town_square to tavern -> 1 tick)
	ok, ticks, err := CanTravelLocation(database, "town_square", "tavern")
	if err != nil || !ok || ticks != 1 {
		t.Errorf("Expected direct connection town_square -> tavern with 1 tick, got ok=%v, ticks=%d, err=%v", ok, ticks, err)
	}

	// Test town_square to prairie (2 ticks)
	ok, ticks, err = CanTravelLocation(database, "town_square", "prairie")
	if err != nil || !ok || ticks != 2 {
		t.Errorf("Expected direct connection town_square -> prairie with 2 ticks, got ok=%v, ticks=%d, err=%v", ok, ticks, err)
	}

	// Test prairie to deep_forest (4 ticks)
	ok, ticks, err = CanTravelLocation(database, "prairie", "deep_forest")
	if err != nil || !ok || ticks != 4 {
		t.Errorf("Expected direct connection prairie -> deep_forest with 4 ticks, got ok=%v, ticks=%d, err=%v", ok, ticks, err)
	}

	// Test unreachable direct connection (town_mill to deep_forest without intermediate node)
	ok, _, err = CanTravelLocation(database, "town_mill", "deep_forest")
	if err != nil || ok {
		t.Errorf("Expected no direct connection town_mill -> deep_forest, got ok=%v", ok)
	}
}

func TestInitDBWithoutSeed(t *testing.T) {
	database, err := InitDB(":memory:", false)
	if err != nil {
		t.Fatalf("Failed to initialize in-memory DB without seed: %v", err)
	}
	defer database.Close()

	var locCount int
	err = database.QueryRow("SELECT COUNT(*) FROM locations").Scan(&locCount)
	if err != nil {
		t.Fatalf("Failed to count locations: %v", err)
	}
	if locCount != 0 {
		t.Errorf("Expected 0 locations when seeding is disabled, got %d", locCount)
	}

	// Now seed manually
	if err := SeedDB(database); err != nil {
		t.Fatalf("Failed to seed database manually: %v", err)
	}

	err = database.QueryRow("SELECT COUNT(*) FROM locations").Scan(&locCount)
	if err != nil {
		t.Fatalf("Failed to count locations after seeding: %v", err)
	}
	if locCount != 20 {
		t.Errorf("Expected 20 locations after manual seeding, got %d", locCount)
	}
}
