package db

import (
	"database/sql"
	"fmt"
	"log"
)

type Workstation struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	LocationID       string `json:"location_id"`
	EquipmentPresent string `json:"equipment_present"`
	CraftingDomain   string `json:"crafting_domain"`
}

type RecipeIngredient struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type Recipe struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	RequiredStation string             `json:"required_station"` // Empty if craftable anywhere
	OutputItemID    string             `json:"output_item_id"`
	OutputQuantity  int                `json:"output_quantity"`
	DurationTicks   int                `json:"duration_ticks"`
	EnergyCost      int                `json:"energy_cost"`
	RequiredSkillID string             `json:"required_skill_id"`
	MinSkillLevel   int                `json:"min_skill_level"`
	Ingredients     []RecipeIngredient `json:"ingredients"`
}

type CraftYieldItem struct {
	ItemID   string  `json:"item_id"`
	Quantity int     `json:"quantity"`
	WeightKg float64 `json:"weight_kg"`
}

type CraftResolutionResult struct {
	RecipeID       string           `json:"recipe_id"`
	RecipeName     string           `json:"recipe_name"`
	TicksSpent     int              `json:"ticks_spent"`
	EnergyDeducted int              `json:"energy_deducted"`
	SkillXPGained  int              `json:"skill_xp_gained"`
	Yield          []CraftYieldItem `json:"yield"`
	TotalWeightKg  float64          `json:"total_weight_kg"`
	MaxCapacityKg  float64          `json:"max_capacity_kg"`
	NarrativeLog   string           `json:"narrative_log"`
}

var DefaultWorkstations = []Workstation{
	{ID: "millstones", Name: "Water-Driven Millstones", LocationID: "town_mill", EquipmentPresent: "Water-driven mill wheels", CraftingDomain: "Grain processing (Wheat -> Flour)"},
	{ID: "stone_hearth", Name: "Stone Hearth Oven", LocationID: "bakery", EquipmentPresent: "Large stone baking oven", CraftingDomain: "Bread, pies, rations"},
	{ID: "forge_anvil", Name: "Forge & Anvil", LocationID: "blacksmith", EquipmentPresent: "Coal forge, anvils, quench tanks", CraftingDomain: "Tools, metal hardware, nails, horseshoes"},
	{ID: "brew_cask", Name: "Fermentation Cask", LocationID: "tavern", EquipmentPresent: "Fermentation casks & presses", CraftingDomain: "Ale, cider, fruit wine"},
	{ID: "apothecary_kit", Name: "Apothecary Lab", LocationID: "herbalist", EquipmentPresent: "Mortar & pestle, alembic", CraftingDomain: "Remedies, tinctures, dried herbs"},
	{ID: "timber_saw", Name: "Timber Pit Saw", LocationID: "lumberjack_lodge", EquipmentPresent: "Pit saws, shaping benches", CraftingDomain: "Planks, beams, firewood bundles"},
}

var DefaultRecipes = []Recipe{
	{
		ID:              "mill_flour",
		Name:            "Milling Flour",
		RequiredStation: "millstones",
		OutputItemID:    "flour",
		OutputQuantity:  2,
		DurationTicks:   4,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wheat", Quantity: 2},
		},
	},
	{
		ID:              "bake_bread",
		Name:            "Baking Bread",
		RequiredStation: "stone_hearth",
		OutputItemID:    "bread",
		OutputQuantity:  3,
		DurationTicks:   8,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "flour", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 1},
		},
	},
	{
		ID:              "smelt_iron",
		Name:            "Smelting Iron",
		RequiredStation: "forge_anvil",
		OutputItemID:    "iron_ingot",
		OutputQuantity:  1,
		DurationTicks:   10,
		EnergyCost:      8,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "iron_ore", Quantity: 2},
			{ItemID: "coal", Quantity: 1},
		},
	},
	{
		ID:              "forge_hammer",
		Name:            "Forging Smithing Hammer",
		RequiredStation: "forge_anvil",
		OutputItemID:    "smithing_hammer",
		OutputQuantity:  1,
		DurationTicks:   12,
		EnergyCost:      10,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "iron_ingot", Quantity: 1},
			{ItemID: "firewood", Quantity: 1},
		},
	},
	{
		ID:              "brew_berry_wine",
		Name:            "Brewing Berry Wine",
		RequiredStation: "brew_cask",
		OutputItemID:    "berry_wine",
		OutputQuantity:  2,
		DurationTicks:   15,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "field_berries", Quantity: 3},
			{ItemID: "fresh_water", Quantity: 1},
		},
	},
	{
		ID:              "weave_basket",
		Name:            "Basket Weaving",
		RequiredStation: "", // Craftable anywhere without fixed station
		OutputItemID:    "woven_basket",
		OutputQuantity:  1,
		DurationTicks:   8,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "river_reeds", Quantity: 4},
		},
	},
	{
		ID:              "saw_firewood",
		Name:            "Sawing Firewood",
		RequiredStation: "timber_saw",
		OutputItemID:    "firewood",
		OutputQuantity:  4,
		DurationTicks:   6,
		EnergyCost:      6,
		RequiredSkillID: "woodcutting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wood_log", Quantity: 1},
		},
	},
	{
		ID:              "brew_elderflower_syrup",
		Name:            "Brewing Spiced Elderflower Syrup",
		RequiredStation: "apothecary_kit",
		OutputItemID:    "spiced_elderflower_syrup",
		OutputQuantity:  2,
		DurationTicks:   12,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "elderflowers", Quantity: 3},
			{ItemID: "honey_jar", Quantity: 1},
			{ItemID: "fresh_water", Quantity: 1},
		},
	},
	{
		ID:              "brew_mint_tea",
		Name:            "Brewing Wild Mint Tea",
		RequiredStation: "stone_hearth",
		OutputItemID:    "mint_tea",
		OutputQuantity:  2,
		DurationTicks:   5,
		EnergyCost:      3,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wild_mint", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 1},
		},
	},
	{
		ID:              "brew_elderflower_wine",
		Name:            "Brewing Elderflower Wine",
		RequiredStation: "brew_cask",
		OutputItemID:    "elderflower_wine",
		OutputQuantity:  2,
		DurationTicks:   15,
		EnergyCost:      6,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "elderflowers", Quantity: 4},
			{ItemID: "honey_jar", Quantity: 1},
			{ItemID: "fresh_water", Quantity: 2},
		},
	},
}

// SeedCrafting initializes default workstations and recipe registries in SQLite.
func SeedCrafting(db *sql.DB) error {
	// 1. Seed Workstations
	wsQuery := `
	INSERT INTO workstations (id, name, location_id, equipment_present, crafting_domain)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		location_id=excluded.location_id,
		equipment_present=excluded.equipment_present,
		crafting_domain=excluded.crafting_domain;
	`
	wsStmt, err := db.Prepare(wsQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare workstation seed statement: %w", err)
	}
	defer wsStmt.Close()

	for _, ws := range DefaultWorkstations {
		_, err := wsStmt.Exec(ws.ID, ws.Name, ws.LocationID, ws.EquipmentPresent, ws.CraftingDomain)
		if err != nil {
			return fmt.Errorf("failed to seed workstation %s: %w", ws.ID, err)
		}
	}

	// 2. Seed Recipes
	recQuery := `
	INSERT INTO recipes (id, name, required_station, output_item_id, output_quantity, duration_ticks, energy_cost, required_skill_id, min_skill_level)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		required_station=excluded.required_station,
		output_item_id=excluded.output_item_id,
		output_quantity=excluded.output_quantity,
		duration_ticks=excluded.duration_ticks,
		energy_cost=excluded.energy_cost,
		required_skill_id=excluded.required_skill_id,
		min_skill_level=excluded.min_skill_level;
	`
	recStmt, err := db.Prepare(recQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare recipe seed statement: %w", err)
	}
	defer recStmt.Close()

	ingQuery := `
	INSERT INTO recipe_ingredients (recipe_id, item_id, quantity)
	VALUES (?, ?, ?)
	ON CONFLICT(recipe_id, item_id) DO UPDATE SET
		quantity=excluded.quantity;
	`
	ingStmt, err := db.Prepare(ingQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare recipe ingredient seed statement: %w", err)
	}
	defer ingStmt.Close()

	for _, rec := range DefaultRecipes {
		var stationVal *string
		if rec.RequiredStation != "" {
			stationVal = &rec.RequiredStation
		}
		_, err := recStmt.Exec(rec.ID, rec.Name, stationVal, rec.OutputItemID, rec.OutputQuantity, rec.DurationTicks, rec.EnergyCost, rec.RequiredSkillID, rec.MinSkillLevel)
		if err != nil {
			return fmt.Errorf("failed to seed recipe %s: %w", rec.ID, err)
		}

		for _, ing := range rec.Ingredients {
			_, err := ingStmt.Exec(rec.ID, ing.ItemID, ing.Quantity)
			if err != nil {
				return fmt.Errorf("failed to seed ingredient %s for recipe %s: %w", ing.ItemID, rec.ID, err)
			}
		}
	}

	log.Printf("Successfully seeded %d workstations and %d crafting recipes.", len(DefaultWorkstations), len(DefaultRecipes))
	return nil
}

// GetRecipe fetches a recipe and its required ingredients by ID.
func GetRecipe(db *sql.DB, recipeID string) (*Recipe, error) {
	var r Recipe
	var station sql.NullString
	var skill sql.NullString
	err := db.QueryRow(`
		SELECT id, name, required_station, output_item_id, output_quantity, duration_ticks, energy_cost, required_skill_id, min_skill_level
		FROM recipes WHERE id = ?
	`, recipeID).Scan(&r.ID, &r.Name, &station, &r.OutputItemID, &r.OutputQuantity, &r.DurationTicks, &r.EnergyCost, &skill, &r.MinSkillLevel)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("recipe '%s' not found", recipeID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query recipe: %w", err)
	}

	if station.Valid {
		r.RequiredStation = station.String
	}
	if skill.Valid {
		r.RequiredSkillID = skill.String
	}

	rows, err := db.Query("SELECT item_id, quantity FROM recipe_ingredients WHERE recipe_id = ?", recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query recipe ingredients: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ing RecipeIngredient
		if err := rows.Scan(&ing.ItemID, &ing.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan ingredient: %w", err)
		}
		r.Ingredients = append(r.Ingredients, ing)
	}

	return &r, nil
}

// CanCraftRecipe checks workstation spatial grounding, ingredients, status, and character energy.
func CanCraftRecipe(db *sql.DB, characterID, recipeID string) (bool, string, error) {
	recipe, err := GetRecipe(db, recipeID)
	if err != nil {
		return false, err.Error(), nil
	}

	// 1. Character Status and Energy check
	var status, charLocation string
	var energy int
	err = db.QueryRow("SELECT status, current_location, energy FROM characters WHERE id = ?", characterID).Scan(&status, &charLocation, &energy)
	if err != nil {
		return false, "character not found", err
	}

	if status != "IDLE" {
		return false, fmt.Sprintf("character is currently %s", status), nil
	}

	if energy < recipe.EnergyCost {
		return false, fmt.Sprintf("insufficient energy: have %d, recipe requires %d", energy, recipe.EnergyCost), nil
	}

	// 2. Spatial Workstation check
	if recipe.RequiredStation != "" {
		var stationLoc string
		err := db.QueryRow("SELECT location_id FROM workstations WHERE id = ?", recipe.RequiredStation).Scan(&stationLoc)
		if err != nil {
			return false, fmt.Sprintf("workstation '%s' is not registered", recipe.RequiredStation), nil
		}
		if charLocation != stationLoc {
			return false, fmt.Sprintf("recipe requires workstation '%s' located at '%s' (currently at '%s')", recipe.RequiredStation, stationLoc, charLocation), nil
		}
	}

	// 3. Skill Level check
	if recipe.RequiredSkillID != "" && recipe.MinSkillLevel > 1 {
		skillLvl, _, _ := GetSkill(db, characterID, recipe.RequiredSkillID)
		if skillLvl < recipe.MinSkillLevel {
			return false, fmt.Sprintf("skill '%s' level %d required (currently level %d)", recipe.RequiredSkillID, recipe.MinSkillLevel, skillLvl), nil
		}
	}

	// 4. Ingredient Availability check
	for _, ing := range recipe.Ingredients {
		var availableQty int
		err := db.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, ing.ItemID).Scan(&availableQty)
		if err == sql.ErrNoRows || availableQty < ing.Quantity {
			var itemName string
			_ = db.QueryRow("SELECT name FROM items WHERE id = ?", ing.ItemID).Scan(&itemName)
			if itemName == "" {
				itemName = ing.ItemID
			}
			return false, fmt.Sprintf("missing required ingredient: %dx %s", ing.Quantity, itemName), nil
		}
	}

	return true, "", nil
}

// StartCrafting validates preconditions, commits required ingredients, and locks character in CRAFTING state.
func StartCrafting(db *sql.DB, characterID, recipeID, intentID string, startTick uint64) error {
	canCraft, reason, err := CanCraftRecipe(db, characterID, recipeID)
	if err != nil {
		return err
	}
	if !canCraft {
		return fmt.Errorf("%s", reason)
	}

	recipe, err := GetRecipe(db, recipeID)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Deduct ingredients immediately from character inventory
	for _, ing := range recipe.Ingredients {
		var curQty int
		err := tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, ing.ItemID).Scan(&curQty)
		if err != nil || curQty < ing.Quantity {
			return fmt.Errorf("insufficient ingredients for %s", ing.ItemID)
		}

		if curQty == ing.Quantity {
			_, err = tx.Exec("DELETE FROM inventories WHERE character_id = ? AND item_id = ?", characterID, ing.ItemID)
		} else {
			_, err = tx.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", ing.Quantity, characterID, ing.ItemID)
		}
		if err != nil {
			return fmt.Errorf("failed to deduct ingredient %s: %w", ing.ItemID, err)
		}
	}

	// Action Lock character: status = 'CRAFTING'
	endTick := startTick + uint64(recipe.DurationTicks)
	_, err = tx.Exec(`
		UPDATE characters 
		SET status = 'CRAFTING', action_start_tick = ?, action_end_tick = ?, action_target = ?, action_intent_id = ?
		WHERE id = ?
	`, startTick, endTick, recipeID, intentID, characterID)
	if err != nil {
		return fmt.Errorf("failed to update character action lock: %w", err)
	}

	return tx.Commit()
}

// CancelCrafting aborts ongoing crafting, refunds deducted ingredients, and sets status back to IDLE.
func CancelCrafting(db *sql.DB, characterID string) error {
	var status, recipeID string
	err := db.QueryRow("SELECT status, action_target FROM characters WHERE id = ?", characterID).Scan(&status, &recipeID)
	if err != nil {
		return fmt.Errorf("character not found: %w", err)
	}

	if status != "CRAFTING" {
		return fmt.Errorf("character is not currently crafting")
	}

	recipe, err := GetRecipe(db, recipeID)
	if err != nil {
		return fmt.Errorf("failed to get recipe for refund: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Refund ingredients
	for _, ing := range recipe.Ingredients {
		_, err := tx.Exec(`
			INSERT INTO inventories (character_id, item_id, quantity)
			VALUES (?, ?, ?)
			ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
		`, characterID, ing.ItemID, ing.Quantity, ing.Quantity)
		if err != nil {
			return fmt.Errorf("failed to refund ingredient %s: %w", ing.ItemID, err)
		}
	}

	// Reset character to IDLE
	_, err = tx.Exec(`
		UPDATE characters 
		SET status = 'IDLE', action_start_tick = 0, action_end_tick = 0, action_target = '', action_intent_id = ''
		WHERE id = ?
	`, characterID)
	if err != nil {
		return fmt.Errorf("failed to reset character status: %w", err)
	}

	return tx.Commit()
}

// ResolveCrafting completes the recipe, awards output items, deducts energy, grants skill XP, and resets status.
func ResolveCrafting(db *sql.DB, characterID, recipeID string) (*CraftResolutionResult, error) {
	recipe, err := GetRecipe(db, recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}

	// 1. Query output item details
	var outputName string
	var outputWeight float64
	err = db.QueryRow("SELECT name, weight_kg FROM items WHERE id = ?", recipe.OutputItemID).Scan(&outputName, &outputWeight)
	if err != nil {
		outputName = recipe.OutputItemID
		outputWeight = 0.5
	}

	// 2. Query character location and name
	var charLoc string
	err = db.QueryRow("SELECT current_location FROM characters WHERE id = ?", characterID).Scan(&charLoc)
	if err != nil {
		return nil, fmt.Errorf("character not found: %w", err)
	}

	var locName string
	_ = db.QueryRow("SELECT name FROM locations WHERE id = ?", charLoc).Scan(&locName)
	if locName == "" {
		locName = charLoc
	}

	// 3. Deduct Energy
	_, _ = db.Exec("UPDATE characters SET energy = MAX(0, energy - ?) WHERE id = ?", recipe.EnergyCost, characterID)

	// 4. Award Skill XP
	skillID := recipe.RequiredSkillID
	if skillID == "" {
		skillID = "crafting"
	}
	xpGained := recipe.DurationTicks / 2
	if xpGained < 5 {
		xpGained = 5
	}
	_, _, _ = AddSkillXP(db, characterID, skillID, xpGained)

	// 5. Add output items to inventory (or ground if overburdened)
	wInfo, _ := GetCharacterWeightInfo(db, characterID)
	addedWeight := float64(recipe.OutputQuantity) * outputWeight

	canCarry := true
	if wInfo != nil {
		if wInfo.TotalWeightKg+addedWeight > wInfo.OverburdenLimitKg {
			canCarry = false
		}
		var existingQty int
		err := db.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, recipe.OutputItemID).Scan(&existingQty)
		if (err == sql.ErrNoRows || existingQty == 0) && wInfo.SlotsUsed >= wInfo.MaxSlots {
			canCarry = false
		}
	}

	if canCarry {
		_, _ = db.Exec(`
			INSERT INTO inventories (character_id, item_id, quantity)
			VALUES (?, ?, ?)
			ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
		`, characterID, recipe.OutputItemID, recipe.OutputQuantity, recipe.OutputQuantity)
	} else {
		// Output placed on ground
		_, _ = db.Exec(`
			INSERT INTO location_items (location_id, item_id, quantity, dropped_by, dropped_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(location_id, item_id) DO UPDATE SET quantity = quantity + ?
		`, charLoc, recipe.OutputItemID, recipe.OutputQuantity, characterID, recipe.OutputQuantity)
	}

	// 6. Reset character to IDLE
	_, _ = db.Exec(`
		UPDATE characters 
		SET status = 'IDLE', action_start_tick = 0, action_end_tick = 0, action_target = '', action_intent_id = ''
		WHERE id = ?
	`, characterID)

	// 7. Final weight info
	finalWInfo, _ := GetCharacterWeightInfo(db, characterID)
	finalWeight := 0.0
	maxCap := BaseUnassistedCapacityKg
	if finalWInfo != nil {
		finalWeight = finalWInfo.TotalWeightKg
		maxCap = finalWInfo.MaxCapacityKg
	}

	narrativeLog := fmt.Sprintf("You crafted %dx %s at %s.", recipe.OutputQuantity, outputName, locName)

	return &CraftResolutionResult{
		RecipeID:       recipe.ID,
		RecipeName:     recipe.Name,
		TicksSpent:     recipe.DurationTicks,
		EnergyDeducted: recipe.EnergyCost,
		SkillXPGained:  xpGained,
		Yield: []CraftYieldItem{
			{
				ItemID:   recipe.OutputItemID,
				Quantity: recipe.OutputQuantity,
				WeightKg: outputWeight,
			},
		},
		TotalWeightKg: finalWeight,
		MaxCapacityKg: maxCap,
		NarrativeLog:  narrativeLog,
	}, nil
}
