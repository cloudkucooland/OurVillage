package db

import (
	"database/sql"
	"fmt"
)

type FarmCrop struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	SeedItemID      string `json:"seed_item_id"`
	SeedItemName    string `json:"seed_item_name"`
	HarvestItemID   string `json:"harvest_item_id"`
	HarvestItemName string `json:"harvest_item_name"`
	GrowthTicks     int    `json:"growth_ticks"`
	YieldQuantity   int    `json:"yield_quantity"`
}

type FarmPlot struct {
	ID            string `json:"id"`
	CropID        string `json:"crop_id,omitempty"`
	CropName      string `json:"crop_name,omitempty"`
	SeedItemID    string `json:"seed_item_id,omitempty"`
	HarvestItemID string `json:"harvest_item_id,omitempty"`
	PlantedTick   uint64 `json:"planted_tick,omitempty"`
	MatureTick    uint64 `json:"mature_tick,omitempty"`
}

type FarmActionResult struct {
	PlotID         string `json:"plot_id"`
	LocationID     string `json:"location_id"`
	CropID         string `json:"crop_id"`
	CropName       string `json:"crop_name"`
	ItemID         string `json:"item_id,omitempty"`
	ItemName       string `json:"item_name,omitempty"`
	Quantity       int    `json:"quantity,omitempty"`
	MatureTick     uint64 `json:"mature_tick,omitempty"`
	PlacedOnGround bool   `json:"placed_on_ground,omitempty"`
}

var defaultFarmCrops = []FarmCrop{
	{
		ID:            "wheat",
		Name:          "Wheat",
		SeedItemID:    "wheat",
		HarvestItemID: "wheat",
		GrowthTicks:   24,
		YieldQuantity: 3,
	},
	{
		ID:            "vegetables",
		Name:          "Vegetables",
		SeedItemID:    "vegetable_seeds",
		HarvestItemID: "vegetable_basket",
		GrowthTicks:   24,
		YieldQuantity: 3,
	},
}

// SeedFarming ensures the communal field, crop items, definitions, and plots exist without resetting world state.
func SeedFarming(database *sql.DB) error {
	var field LocationSeed
	for _, location := range DefaultLocations {
		if location.ID == "village_field" {
			field = location
			break
		}
	}
	if field.ID == "" {
		return fmt.Errorf("default village field location is missing")
	}
	isPublic, isIndoor := 0, 0
	if field.IsPublic {
		isPublic = 1
	}
	if field.IsIndoor {
		isIndoor = 1
	}
	if _, err := database.Exec(`
		INSERT INTO locations (id, name, description, owner_id, is_public_property, is_indoor, vehicle_access_tier, open_time, close_time, rules_prompt)
		VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, field.ID, field.Name, field.Description, isPublic, isIndoor, field.VehicleAccessTier,
		field.OpenTime, field.CloseTime, field.RulesPrompt); err != nil {
		return fmt.Errorf("ensure communal village field: %w", err)
	}

	for _, connection := range []struct {
		from, to string
		ticks    int
	}{
		{"town_square", "village_field", 1},
		{"village_field", "town_square", 1},
		{"village_field", "prairie", 2},
		{"prairie", "village_field", 2},
	} {
		var locationCount int
		if err := database.QueryRow(`
			SELECT COUNT(*) FROM locations WHERE id IN (?, ?)
		`, connection.from, connection.to).Scan(&locationCount); err != nil {
			return fmt.Errorf("check farm path %q -> %q: %w", connection.from, connection.to, err)
		}
		if locationCount == 2 {
			if _, err := database.Exec(`
				INSERT INTO location_connections (from_location_id, to_location_id, travel_ticks)
				VALUES (?, ?, ?)
				ON CONFLICT(from_location_id, to_location_id) DO NOTHING
			`, connection.from, connection.to, connection.ticks); err != nil {
				return fmt.Errorf("ensure farm path %q -> %q: %w", connection.from, connection.to, err)
			}
		}
	}

	for _, crop := range defaultFarmCrops {
		if err := ensureFarmItem(database, crop.SeedItemID); err != nil {
			return fmt.Errorf("ensure seed item for crop %q: %w", crop.ID, err)
		}
		if err := ensureFarmItem(database, crop.HarvestItemID); err != nil {
			return fmt.Errorf("ensure harvest item for crop %q: %w", crop.ID, err)
		}
		_, err := database.Exec(`
			INSERT INTO farm_crops (id, name, seed_item_id, harvest_item_id, growth_ticks, yield_quantity)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				seed_item_id = excluded.seed_item_id,
				harvest_item_id = excluded.harvest_item_id,
				growth_ticks = excluded.growth_ticks,
				yield_quantity = excluded.yield_quantity
		`, crop.ID, crop.Name, crop.SeedItemID, crop.HarvestItemID, crop.GrowthTicks, crop.YieldQuantity)
		if err != nil {
			return fmt.Errorf("seed farm crop %q: %w", crop.ID, err)
		}
	}

	if _, err := database.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity)
		SELECT id, 'vegetable_seeds', 10 FROM characters
		WHERE id = 'luke_farmer'
		  AND NOT EXISTS (
		      SELECT 1 FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'vegetable_seeds'
		  )
	`); err != nil {
		return fmt.Errorf("ensure farmer starter vegetable seeds: %w", err)
	}
	var marketExists int
	if err := database.QueryRow(`SELECT COUNT(*) FROM locations WHERE id = 'market'`).Scan(&marketExists); err != nil {
		return fmt.Errorf("check market for vegetable seeds: %w", err)
	}
	if marketExists > 0 {
		for _, shopItem := range DefaultShopItems {
			if shopItem.LocationID != "market" || shopItem.ItemID != "vegetable_seeds" {
				continue
			}
			if _, err := database.Exec(`
				INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(location_id, item_id) DO NOTHING
			`, shopItem.LocationID, shopItem.ItemID, shopItem.PriceGold, shopItem.StockQuantity); err != nil {
				return fmt.Errorf("ensure vegetable seed stock: %w", err)
			}
		}
	}

	for _, plotID := range []string{"field_plot_1", "field_plot_2", "field_plot_3", "field_plot_4"} {
		_, err := database.Exec(`
			INSERT INTO farm_plots (id, location_id) VALUES (?, 'village_field')
			ON CONFLICT(id) DO NOTHING
		`, plotID)
		if err != nil {
			return fmt.Errorf("seed farm plot %q: %w", plotID, err)
		}
	}
	return nil
}

func ensureFarmItem(database *sql.DB, itemID string) error {
	for _, item := range DefaultItems {
		if item.ID != itemID {
			continue
		}
		isConsumable, isContainer := 0, 0
		if item.IsConsumable {
			isConsumable = 1
		}
		if item.IsContainer {
			isContainer = 1
		}
		_, err := database.Exec(`
			INSERT INTO items (id, name, description, weight_kg, is_consumable, hunger_restore, health_restore, is_container, capacity_kg, slot_limit)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, item.ID, item.Name, item.Description, item.WeightKg, isConsumable, item.HungerRestore,
			item.HealthRestore, isContainer, item.CapacityKg, item.SlotLimit)
		return err
	}
	return fmt.Errorf("item %q is not defined", itemID)
}

// GetFarmLocationData returns the crops available to plant and the state of a location's plots.
func GetFarmLocationData(q Queryable, locationID string) ([]FarmCrop, []FarmPlot, error) {
	cropRows, err := q.Query(`
		SELECT c.id, c.name, c.seed_item_id, si.name, c.harvest_item_id, hi.name, c.growth_ticks, c.yield_quantity
		FROM farm_crops c
		JOIN items si ON si.id = c.seed_item_id
		JOIN items hi ON hi.id = c.harvest_item_id
		ORDER BY c.name
	`)
	if err != nil {
		return nil, nil, fmt.Errorf("query farm crops: %w", err)
	}
	crops := make([]FarmCrop, 0)
	for cropRows.Next() {
		var crop FarmCrop
		if err := cropRows.Scan(&crop.ID, &crop.Name, &crop.SeedItemID, &crop.SeedItemName,
			&crop.HarvestItemID, &crop.HarvestItemName, &crop.GrowthTicks, &crop.YieldQuantity); err != nil {
			cropRows.Close()
			return nil, nil, fmt.Errorf("scan farm crop: %w", err)
		}
		crops = append(crops, crop)
	}
	if err := cropRows.Err(); err != nil {
		cropRows.Close()
		return nil, nil, fmt.Errorf("read farm crops: %w", err)
	}
	if err := cropRows.Close(); err != nil {
		return nil, nil, fmt.Errorf("close farm crops: %w", err)
	}

	plotRows, err := q.Query(`
		SELECT p.id, COALESCE(c.id, ''), COALESCE(c.name, ''), COALESCE(c.seed_item_id, ''),
		       COALESCE(c.harvest_item_id, ''), p.planted_tick, p.mature_tick
		FROM farm_plots p
		LEFT JOIN farm_crops c ON c.id = p.crop_id
		WHERE p.location_id = ?
		ORDER BY p.id
	`, locationID)
	if err != nil {
		return nil, nil, fmt.Errorf("query farm plots: %w", err)
	}
	plots := make([]FarmPlot, 0)
	for plotRows.Next() {
		var plot FarmPlot
		if err := plotRows.Scan(&plot.ID, &plot.CropID, &plot.CropName, &plot.SeedItemID,
			&plot.HarvestItemID, &plot.PlantedTick, &plot.MatureTick); err != nil {
			plotRows.Close()
			return nil, nil, fmt.Errorf("scan farm plot: %w", err)
		}
		plots = append(plots, plot)
	}
	if err := plotRows.Err(); err != nil {
		plotRows.Close()
		return nil, nil, fmt.Errorf("read farm plots: %w", err)
	}
	if err := plotRows.Close(); err != nil {
		return nil, nil, fmt.Errorf("close farm plots: %w", err)
	}
	return crops, plots, nil
}

// PlantFarmPlot consumes a seed and plants a communal plot for any idle character at its location.
func PlantFarmPlot(database *sql.DB, characterID, plotID, cropID string, currentTick uint64) (*FarmActionResult, error) {
	tx, err := database.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin planting transaction: %w", err)
	}
	defer tx.Rollback()

	var locationID, status string
	if err := tx.QueryRow(`SELECT current_location, status FROM characters WHERE id = ?`, characterID).Scan(&locationID, &status); err != nil {
		return nil, fmt.Errorf("character %q not found: %w", characterID, err)
	}
	if status != "IDLE" {
		return nil, fmt.Errorf("character is busy: %s", status)
	}

	var plotLocation string
	var existingCrop sql.NullString
	if err := tx.QueryRow(`SELECT location_id, crop_id FROM farm_plots WHERE id = ?`, plotID).Scan(&plotLocation, &existingCrop); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("farm plot %q does not exist", plotID)
		}
		return nil, fmt.Errorf("load farm plot %q: %w", plotID, err)
	}
	if plotLocation != locationID {
		return nil, fmt.Errorf("farm plot %q is not at the character's location", plotID)
	}
	if existingCrop.Valid {
		return nil, fmt.Errorf("farm plot %q is occupied", plotID)
	}

	var crop FarmCrop
	if err := tx.QueryRow(`
		SELECT c.id, c.name, c.seed_item_id, si.name, c.harvest_item_id, hi.name, c.growth_ticks, c.yield_quantity
		FROM farm_crops c
		JOIN items si ON si.id = c.seed_item_id
		JOIN items hi ON hi.id = c.harvest_item_id
		WHERE c.id = ?
	`, cropID).Scan(&crop.ID, &crop.Name, &crop.SeedItemID, &crop.SeedItemName,
		&crop.HarvestItemID, &crop.HarvestItemName, &crop.GrowthTicks, &crop.YieldQuantity); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("crop %q is not available", cropID)
		}
		return nil, fmt.Errorf("load crop %q: %w", cropID, err)
	}

	result, err := tx.Exec(`
		UPDATE inventories SET quantity = quantity - ?
		WHERE character_id = ? AND item_id = ? AND quantity >= ?
	`, 1, characterID, crop.SeedItemID, 1)
	if err != nil {
		return nil, fmt.Errorf("consume planting seed: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check planting seed inventory: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("character does not have %s", crop.SeedItemName)
	}
	if _, err := tx.Exec(`DELETE FROM inventories WHERE character_id = ? AND item_id = ? AND quantity <= 0`, characterID, crop.SeedItemID); err != nil {
		return nil, fmt.Errorf("remove depleted seed stack: %w", err)
	}

	matureTick := currentTick + uint64(crop.GrowthTicks)
	result, err = tx.Exec(`
		UPDATE farm_plots SET crop_id = ?, planted_tick = ?, mature_tick = ?
		WHERE id = ? AND location_id = ? AND crop_id IS NULL
	`, crop.ID, currentTick, matureTick, plotID, locationID)
	if err != nil {
		return nil, fmt.Errorf("plant farm plot: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check planted farm plot: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("farm plot %q is no longer available", plotID)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit planting: %w", err)
	}
	return &FarmActionResult{PlotID: plotID, LocationID: locationID, CropID: crop.ID, CropName: crop.Name, MatureTick: matureTick}, nil
}

// HarvestFarmPlot harvests a mature communal plot into inventory, or onto the ground if the character is full.
func HarvestFarmPlot(database *sql.DB, characterID, plotID string, currentTick uint64) (*FarmActionResult, error) {
	tx, err := database.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin harvest transaction: %w", err)
	}
	defer tx.Rollback()

	var locationID, status string
	if err := tx.QueryRow(`SELECT current_location, status FROM characters WHERE id = ?`, characterID).Scan(&locationID, &status); err != nil {
		return nil, fmt.Errorf("character %q not found: %w", characterID, err)
	}
	if status != "IDLE" {
		return nil, fmt.Errorf("character is busy: %s", status)
	}

	var plotLocation string
	var cropID sql.NullString
	var matureTick uint64
	if err := tx.QueryRow(`SELECT location_id, crop_id, mature_tick FROM farm_plots WHERE id = ?`, plotID).Scan(&plotLocation, &cropID, &matureTick); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("farm plot %q does not exist", plotID)
		}
		return nil, fmt.Errorf("load farm plot %q: %w", plotID, err)
	}
	if plotLocation != locationID {
		return nil, fmt.Errorf("farm plot %q is not at the character's location", plotID)
	}
	if !cropID.Valid {
		return nil, fmt.Errorf("farm plot %q is empty", plotID)
	}
	if currentTick < matureTick {
		return nil, fmt.Errorf("crop in farm plot %q is not mature until tick %d", plotID, matureTick)
	}

	var cropName, itemID, itemName string
	var quantity int
	if err := tx.QueryRow(`
		SELECT c.name, c.harvest_item_id, i.name, c.yield_quantity
		FROM farm_crops c
		JOIN items i ON i.id = c.harvest_item_id
		WHERE c.id = ?
	`, cropID.String).Scan(&cropName, &itemID, &itemName, &quantity); err != nil {
		return nil, fmt.Errorf("load planted crop %q: %w", cropID.String, err)
	}

	result, err := tx.Exec(`
		UPDATE farm_plots SET crop_id = NULL, planted_tick = 0, mature_tick = 0
		WHERE id = ? AND location_id = ? AND crop_id = ? AND mature_tick <= ?
	`, plotID, locationID, cropID.String, currentTick)
	if err != nil {
		return nil, fmt.Errorf("clear harvested farm plot: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("check harvested farm plot: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("farm plot %q is no longer ready to harvest", plotID)
	}

	weightInfo, err := GetCharacterWeightInfo(tx, characterID)
	if err != nil {
		return nil, fmt.Errorf("check harvest carrying capacity: %w", err)
	}
	var itemWeight float64
	if err := tx.QueryRow(`SELECT weight_kg FROM items WHERE id = ?`, itemID).Scan(&itemWeight); err != nil {
		return nil, fmt.Errorf("load harvest item weight: %w", err)
	}
	var existingQuantity int
	err = tx.QueryRow(`SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?`, characterID, itemID).Scan(&existingQuantity)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("check harvest inventory: %w", err)
	}
	canCarry := weightInfo.TotalWeightKg+float64(quantity)*itemWeight <= weightInfo.OverburdenLimitKg
	if existingQuantity == 0 && weightInfo.SlotsUsed >= weightInfo.MaxSlots {
		canCarry = false
	}

	if canCarry {
		if _, err := tx.Exec(`
			INSERT INTO inventories (character_id, item_id, quantity) VALUES (?, ?, ?)
			ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + excluded.quantity
		`, characterID, itemID, quantity); err != nil {
			return nil, fmt.Errorf("add harvest to inventory: %w", err)
		}
	} else {
		if _, err := tx.Exec(`
			INSERT INTO location_items (location_id, item_id, quantity, dropped_by, dropped_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(location_id, item_id) DO UPDATE SET quantity = quantity + excluded.quantity
		`, locationID, itemID, quantity, characterID); err != nil {
			return nil, fmt.Errorf("place harvest on ground: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit harvest: %w", err)
	}
	return &FarmActionResult{
		PlotID: plotID, LocationID: locationID, CropID: cropID.String, CropName: cropName, ItemID: itemID,
		ItemName: itemName, Quantity: quantity, PlacedOnGround: !canCarry,
	}, nil
}
