package db

import (
	"path/filepath"
	"testing"
)

func TestCommunalFarmPlotPlantingAndHarvesting(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		UPDATE characters SET current_location = 'village_field'
		WHERE id IN ('luke_farmer', 'martha_barkeep')
	`); err != nil {
		t.Fatal(err)
	}

	planted, err := PlantFarmPlot(database, "luke_farmer", "field_plot_1", "wheat", 100)
	if err != nil {
		t.Fatalf("PlantFarmPlot() error = %v", err)
	}
	if planted.MatureTick != 124 {
		t.Fatalf("mature tick = %d, want 124", planted.MatureTick)
	}

	var seeds int
	if err := database.QueryRow(`
		SELECT quantity FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'wheat'
	`).Scan(&seeds); err != nil {
		t.Fatal(err)
	}
	if seeds != 9 {
		t.Fatalf("wheat after planting = %d, want 9", seeds)
	}

	if _, err := HarvestFarmPlot(database, "martha_barkeep", "field_plot_1", 123); err == nil {
		t.Fatal("harvesting before maturity unexpectedly succeeded")
	}

	harvested, err := HarvestFarmPlot(database, "martha_barkeep", "field_plot_1", 124)
	if err != nil {
		t.Fatalf("communal HarvestFarmPlot() error = %v", err)
	}
	if harvested.Quantity != 3 || harvested.ItemID != "wheat" || harvested.PlacedOnGround {
		t.Fatalf("unexpected harvest result: %+v", harvested)
	}

	var wheat int
	if err := database.QueryRow(`
		SELECT quantity FROM inventories WHERE character_id = 'martha_barkeep' AND item_id = 'wheat'
	`).Scan(&wheat); err != nil {
		t.Fatal(err)
	}
	if wheat != 3 {
		t.Fatalf("harvested wheat = %d, want 3", wheat)
	}

	var cropID string
	if err := database.QueryRow(`SELECT COALESCE(crop_id, '') FROM farm_plots WHERE id = 'field_plot_1'`).Scan(&cropID); err != nil {
		t.Fatal(err)
	}
	if cropID != "" {
		t.Fatalf("plot crop after harvest = %q, want empty", cropID)
	}
}

func TestFarmPlotRequiresLocalIdleCharacterAndSeed(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := PlantFarmPlot(database, "luke_farmer", "field_plot_1", "wheat", 0); err == nil {
		t.Fatal("planting away from the field unexpectedly succeeded")
	}

	if _, err := database.Exec(`UPDATE characters SET current_location = 'village_field', status = 'CRAFTING' WHERE id = 'luke_farmer'`); err != nil {
		t.Fatal(err)
	}
	if _, err := PlantFarmPlot(database, "luke_farmer", "field_plot_1", "wheat", 0); err == nil {
		t.Fatal("planting while busy unexpectedly succeeded")
	}

	if _, err := database.Exec(`UPDATE characters SET status = 'IDLE' WHERE id = 'luke_farmer'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'wheat'`); err != nil {
		t.Fatal(err)
	}
	if _, err := PlantFarmPlot(database, "luke_farmer", "field_plot_1", "wheat", 0); err == nil {
		t.Fatal("planting without seeds unexpectedly succeeded")
	}
}

func TestFarmPlotAndCropPerception(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`UPDATE characters SET current_location = 'village_field' WHERE id = 'luke_farmer'`); err != nil {
		t.Fatal(err)
	}
	perception, err := GetCharacterPerception(database, "luke_farmer")
	if err != nil {
		t.Fatal(err)
	}
	if len(perception.Location.FarmCrops) != 2 {
		t.Fatalf("available farm crops = %+v, want wheat and vegetables", perception.Location.FarmCrops)
	}
	foundVegetables := false
	for _, crop := range perception.Location.FarmCrops {
		if crop.ID == "vegetables" && crop.SeedItemID == "vegetable_seeds" &&
			crop.HarvestItemID == "vegetable_basket" && crop.YieldQuantity == 3 {
			foundVegetables = true
		}
	}
	if !foundVegetables {
		t.Fatalf("vegetable crop missing from perception: %+v", perception.Location.FarmCrops)
	}
	if len(perception.Location.FarmPlots) != 4 {
		t.Fatalf("farm plots = %d, want 4", len(perception.Location.FarmPlots))
	}
}

func TestVegetableCropProducesExistingFoodItem(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`UPDATE characters SET current_location = 'village_field' WHERE id = 'luke_farmer'`); err != nil {
		t.Fatal(err)
	}
	planted, err := PlantFarmPlot(database, "luke_farmer", "field_plot_3", "vegetables", 50)
	if err != nil {
		t.Fatal(err)
	}
	if planted.MatureTick != 74 {
		t.Fatalf("vegetable maturity tick = %d, want 74", planted.MatureTick)
	}

	var seeds int
	if err := database.QueryRow(`
		SELECT quantity FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'vegetable_seeds'
	`).Scan(&seeds); err != nil {
		t.Fatal(err)
	}
	if seeds != 9 {
		t.Fatalf("vegetable seeds after planting = %d, want 9", seeds)
	}

	harvested, err := HarvestFarmPlot(database, "luke_farmer", "field_plot_3", 74)
	if err != nil {
		t.Fatal(err)
	}
	if harvested.ItemID != "vegetable_basket" || harvested.Quantity != 3 {
		t.Fatalf("vegetable harvest = %+v, want 3 vegetable baskets", harvested)
	}
	var baskets int
	if err := database.QueryRow(`
		SELECT quantity FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'vegetable_basket'
	`).Scan(&baskets); err != nil {
		t.Fatal(err)
	}
	if baskets != 5 {
		t.Fatalf("vegetable baskets after harvest = %d, want 5", baskets)
	}
}

func TestHarvestUsesGroundWhenCharacterIsOverburdened(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		UPDATE characters SET current_location = 'village_field'
		WHERE id IN ('luke_farmer', 'martha_barkeep')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity) VALUES ('martha_barkeep', 'coal', 100)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := PlantFarmPlot(database, "luke_farmer", "field_plot_2", "wheat", 0); err != nil {
		t.Fatal(err)
	}

	harvested, err := HarvestFarmPlot(database, "martha_barkeep", "field_plot_2", 24)
	if err != nil {
		t.Fatal(err)
	}
	if !harvested.PlacedOnGround {
		t.Fatal("overburdened character's harvest was not placed on the ground")
	}

	var groundQuantity int
	if err := database.QueryRow(`
		SELECT quantity FROM location_items WHERE location_id = 'village_field' AND item_id = 'wheat'
	`).Scan(&groundQuantity); err != nil {
		t.Fatal(err)
	}
	if groundQuantity != 3 {
		t.Fatalf("ground wheat = %d, want 3", groundQuantity)
	}
}

func TestExistingWorldReceivesAgricultureDataWithoutFullReseed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.db")
	database, err := InitDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO locations (id, name, description) VALUES
			('town_square', 'Town Square', ''),
			('prairie', 'Prairie', ''),
			('market', 'Market', '')
	`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = InitDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM farm_plots`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("migrated farm plots = %d, want 4", count)
	}
	var fieldCount, seedItemCount, connectionCount, shopSeedCount, cropCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM locations WHERE id = 'village_field'`).Scan(&fieldCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM items WHERE id = 'wheat'`).Scan(&seedItemCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM farm_crops`).Scan(&cropCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM location_shop_items
		WHERE location_id = 'market' AND item_id = 'vegetable_seeds'
	`).Scan(&shopSeedCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM location_connections
		WHERE from_location_id = 'town_square' AND to_location_id = 'village_field'
	`).Scan(&connectionCount); err != nil {
		t.Fatal(err)
	}
	if fieldCount != 1 || seedItemCount != 1 || connectionCount != 1 || shopSeedCount != 1 || cropCount != 2 {
		t.Fatalf("migration incomplete: field=%d seed=%d connection=%d shop_seed=%d crops=%d",
			fieldCount, seedItemCount, connectionCount, shopSeedCount, cropCount)
	}
}
