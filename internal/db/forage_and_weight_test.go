package db

import (
	"testing"
)

func TestWeightAndEncumbrance(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	// 1. Check baseline unassisted weight for Eldon
	wInfo, err := GetCharacterWeightInfo(database, "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to get weight info: %v", err)
	}

	if wInfo.MaxCapacityKg != 20.0 {
		t.Errorf("Expected base capacity 20.0 kg, got %.1f", wInfo.MaxCapacityKg)
	}
	if wInfo.OverburdenLimitKg != 30.0 {
		t.Errorf("Expected overburden limit 30.0 kg, got %.1f", wInfo.OverburdenLimitKg)
	}
	if wInfo.Encumbrance != EncumbranceUnencumbered {
		t.Errorf("Expected initial unencumbered state, got %s", wInfo.Encumbrance)
	}

	// Initial inventory: 4 wheat (2kg) + 2 flour (1kg) + 1 iron_wrench (2kg) = 5kg
	if wInfo.TotalWeightKg != 5.0 {
		t.Errorf("Expected total weight 5.0 kg, got %.1f", wInfo.TotalWeightKg)
	}
	if wInfo.SlotsUsed != 3 {
		t.Errorf("Expected 3 slots used, got %d", wInfo.SlotsUsed)
	}
	if wInfo.MaxSlots != 5 {
		t.Errorf("Expected 5 max slots, got %d", wInfo.MaxSlots)
	}

	// 2. Add coal to transition to ENCUMBERED (21 kg total)
	_, err = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES (?, 'coal', 16)", "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to add coal: %v", err)
	}

	wInfo, err = GetCharacterWeightInfo(database, "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to get weight info: %v", err)
	}
	if wInfo.TotalWeightKg != 21.0 {
		t.Errorf("Expected 21.0 kg, got %.1f", wInfo.TotalWeightKg)
	}
	if wInfo.Encumbrance != EncumbranceEncumbered {
		t.Errorf("Expected ENCUMBERED state, got %s", wInfo.Encumbrance)
	}

	// 3. Add more coal to transition to OVERBURDENED (> 30 kg)
	_, err = database.Exec("UPDATE inventories SET quantity = 26 WHERE character_id = ? AND item_id = 'coal'", "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to update coal: %v", err)
	}

	wInfo, err = GetCharacterWeightInfo(database, "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to get weight info: %v", err)
	}
	if wInfo.TotalWeightKg != 31.0 {
		t.Errorf("Expected 31.0 kg, got %.1f", wInfo.TotalWeightKg)
	}
	if wInfo.Encumbrance != EncumbranceOverburdened {
		t.Errorf("Expected OVERBURDENED state, got %s", wInfo.Encumbrance)
	}

	// 4. Equip woven_basket (capacity 30kg, overburden 45kg)
	_, _ = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'woven_basket', 1)")
	err = EquipContainer(database, "eldon_millwright", "woven_basket")
	if err != nil {
		t.Fatalf("Failed to equip woven_basket: %v", err)
	}

	wInfo, err = GetCharacterWeightInfo(database, "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to get weight info after container equip: %v", err)
	}
	if wInfo.MaxCapacityKg != 30.0 {
		t.Errorf("Expected capacity 30.0 with woven_basket, got %.1f", wInfo.MaxCapacityKg)
	}
	if wInfo.MaxSlots != 8 {
		t.Errorf("Expected slot limit 8 with woven_basket, got %d", wInfo.MaxSlots)
	}
	if wInfo.Encumbrance != EncumbranceEncumbered {
		t.Errorf("Expected ENCUMBERED state with basket (carrying 31.5kg, limit 45kg), got %s", wInfo.Encumbrance)
	}

	// 5. Equip leather_backpack (capacity 45kg)
	_, _ = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'leather_backpack', 1)")
	err = EquipContainer(database, "eldon_millwright", "leather_backpack")
	if err != nil {
		t.Fatalf("Failed to equip leather_backpack: %v", err)
	}

	wInfo, err = GetCharacterWeightInfo(database, "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to get weight info with backpack: %v", err)
	}
	if wInfo.MaxCapacityKg != 45.0 {
		t.Errorf("Expected capacity 45.0 with leather_backpack, got %.1f", wInfo.MaxCapacityKg)
	}
	// Total weight: 5kg initial + 26kg coal + 0.5kg basket + 1.5kg backpack = 33.0kg <= 45.0kg -> UNENCUMBERED
	if wInfo.Encumbrance != EncumbranceUnencumbered {
		t.Errorf("Expected UNENCUMBERED state with backpack, got %s", wInfo.Encumbrance)
	}
}

func TestVehicleAccessibility(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	// 1. Unassisted access (Eldon has no container equipped)
	ok, _, err := CanAccessLocation(database, "eldon_millwright", "tavern")
	if err != nil || !ok {
		t.Errorf("Unassisted character should have access to tavern, got %v (err: %v)", ok, err)
	}
	ok, _, err = CanAccessLocation(database, "eldon_millwright", "deep_forest")
	if err != nil || !ok {
		t.Errorf("Unassisted character should have access to deep_forest, got %v (err: %v)", ok, err)
	}

	// 2. Wheelbarrow equipped (Gideon has wheelbarrow)
	ok, _, err = CanAccessLocation(database, "gideon_forge", "town_mill")
	if err != nil || !ok {
		t.Errorf("Wheelbarrow should have access to town_mill (FOOT_AND_WHEELBARROW), got %v", ok)
	}
	ok, reason, err := CanAccessLocation(database, "gideon_forge", "deep_forest")
	if err != nil {
		t.Fatalf("Error checking wheelbarrow deep_forest access: %v", err)
	}
	if ok {
		t.Errorf("Wheelbarrow should NOT have access to deep_forest (FOOT_ONLY)")
	}
	if reason == "" {
		t.Errorf("Expected failure reason for wheelbarrow in deep_forest")
	}

	// 3. Draft Horse & Cart equipped
	_, _ = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('gideon_forge', 'draft_horse_cart', 1)")
	err = EquipContainer(database, "gideon_forge", "draft_horse_cart")
	if err != nil {
		t.Fatalf("Failed to equip draft_horse_cart: %v", err)
	}

	// Allowed in stables (ALL, outdoor)
	ok, _, err = CanAccessLocation(database, "gideon_forge", "stables")
	if err != nil || !ok {
		t.Errorf("Draft horse & cart should access stables, got %v", ok)
	}

	// Blocked from indoor rooms (tavern, church, blacksmith)
	ok, _, err = CanAccessLocation(database, "gideon_forge", "tavern")
	if ok {
		t.Errorf("Draft horse & cart should be blocked from indoor tavern")
	}

	// Blocked from FOOT_AND_WHEELBARROW or FOOT_ONLY
	ok, _, err = CanAccessLocation(database, "gideon_forge", "deep_forest")
	if ok {
		t.Errorf("Draft horse & cart should be blocked from deep_forest")
	}
}

func TestGroundItemsDropAndPickup(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	// Eldon starts at town_mill with 4 wheat
	// Drop 2 wheat
	err = DropItem(database, "eldon_millwright", "wheat", 2)
	if err != nil {
		t.Fatalf("Failed to drop wheat: %v", err)
	}

	// Verify inventory reduced to 2
	var invQty int
	err = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&invQty)
	if err != nil || invQty != 2 {
		t.Errorf("Expected 2 wheat remaining in inventory, got %d (err: %v)", invQty, err)
	}

	// Verify ground item exists in town_mill
	var groundQty int
	err = database.QueryRow("SELECT quantity FROM location_items WHERE location_id = 'town_mill' AND item_id = 'wheat'").Scan(&groundQty)
	if err != nil || groundQty != 2 {
		t.Errorf("Expected 2 wheat on the ground, got %d (err: %v)", groundQty, err)
	}

	// Pickup 1 wheat
	err = PickupItem(database, "eldon_millwright", "wheat", 1)
	if err != nil {
		t.Fatalf("Failed to pickup wheat: %v", err)
	}

	err = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&invQty)
	if err != nil || invQty != 3 {
		t.Errorf("Expected 3 wheat in inventory, got %d", invQty)
	}

	err = database.QueryRow("SELECT quantity FROM location_items WHERE location_id = 'town_mill' AND item_id = 'wheat'").Scan(&groundQty)
	if err != nil || groundQty != 1 {
		t.Errorf("Expected 1 wheat remaining on ground, got %d", groundQty)
	}

	// Test pickup rejection when overburdened
	// Add massive iron_ingot on ground (10 ingots * 3kg = 30kg)
	_, _ = database.Exec("INSERT INTO location_items (location_id, item_id, quantity) VALUES ('town_mill', 'iron_ingot', 10)")
	err = PickupItem(database, "eldon_millwright", "iron_ingot", 10)
	if err == nil {
		t.Errorf("Expected pickup of 30kg iron ingots to fail due to capacity limits")
	}
}

func TestSkillsProgression(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	lvl, exp, err := GetSkill(database, "eldon_millwright", "woodcutting")
	if err != nil {
		t.Fatalf("Failed to get skill: %v", err)
	}
	if lvl != 1 || exp != 0 {
		t.Errorf("Expected initial level 1, exp 0, got lvl %d, exp %d", lvl, exp)
	}

	// Award 15 XP
	newLvl, newExp, err := AddSkillXP(database, "eldon_millwright", "woodcutting", 15)
	if err != nil {
		t.Fatalf("Failed to add skill XP: %v", err)
	}
	if newLvl != 1 || newExp != 15 {
		t.Errorf("Expected lvl 1, exp 15, got lvl %d, exp %d", newLvl, newExp)
	}

	// Award 200 XP -> total 215 -> level 3
	newLvl, newExp, err = AddSkillXP(database, "eldon_millwright", "woodcutting", 200)
	if err != nil {
		t.Fatalf("Failed to add skill XP: %v", err)
	}
	if newLvl != 3 || newExp != 215 {
		t.Errorf("Expected lvl 3, exp 215, got lvl %d, exp %d", newLvl, newExp)
	}
}

func TestScaleDurationForSkill(t *testing.T) {
	tests := []struct {
		baseTicks int
		level     int
		want      int
	}{
		{baseTicks: 60, level: 1, want: 60},
		{baseTicks: 4, level: 10, want: 3},
		{baseTicks: 60, level: 100, want: 44},
		{baseTicks: 1, level: 100, want: 1},
		{baseTicks: 0, level: 10, want: 1},
	}
	for _, tt := range tests {
		if got := ScaleDurationForSkill(tt.baseTicks, tt.level); got != tt.want {
			t.Errorf("ScaleDurationForSkill(%d, %d) = %d, want %d", tt.baseTicks, tt.level, got, tt.want)
		}
	}
}

func TestResolveForaging(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	// Move Eldon to deep_forest for foraging
	_, _ = database.Exec("UPDATE characters SET current_location = 'deep_forest' WHERE id = 'eldon_millwright'")

	// Resolve 60 ticks of firewood foraging
	res, err := ResolveForaging(database, "eldon_millwright", "deep_forest", "firewood", 60, "clear", false)
	if err != nil {
		t.Fatalf("Failed to resolve foraging: %v", err)
	}

	if res.TicksSpent != 60 {
		t.Errorf("Expected 60 ticks spent, got %d", res.TicksSpent)
	}
	if res.SkillXPGained != 15 {
		t.Errorf("Expected 15 XP gained for 60 ticks, got %d", res.SkillXPGained)
	}
	if res.EnergyDeducted <= 0 {
		t.Errorf("Expected energy deduction, got %d", res.EnergyDeducted)
	}
	if len(res.Yield) == 0 {
		t.Errorf("Expected at least primary yield for 60 ticks")
	}
	if res.NarrativeLog == "" {
		t.Errorf("Expected narrative log to be non-empty")
	}

	// Check character status was reset to IDLE
	var status string
	err = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if err != nil || status != "IDLE" {
		t.Errorf("Expected character status IDLE, got %s (err: %v)", status, err)
	}
}

func TestResolveForagingReturnsSkillLevelUpAndPersistenceErrors(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, _, err := AddSkillXP(database, "eldon_millwright", "woodcutting", 95); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE characters SET current_location = 'deep_forest' WHERE id = 'eldon_millwright'"); err != nil {
		t.Fatal(err)
	}
	result, err := ResolveForaging(database, "eldon_millwright", "deep_forest", "firewood", 60, "clear", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.PreviousLevel != 1 || result.SkillLevel != 2 || result.SkillExperience != 110 {
		t.Errorf("foraging skill progress = %d -> %d (%d XP), want 1 -> 2 (110 XP)", result.PreviousLevel, result.SkillLevel, result.SkillExperience)
	}

	if _, err := database.Exec("DROP TABLE character_skills"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveForaging(database, "eldon_millwright", "deep_forest", "firewood", 60, "clear", false); err == nil {
		t.Fatal("expected skill persistence failure to fail foraging resolution")
	}
}
