package db

import (
	"path/filepath"
	"testing"
)

func TestGetCharacterPerception(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_perception.db")
	database, err := InitDB(tempDB)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	perception, err := GetCharacterPerception(database, "eldon_millwright")
	if err != nil {
		t.Fatalf("failed to get character perception: %v", err)
	}

	if perception.Type != "STATE_SYNC" {
		t.Errorf("expected type STATE_SYNC, got %s", perception.Type)
	}
	if perception.Character.ID != "eldon_millwright" {
		t.Errorf("expected eldon_millwright, got %s", perception.Character.ID)
	}
	if perception.Character.Gold != 15 {
		t.Errorf("expected 15 gold, got %d", perception.Character.Gold)
	}
	if perception.Character.CurrentLocation != "town_mill" {
		t.Errorf("expected town_mill, got %s", perception.Character.CurrentLocation)
	}
	if len(perception.Character.Inventory) == 0 {
		t.Error("expected non-empty inventory for Eldon")
	}
	if perception.Location.ID != "town_mill" {
		t.Errorf("expected location ID town_mill, got %s", perception.Location.ID)
	}
	if perception.Location.Name != "The Town Mill" {
		t.Errorf("expected location name The Town Mill, got %s", perception.Location.Name)
	}
	if len(perception.Location.Workstations) == 0 {
		t.Error("expected workstation in town mill (timber_saw)")
	}

	// Test non-existent character
	_, err = GetCharacterPerception(database, "nonexistent_character")
	if err == nil {
		t.Fatal("expected error for nonexistent character, got nil")
	}
}
