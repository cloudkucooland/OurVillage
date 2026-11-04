package admin

import (
	"testing"

	"agentmud/internal/db"
)

func TestAdminCommands(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	weatherUpdated := ""
	setWeather := func(w string) {
		weatherUpdated = w
	}

	adm := NewAdminCommandExecutor(database, setWeather)

	// 1. Test SetWeather
	if err := adm.SetWeather("rain"); err != nil {
		t.Errorf("expected no error setting weather, got %v", err)
	}
	if weatherUpdated != "rain" {
		t.Errorf("expected weatherUpdated to be 'rain', got %s", weatherUpdated)
	}

	if err := adm.SetWeather("hurricane"); err == nil {
		t.Errorf("expected error for invalid weather 'hurricane', got nil")
	}

	// 2. Test SetLocationDescription
	if err := adm.SetLocationDescription("tavern", "A cozy warm tavern."); err != nil {
		t.Errorf("failed to set location description: %v", err)
	}
	var desc string
	_ = database.QueryRow("SELECT description FROM locations WHERE id = 'tavern'").Scan(&desc)
	if desc != "A cozy warm tavern." {
		t.Errorf("expected 'A cozy warm tavern.', got %q", desc)
	}

	// 3. Test SetLocationOwner
	if err := adm.SetLocationOwner("tavern", "martha_barkeep"); err != nil {
		t.Errorf("failed to set location owner: %v", err)
	}
	var ownerID string
	_ = database.QueryRow("SELECT owner_id FROM locations WHERE id = 'tavern'").Scan(&ownerID)
	if ownerID != "martha_barkeep" {
		t.Errorf("expected 'martha_barkeep', got %q", ownerID)
	}

	// 4. Test AddItemToCharacter & RemoveItemFromCharacter
	if err := adm.AddItemToCharacter("eldon_millwright", "bread", 5); err != nil {
		t.Errorf("failed to add item: %v", err)
	}
	var qty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'bread'").Scan(&qty)
	if qty != 5 {
		t.Errorf("expected 5 bread, got %d", qty)
	}

	if err := adm.RemoveItemFromCharacter("eldon_millwright", "bread", 2); err != nil {
		t.Errorf("failed to remove item: %v", err)
	}
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'bread'").Scan(&qty)
	if qty != 3 {
		t.Errorf("expected 3 bread remaining, got %d", qty)
	}

	// 5. Test SetCharacterGold
	if err := adm.SetCharacterGold("eldon_millwright", 125); err != nil {
		t.Errorf("failed to set gold: %v", err)
	}
	var gold int
	_ = database.QueryRow("SELECT gold FROM characters WHERE id = 'eldon_millwright'").Scan(&gold)
	if gold != 125 {
		t.Errorf("expected 125 gold, got %d", gold)
	}
}
