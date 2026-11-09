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

	if _, err := database.Exec(`
		UPDATE character_skills SET level = 3, experience = 215
		WHERE character_id = 'eldon_millwright' AND skill_id = 'crafting'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE characters SET hunger = 61, energy = 72, health = 43
		WHERE id = 'eldon_millwright'
	`); err != nil {
		t.Fatal(err)
	}
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
	if perception.Character.Hunger != 61 || perception.Character.Energy != 72 || perception.Character.Health != 43 {
		t.Errorf("expected distinct hunger/energy/health values 61/72/43, got %d/%d/%d",
			perception.Character.Hunger, perception.Character.Energy, perception.Character.Health)
	}
	if perception.Character.CurrentLocation != "town_mill" {
		t.Errorf("expected town_mill, got %s", perception.Character.CurrentLocation)
	}
	if perception.Character.HomeLocationID != "eldons_room" || perception.Character.HomeLocationName != "Eldon's Room" {
		t.Errorf("expected Eldon's home to be eldons_room, got %q (%q)", perception.Character.HomeLocationID, perception.Character.HomeLocationName)
	}
	if len(perception.LocationGraph) == 0 {
		t.Fatal("expected state sync to include the location graph")
	}
	if len(perception.Character.Inventory) == 0 {
		t.Error("expected non-empty inventory for Eldon")
	}
	if perception.Character.MaxCapacityKg <= 0 || perception.Character.MaxSlots <= 0 {
		t.Errorf("expected inventory capacity data, got %+v", perception.Character)
	}
	if perception.Character.SlotsUsed != len(perception.Character.Inventory) {
		t.Errorf("slots used = %d, inventory entries = %d", perception.Character.SlotsUsed, len(perception.Character.Inventory))
	}
	if len(perception.Character.Skills) != 4 {
		t.Fatalf("expected 4 visible skills, got %d", len(perception.Character.Skills))
	}
	foundCraftingSkill := false
	for _, skill := range perception.Character.Skills {
		if skill.SkillID == "crafting" {
			foundCraftingSkill = skill.Level == 3 && skill.Experience == 215 && skill.XPIntoLevel == 15 && skill.XPToNextLevel == 85
		}
	}
	if !foundCraftingSkill {
		t.Errorf("expected crafting level and progress in perception, got %+v", perception.Character.Skills)
	}
	if len(perception.Location.AvailableRecipes) != 2 {
		t.Fatalf("expected Eldon's two known recipes at the mill, got %+v", perception.Location.AvailableRecipes)
	}
	for _, recipe := range perception.Location.AvailableRecipes {
		if recipe.RecipeID != "mill_flour" && recipe.RecipeID != "weave_basket" {
			t.Errorf("unexpected recipe in Eldon's mill perception: %s", recipe.RecipeID)
		}
		if recipe.DurationTicks != ScaleDurationForSkill(recipe.BaseDurationTicks, recipe.SkillLevel) {
			t.Errorf("recipe %s duration was not adjusted for skill: %+v", recipe.RecipeID, recipe)
		}
	}
	if perception.Location.ID != "town_mill" {
		t.Errorf("expected location ID town_mill, got %s", perception.Location.ID)
	}
	foundWheatBuyOffer := false
	for _, item := range perception.Location.ShopItems {
		if item.ItemID == "wheat" {
			foundWheatBuyOffer = item.BuyPriceGold == 1 && item.StockQty == 0
			break
		}
	}
	if !foundWheatBuyOffer {
		t.Errorf("expected perception to show the mill's wheat buy offer: %+v", perception.Location.ShopItems)
	}
	if perception.Location.Name != "The Town Mill" {
		t.Errorf("expected location name The Town Mill, got %s", perception.Location.Name)
	}
	if len(perception.Location.Workstations) == 0 {
		t.Error("expected workstation in town mill (timber_saw)")
	}
	if _, err := database.Exec("UPDATE characters SET current_location = 'tavern' WHERE id = 'eldon_millwright'"); err != nil {
		t.Fatal(err)
	}
	tavernPerception, err := GetCharacterPerception(database, "eldon_millwright")
	if err != nil {
		t.Fatal(err)
	}
	for _, recipe := range tavernPerception.Location.AvailableRecipes {
		if recipe.RecipeID == "brew_berry_wine" {
			t.Fatal("Eldon should not see a recipe he does not know")
		}
	}
	marthaPerception, err := GetCharacterPerception(database, "martha_barkeep")
	if err != nil {
		t.Fatal(err)
	}
	foundBerryWine, foundAdvancedWine := false, false
	for _, recipe := range marthaPerception.Location.AvailableRecipes {
		foundBerryWine = foundBerryWine || recipe.RecipeID == "brew_berry_wine"
		foundAdvancedWine = foundAdvancedWine || recipe.RecipeID == "brew_elderflower_wine"
	}
	if !foundBerryWine || foundAdvancedWine {
		t.Errorf("Martha's recipe list should include her basic wine but gate advanced wine by skill: %+v", marthaPerception.Location.AvailableRecipes)
	}
	foundWeightedShopItem := false
	for _, item := range tavernPerception.Location.ShopItems {
		if item.ItemID == "ale_mug" {
			foundWeightedShopItem = item.WeightKg > 0
			break
		}
	}
	if !foundWeightedShopItem {
		t.Error("expected shop perception to include item weight for ale_mug")
	}

	// Test non-existent character
	_, err = GetCharacterPerception(database, "nonexistent_character")
	if err == nil {
		t.Fatal("expected error for nonexistent character, got nil")
	}
}

func TestLocationGraphFiltersVehicleInaccessibleHops(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	perception, err := GetCharacterPerception(database, "gideon_forge")
	if err != nil {
		t.Fatal(err)
	}
	hasHomeEdge := false
	hasFootOnlyEdge := false
	for _, edge := range perception.LocationGraph {
		if edge.ToLocationID == "gideons_room" {
			hasHomeEdge = true
		}
		if edge.ToLocationID == "deep_forest" {
			hasFootOnlyEdge = true
		}
	}
	if !hasHomeEdge {
		t.Error("Gideon's route graph should include access to his room")
	}
	if hasFootOnlyEdge {
		t.Error("Gideon's wheelbarrow route graph should exclude foot-only locations")
	}
}

func TestGetPublicCharacterDescriptionUsesReadableEquippedGearName(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	gideon, err := GetPublicCharacterDescription(database, "gideon_forge")
	if err != nil {
		t.Fatal(err)
	}
	if gideon.Equipped != "Wheelbarrow" {
		t.Errorf("equipped gear = %q, want readable item name Wheelbarrow", gideon.Equipped)
	}

	eleanor, err := GetPublicCharacterDescription(database, "eleanor_clerk")
	if err != nil {
		t.Fatal(err)
	}
	if eleanor.Equipped != "unassisted (no container)" {
		t.Errorf("unassisted gear = %q", eleanor.Equipped)
	}

	if _, err := database.Exec(`
		UPDATE characters SET equipped_container_id = 'missing_gear' WHERE id = 'eleanor_clerk'
	`); err != nil {
		t.Fatal(err)
	}
	eleanor, err = GetPublicCharacterDescription(database, "eleanor_clerk")
	if err != nil {
		t.Fatal(err)
	}
	if eleanor.Equipped != "missing_gear" {
		t.Errorf("unknown equipped gear fallback = %q, want ID", eleanor.Equipped)
	}
}
