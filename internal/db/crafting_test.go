package db

import (
	"strings"
	"testing"
)

func TestSeedCrafting(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	var wsCount int
	err = database.QueryRow("SELECT COUNT(*) FROM workstations").Scan(&wsCount)
	if err != nil {
		t.Fatalf("Failed to count workstations: %v", err)
	}
	if wsCount != 7 {
		t.Errorf("Expected 7 seeded workstations, got %d", wsCount)
	}

	var recCount int
	err = database.QueryRow("SELECT COUNT(*) FROM recipes").Scan(&recCount)
	if err != nil {
		t.Fatalf("Failed to count recipes: %v", err)
	}
	if recCount != 18 {
		t.Errorf("Expected 18 seeded recipes, got %d", recCount)
	}
}

func TestScotusKnowsMillFlourAfterProgressionInitialization(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		INSERT INTO characters (
			id, name, persona_prompt, occupation, gold, hunger, energy,
			current_location, home_location_id, controller_type, mqtt_password
		) VALUES ('scotus', 'Scotus', '', 'Monk', 0, 100, 100, 'town_mill', 'town_mill', 'agent', 'test')
	`); err != nil {
		t.Fatal(err)
	}
	if err := InitializeAllCharacterProgression(database); err != nil {
		t.Fatal(err)
	}

	recipe, err := GetRecipe(database, "mill_flour")
	if err != nil {
		t.Fatal(err)
	}
	accessible, _, err := CanAccessRecipe(database, "scotus", recipe)
	if err != nil || !accessible {
		t.Fatalf("Scotus should be able to access level 1 recipe mill_flour: accessible=%v err=%v", accessible, err)
	}
}

func TestCanCraftRecipeValidation(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	// Eldon is at town_mill with 4 wheat
	// 1. Valid craft: mill_flour at town_mill with millstones
	ok, reason, err := CanCraftRecipe(database, "eldon_millwright", "mill_flour")
	if err != nil {
		t.Fatalf("Error checking CanCraftRecipe: %v", err)
	}
	if !ok {
		t.Errorf("Expected Eldon to be able to craft mill_flour, failed with: %s", reason)
	}

	// 2. Spatial Workstation restriction: Eldon trying to bake bread at town_mill (requires stone_hearth at bakery)
	ok, reason, err = CanCraftRecipe(database, "eldon_millwright", "bake_bread")
	if err != nil {
		t.Fatalf("Error checking CanCraftRecipe: %v", err)
	}
	if ok {
		t.Errorf("Expected bake_bread to fail at town_mill due to missing stone_hearth")
	}

	// 3. Move Eldon to bakery, but missing fresh_water ingredient for bake_bread
	_, _ = database.Exec("UPDATE characters SET current_location = 'bakery' WHERE id = 'eldon_millwright'")
	ok, reason, err = CanCraftRecipe(database, "eldon_millwright", "bake_bread")
	if err != nil {
		t.Fatalf("Error checking CanCraftRecipe: %v", err)
	}
	if ok {
		t.Errorf("Expected bake_bread to fail due to missing fresh_water")
	}

	// 4. Mobile recipe: Basket weaving requires no fixed station
	// Give Eldon 4 river_reeds
	_, _ = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'river_reeds', 4)")
	ok, reason, err = CanCraftRecipe(database, "eldon_millwright", "weave_basket")
	if err != nil {
		t.Fatalf("Error checking CanCraftRecipe: %v", err)
	}
	if !ok {
		t.Errorf("Expected basket weaving to be allowed anywhere with ingredients, failed with: %s", reason)
	}

	// Recipe access for higher tier recipes (e.g., brew_charcoal_draught requires herbalism level 2).
	charcoalRecipe, err := GetRecipe(database, "brew_charcoal_draught")
	if err != nil {
		t.Fatal(err)
	}
	ok, reason, err = CanAccessRecipe(database, "eldon_millwright", charcoalRecipe)
	if err != nil {
		t.Fatalf("Error checking restricted recipe: %v", err)
	}
	if ok || (!strings.Contains(reason, "level") && !strings.Contains(reason, "mentorship") && !strings.Contains(reason, "required")) {
		t.Errorf("expected Eldon not to have access to brew_charcoal_draught, got ok=%v reason=%q", ok, reason)
	}
	forgeHammerRecipe, err := GetRecipe(database, "forge_hammer")
	if err != nil {
		t.Fatal(err)
	}
	ok, reason, err = CanAccessRecipe(database, "eldon_millwright", forgeHammerRecipe)
	if err != nil {
		t.Fatalf("Error checking skill-gated recipe: %v", err)
	}
	if ok || (!strings.Contains(reason, "level 2") && !strings.Contains(reason, "required")) {
		t.Errorf("expected level requirement for forge_hammer, got ok=%v reason=%q", ok, reason)
	}
}

func TestStartAndResolveCrafting(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	// Eldon at town_mill has 4 wheat, 2 flour, 100 energy
	err = StartCrafting(database, "eldon_millwright", "mill_flour", "intent_craft_1", 100)
	if err != nil {
		t.Fatalf("Failed to start crafting: %v", err)
	}

	// Verify 2 wheat deducted from inventory
	var wheatQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&wheatQty)
	if wheatQty != 2 {
		t.Errorf("Expected 2 wheat remaining after starting craft, got %d", wheatQty)
	}

	// Verify character status is CRAFTING
	var status string
	var endTick uint64
	_ = database.QueryRow("SELECT status, action_end_tick FROM characters WHERE id = 'eldon_millwright'").Scan(&status, &endTick)
	if status != "CRAFTING" {
		t.Errorf("Expected character status CRAFTING, got %s", status)
	}
	if endTick != 104 { // 100 + 4 duration
		t.Errorf("Expected endTick 104, got %d", endTick)
	}

	// Resolve Crafting
	res, err := ResolveCrafting(database, "eldon_millwright", "mill_flour")
	if err != nil {
		t.Fatalf("Failed to resolve crafting: %v", err)
	}

	if res.Yield[0].ItemID != "flour" || res.Yield[0].Quantity != 2 {
		t.Errorf("Expected yield of 2x flour, got %v", res.Yield)
	}

	// Verify inventory now has 4 flour (2 initial + 2 produced)
	var flourQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'flour'").Scan(&flourQty)
	if flourQty != 4 {
		t.Errorf("Expected 4 flour in inventory, got %d", flourQty)
	}

	// Verify energy deducted: 100 - 5 = 95
	var energy int
	_ = database.QueryRow("SELECT energy FROM characters WHERE id = 'eldon_millwright'").Scan(&energy)
	if energy != 95 {
		t.Errorf("Expected 95 energy after crafting, got %d", energy)
	}

	// Verify status is back to IDLE
	_ = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if status != "IDLE" {
		t.Errorf("Expected status to be IDLE after resolve, got %s", status)
	}
}

func TestStartCraftingUsesSkillAdjustedDuration(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		UPDATE character_skills SET level = 10, experience = 900
		WHERE character_id = 'eldon_millwright' AND skill_id = 'crafting'
	`); err != nil {
		t.Fatal(err)
	}
	if err := StartCrafting(database, "eldon_millwright", "mill_flour", "intent_fast_craft", 100); err != nil {
		t.Fatal(err)
	}
	var endTick uint64
	if err := database.QueryRow("SELECT action_end_tick FROM characters WHERE id = 'eldon_millwright'").Scan(&endTick); err != nil {
		t.Fatal(err)
	}
	if endTick != 103 {
		t.Errorf("skill level 10 should reduce 4-tick recipe to 3 ticks, end tick = %d", endTick)
	}
}

func TestCancelCraftingRefund(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init in-memory DB: %v", err)
	}
	defer database.Close()

	// Start crafting mill_flour (takes 2 wheat)
	err = StartCrafting(database, "eldon_millwright", "mill_flour", "intent_cancel_test", 100)
	if err != nil {
		t.Fatalf("Failed to start crafting: %v", err)
	}

	var wheatQty int
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&wheatQty)
	if wheatQty != 2 {
		t.Errorf("Expected 2 wheat remaining, got %d", wheatQty)
	}

	// Cancel crafting
	err = CancelCrafting(database, "eldon_millwright")
	if err != nil {
		t.Fatalf("Failed to cancel crafting: %v", err)
	}

	// Verify 2 wheat was refunded (back to 4)
	_ = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'wheat'").Scan(&wheatQty)
	if wheatQty != 4 {
		t.Errorf("Expected 4 wheat after refund, got %d", wheatQty)
	}

	// Verify status is IDLE
	var status string
	_ = database.QueryRow("SELECT status FROM characters WHERE id = 'eldon_millwright'").Scan(&status)
	if status != "IDLE" {
		t.Errorf("Expected status IDLE, got %s", status)
	}
}

func TestResolveCraftingReturnsSkillPersistenceErrors(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec("DROP TABLE character_skills"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveCrafting(database, "eldon_millwright", "mill_flour"); err == nil {
		t.Fatal("expected skill persistence failure to fail crafting resolution")
	}
	var energy int
	if err := database.QueryRow("SELECT energy FROM characters WHERE id = 'eldon_millwright'").Scan(&energy); err != nil {
		t.Fatal(err)
	}
	if energy != 100 {
		t.Errorf("crafting should not deduct energy when skill progression fails, got %d", energy)
	}
}
