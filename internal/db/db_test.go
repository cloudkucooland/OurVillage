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

	if locCount != 15 {
		t.Errorf("Expected 15 seeded locations, got %d", locCount)
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
