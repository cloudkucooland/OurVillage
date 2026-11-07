package server

import (
	"encoding/json"
	"testing"

	"agentmud/internal/db"
)

func TestPlantAndHarvestIntents(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	srv, err := NewServer(database, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE characters SET current_location = 'village_field' WHERE id = 'luke_farmer'`); err != nil {
		t.Fatal(err)
	}

	plantPayload, _ := json.Marshal(PlantPayload{PlotID: "field_plot_1", CropID: "wheat"})
	srv.SetTickCount(10)
	srv.handlePlant("luke_farmer", "plant-wheat", plantPayload)

	var cropID string
	if err := database.QueryRow(`SELECT COALESCE(crop_id, '') FROM farm_plots WHERE id = 'field_plot_1'`).Scan(&cropID); err != nil {
		t.Fatal(err)
	}
	if cropID != "wheat" {
		t.Fatalf("planted crop = %q, want wheat", cropID)
	}

	harvestPayload, _ := json.Marshal(HarvestPayload{PlotID: "field_plot_1"})
	srv.SetTickCount(34)
	srv.handleHarvest("luke_farmer", "harvest-wheat", harvestPayload)

	var wheat int
	if err := database.QueryRow(`
		SELECT quantity FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'wheat'
	`).Scan(&wheat); err != nil {
		t.Fatal(err)
	}
	if wheat != 12 {
		t.Fatalf("wheat after harvest = %d, want 12", wheat)
	}
}
