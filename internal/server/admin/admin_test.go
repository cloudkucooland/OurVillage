package admin

import (
	"database/sql"
	"strings"
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
	for _, weather := range []string{"clear", "rain", "snow", "storm", "fog", "cloudy", "hail", "windy", "heatwave", "drought", "frost"} {
		if err := adm.SetWeather(weather); err != nil {
			t.Errorf("expected no error setting weather %q, got %v", weather, err)
		}
		if weatherUpdated != weather {
			t.Errorf("expected weatherUpdated to be %q, got %q", weather, weatherUpdated)
		}
	}
	if err := adm.SetWeather("  CLOUDY "); err != nil || weatherUpdated != "cloudy" {
		t.Errorf("expected weather to be normalized to cloudy, got %q (err: %v)", weatherUpdated, err)
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
	if err := adm.SetLocationPublic("tavern", true); err != nil {
		t.Fatalf("failed to set public visibility: %v", err)
	}
	if err := adm.SetLocationIndoor("tavern", false); err != nil {
		t.Fatalf("failed to set outdoor status: %v", err)
	}
	if err := adm.SetLocationAccess("tavern", "foot_only"); err != nil {
		t.Fatalf("failed to set access tier: %v", err)
	}
	if err := adm.SetLocationHours("tavern", "20:00", "04:30"); err != nil {
		t.Fatalf("failed to set overnight hours: %v", err)
	}
	if err := adm.SetLocationRules("tavern", "Quiet after closing."); err != nil {
		t.Fatalf("failed to set location rules: %v", err)
	}
	var tavernPublic, tavernIndoor bool
	var accessTier, openTime, closeTime, rules string
	if err := database.QueryRow(`
		SELECT is_public_property, is_indoor, vehicle_access_tier, open_time, close_time, rules_prompt
		FROM locations WHERE id = 'tavern'
	`).Scan(&tavernPublic, &tavernIndoor, &accessTier, &openTime, &closeTime, &rules); err != nil {
		t.Fatalf("failed to read location settings: %v", err)
	}
	if !tavernPublic || tavernIndoor || accessTier != "FOOT_ONLY" ||
		openTime != "20:00" || closeTime != "04:30" || rules != "Quiet after closing." {
		t.Errorf("unexpected location settings: public=%v indoor=%v access=%s hours=%s-%s rules=%q",
			tavernPublic, tavernIndoor, accessTier, openTime, closeTime, rules)
	}
	if err := adm.SetLocationOwner("tavern", ""); err != nil {
		t.Fatalf("failed to clear location owner: %v", err)
	}
	var owner sql.NullString
	if err := database.QueryRow("SELECT owner_id FROM locations WHERE id = 'tavern'").Scan(&owner); err != nil {
		t.Fatalf("failed to read cleared owner: %v", err)
	}
	if owner.Valid {
		t.Errorf("expected owner to be cleared, got %q", owner.String)
	}
	if err := adm.SetLocationAccess("tavern", "HORSE_ONLY"); err == nil {
		t.Error("expected invalid access tier to be rejected")
	}
	if err := adm.SetLocationHours("tavern", "25:00", "04:00"); err == nil {
		t.Error("expected invalid opening time to be rejected")
	}
	if err := adm.SetLocationRules("missing_location", "Rules."); err == nil {
		t.Error("expected rules update on missing location to fail")
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

	if err := adm.SetLocationResource("prairie", "wild_grain", 32, 90, 12); err != nil {
		t.Fatalf("failed to configure resource pool: %v", err)
	}
	var currentStock, maxStock, replenishRate int
	if err := database.QueryRow(`
		SELECT current_stock, max_stock, replenish_rate_per_day
		FROM location_resources WHERE location_id = 'prairie' AND resource_id = 'wild_grain'
	`).Scan(&currentStock, &maxStock, &replenishRate); err != nil {
		t.Fatalf("failed to read configured resource pool: %v", err)
	}
	if currentStock != 32 || maxStock != 90 || replenishRate != 12 {
		t.Errorf("unexpected resource pool values: %d/%d replenishes %d", currentStock, maxStock, replenishRate)
	}
	if err := adm.SetLocationResource("prairie", "wild_grain", 5, 40, 4); err != nil {
		t.Fatalf("failed to update resource pool: %v", err)
	}
	if err := database.QueryRow(`
		SELECT current_stock, max_stock, replenish_rate_per_day
		FROM location_resources WHERE location_id = 'prairie' AND resource_id = 'wild_grain'
	`).Scan(&currentStock, &maxStock, &replenishRate); err != nil {
		t.Fatalf("failed to read updated resource pool: %v", err)
	}
	if currentStock != 5 || maxStock != 40 || replenishRate != 4 {
		t.Errorf("unexpected updated resource pool values: %d/%d replenishes %d", currentStock, maxStock, replenishRate)
	}
	if err := adm.SetLocationResource("prairie", "wild_grain", 41, 40, 4); err == nil {
		t.Error("expected current stock above max stock to be rejected")
	}
	if err := adm.SetLocationResource("prairie", "missing_item", 1, 2, 1); err == nil {
		t.Error("expected resource without a matching item definition to be rejected")
	}

	if err := adm.SetShopItem("bakery", "bread", 3, 18); err != nil {
		t.Fatalf("failed to configure shop listing: %v", err)
	}
	var priceGold, shopStock int
	if err := database.QueryRow(`
		SELECT price_gold, stock_quantity FROM location_shop_items
		WHERE location_id = 'bakery' AND item_id = 'bread'
	`).Scan(&priceGold, &shopStock); err != nil {
		t.Fatalf("failed to read configured shop listing: %v", err)
	}
	if priceGold != 3 || shopStock != 18 {
		t.Errorf("unexpected shop listing values: %d gold, stock %d", priceGold, shopStock)
	}
	if err := adm.SetShopItem("bakery", "bread", 4, 0); err != nil {
		t.Fatalf("failed to set out-of-stock listing: %v", err)
	}
	if err := database.QueryRow(`
		SELECT price_gold, stock_quantity FROM location_shop_items
		WHERE location_id = 'bakery' AND item_id = 'bread'
	`).Scan(&priceGold, &shopStock); err != nil {
		t.Fatalf("failed to read updated shop listing: %v", err)
	}
	if priceGold != 4 || shopStock != 0 {
		t.Errorf("unexpected updated shop listing values: %d gold, stock %d", priceGold, shopStock)
	}
	if err := adm.SetShopItem("bakery", "bread", -1, 1); err == nil {
		t.Error("expected negative price to be rejected")
	}

	if err := adm.CreateItem(ItemDefinition{
		ID: "test_item", Name: "Test Item", WeightKg: 1.25, Description: "A new item.",
	}); err != nil {
		t.Fatalf("failed to create item: %v", err)
	}
	if err := adm.CreateItem(ItemDefinition{
		ID: "bad/item", Name: "Bad Item", WeightKg: 1, Description: "An invalid id.",
	}); err == nil {
		t.Error("expected unsafe item id to be rejected")
	}
	if err := adm.CreateItem(ItemDefinition{
		ID: "bad_weight", Name: "Bad Weight", WeightKg: -1, Description: "An invalid weight.",
	}); err == nil {
		t.Error("expected negative item weight to be rejected")
	}
	var itemName string
	if err := database.QueryRow("SELECT name FROM items WHERE id = 'test_item'").Scan(&itemName); err != nil || itemName != "Test Item" {
		t.Errorf("created item missing or incorrect: name=%q err=%v", itemName, err)
	}
	if err := adm.CreateItem(ItemDefinition{
		ID: "test_food", Name: "Test Food", WeightKg: 0.4, Description: "Restores hunger.",
		IsConsumable: true, HungerRestore: 20,
	}); err != nil {
		t.Fatalf("failed to create consumable item: %v", err)
	}
	if err := adm.CreateItem(ItemDefinition{
		ID: "test_bag", Name: "Test Bag", WeightKg: 0.7, Description: "Carries items.",
		IsContainer: true, CapacityKg: 12.5, SlotLimit: 6,
	}); err != nil {
		t.Fatalf("failed to create container item: %v", err)
	}
	var isConsumable, hungerRestore, isContainer, slotLimit int
	var capacityKg float64
	if err := database.QueryRow(`
		SELECT is_consumable, hunger_restore, is_container, capacity_kg, slot_limit
		FROM items WHERE id = 'test_food'
	`).Scan(&isConsumable, &hungerRestore, &isContainer, &capacityKg, &slotLimit); err != nil {
		t.Fatalf("failed to load consumable fields: %v", err)
	}
	if isConsumable != 1 || hungerRestore != 20 || isContainer != 0 || capacityKg != 0 || slotLimit != 0 {
		t.Errorf("unexpected consumable properties: %d %d %d %.1f %d", isConsumable, hungerRestore, isContainer, capacityKg, slotLimit)
	}
	if err := database.QueryRow(`
		SELECT is_consumable, hunger_restore, is_container, capacity_kg, slot_limit
		FROM items WHERE id = 'test_bag'
	`).Scan(&isConsumable, &hungerRestore, &isContainer, &capacityKg, &slotLimit); err != nil {
		t.Fatalf("failed to load container fields: %v", err)
	}
	if isConsumable != 0 || hungerRestore != 0 || isContainer != 1 || capacityKg != 12.5 || slotLimit != 6 {
		t.Errorf("unexpected container properties: %d %d %d %.1f %d", isConsumable, hungerRestore, isContainer, capacityKg, slotLimit)
	}
	if err := adm.CreateItem(ItemDefinition{
		ID: "bad_food", Name: "Bad Food", WeightKg: 1, Description: "Invalid properties.",
		HungerRestore: 10,
	}); err == nil {
		t.Error("expected hunger restore on a non-consumable to be rejected")
	}
	if err := adm.CreateItem(ItemDefinition{
		ID: "bad_container", Name: "Bad Container", WeightKg: 1, Description: "Invalid properties.",
		IsContainer: true, CapacityKg: 0, SlotLimit: 2,
	}); err == nil {
		t.Error("expected container without positive capacity to be rejected")
	}

	if err := adm.CreateLocation("test_room", "Test Room", "A test room."); err != nil {
		t.Fatalf("failed to create location: %v", err)
	}
	if err := adm.CreateLocation("bad/room", "Bad Room", "An invalid id."); err == nil {
		t.Error("expected unsafe location id to be rejected")
	}
	var isPublic bool
	if err := database.QueryRow("SELECT is_public_property FROM locations WHERE id = 'test_room'").Scan(&isPublic); err != nil || !isPublic {
		t.Errorf("expected new location to be public: is_public=%v err=%v", isPublic, err)
	}
	if err := adm.ConnectLocations("town_square", "test_room", 3); err != nil {
		t.Fatalf("failed to connect locations: %v", err)
	}
	var reverseTicks int
	if err := database.QueryRow("SELECT travel_ticks FROM location_connections WHERE from_location_id = 'test_room' AND to_location_id = 'town_square'").Scan(&reverseTicks); err != nil || reverseTicks != 3 {
		t.Errorf("expected reverse connection with 3 ticks, got %d (err: %v)", reverseTicks, err)
	}

	password, err := adm.CreateCharacter("test_character", "Test Character", "Tester", "test_room", "A careful tester.")
	if err != nil {
		t.Fatalf("failed to create character: %v", err)
	}
	var storedPassword string
	if err := database.QueryRow("SELECT mqtt_password FROM characters WHERE id = 'test_character'").Scan(&storedPassword); err != nil || password == "" || storedPassword != password {
		t.Errorf("generated character password not stored correctly (err: %v)", err)
	}
	if len(password) < 40 || strings.ContainsAny(password, "+/=") {
		t.Errorf("expected a URL-safe generated password, got %q", password)
	}
	if _, err := adm.CreateCharacter("bad_character", "Bad Character", "Tester", "missing_location", "Persona"); err == nil {
		t.Error("expected character creation at a missing location to fail")
	}
	if err := adm.ConnectLocations("town_square", "missing_location", 1); err == nil {
		t.Error("expected connecting to a missing location to fail")
	}
}
