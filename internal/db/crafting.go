package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
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
	RecipeID        string           `json:"recipe_id"`
	RecipeName      string           `json:"recipe_name"`
	TicksSpent      int              `json:"ticks_spent"`
	EnergyDeducted  int              `json:"energy_deducted"`
	SkillID         string           `json:"skill_id"`
	SkillXPGained   int              `json:"skill_xp_gained"`
	SkillLevel      int              `json:"skill_level"`
	PreviousLevel   int              `json:"previous_skill_level"`
	SkillExperience int              `json:"skill_experience"`
	Yield           []CraftYieldItem `json:"yield"`
	TotalWeightKg   float64          `json:"total_weight_kg"`
	MaxCapacityKg   float64          `json:"max_capacity_kg"`
	NarrativeLog    string           `json:"narrative_log"`
}

var DefaultWorkstations = []Workstation{
	{ID: "millstones", Name: "Water-Driven Millstones", LocationID: "town_mill", EquipmentPresent: "Water-driven mill wheels", CraftingDomain: "Grain processing (Wheat -> Flour)"},
	{ID: "stone_hearth", Name: "Stone Hearth Oven", LocationID: "bakery", EquipmentPresent: "Large stone baking oven", CraftingDomain: "Bread, roasted meats, herbal infusions"},
	{ID: "forge_anvil", Name: "Forge & Anvil", LocationID: "blacksmith", EquipmentPresent: "Coal forge, anvils, quench tanks", CraftingDomain: "Tools, metal hardware, nails, horseshoes"},
	{ID: "brew_cask", Name: "Fermentation Cask", LocationID: "tavern", EquipmentPresent: "Fermentation casks & presses", CraftingDomain: "Ale, cider, fruit wine"},
	{ID: "apothecary_kit", Name: "Apothecary Lab", LocationID: "herbalist", EquipmentPresent: "Mortar & pestle, alembic", CraftingDomain: "Remedies, tinctures, dried herbs"},
	{ID: "timber_saw", Name: "Timber Pit Saw", LocationID: "lumberjack_lodge", EquipmentPresent: "Pit saws, shaping benches", CraftingDomain: "Planks, beams, firewood bundles"},
	{ID: "carpenter_bench", Name: "Carpenter's Workbench", LocationID: "lumberjack_lodge", EquipmentPresent: "Carving benches, hand saws, planes", CraftingDomain: "Wooden furniture, wheelbarrows, buckets, bowls"},
	{ID: "clerk_desk", Name: "Town Clerk Desk", LocationID: "town_hall", EquipmentPresent: "Writing desk, ledger, brass stamp", CraftingDomain: "Deeds, permits, municipal documents"},
}

var DefaultRecipes = []Recipe{
	{
		ID:              "brew_ale",
		Name:            "Brewing Village Ale",
		RequiredStation: "brew_cask",
		OutputItemID:    "ale_mug",
		OutputQuantity:  3,
		DurationTicks:   12,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wheat", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 2},
		},
	},
	{
		ID:              "roast_mutton",
		Name:            "Roasting Mutton",
		RequiredStation: "stone_hearth",
		OutputItemID:    "roasted_mutton",
		OutputQuantity:  2,
		DurationTicks:   10,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "raw_mutton", Quantity: 2},
			{ItemID: "firewood", Quantity: 1},
		},
	},
	{
		ID:              "brew_willowbark_tea",
		Name:            "Brewing Willowbark Tea",
		RequiredStation: "apothecary_kit",
		OutputItemID:    "willowbark_tea",
		OutputQuantity:  2,
		DurationTicks:   8,
		EnergyCost:      4,
		RequiredSkillID: "herbalism",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "medicinal_herbs", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 1},
		},
	},
	{
		ID:              "brew_charcoal_draught",
		Name:            "Preparing Activated Charcoal Draught",
		RequiredStation: "apothecary_kit",
		OutputItemID:    "charcoal_draught",
		OutputQuantity:  2,
		DurationTicks:   10,
		EnergyCost:      5,
		RequiredSkillID: "herbalism",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "coal", Quantity: 1},
			{ItemID: "medicinal_herbs", Quantity: 1},
			{ItemID: "fresh_water", Quantity: 1},
		},
	},
	{
		ID:              "craft_wheelbarrow",
		Name:            "Crafting Wheelbarrow",
		RequiredStation: "carpenter_bench",
		OutputItemID:    "wheelbarrow",
		OutputQuantity:  1,
		DurationTicks:   20,
		EnergyCost:      15,
		RequiredSkillID: "crafting",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "wood_log", Quantity: 2},
			{ItemID: "iron_ingot", Quantity: 1},
		},
	},
	{
		ID:              "craft_bucket",
		Name:            "Crafting Wooden Bucket",
		RequiredStation: "carpenter_bench",
		OutputItemID:    "bucket",
		OutputQuantity:  1,
		DurationTicks:   8,
		EnergyCost:      6,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wood_log", Quantity: 1},
		},
	},
	{
		ID:              "craft_stool",
		Name:            "Crafting Wooden Stool",
		RequiredStation: "carpenter_bench",
		OutputItemID:    "wooden_stool",
		OutputQuantity:  1,
		DurationTicks:   10,
		EnergyCost:      8,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wood_log", Quantity: 1},
		},
	},
	{
		ID:              "craft_bowl",
		Name:            "Carving Wooden Bowl",
		RequiredStation: "carpenter_bench",
		OutputItemID:    "wooden_bowl",
		OutputQuantity:  2,
		DurationTicks:   4,
		EnergyCost:      3,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wood_log", Quantity: 1},
		},
	},
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
		MinSkillLevel:   2,
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
		MinSkillLevel:   2,
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
		MinSkillLevel:   3,
		Ingredients: []RecipeIngredient{
			{ItemID: "elderflowers", Quantity: 4},
			{ItemID: "honey_jar", Quantity: 1},
			{ItemID: "fresh_water", Quantity: 2},
		},
	},
	{
		ID:              "brew_herbed_ale",
		Name:            "Brewing Herbed Meadow Ale",
		RequiredStation: "brew_cask",
		OutputItemID:    "ale_mug",
		OutputQuantity:  3,
		DurationTicks:   12,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "wild_hops", Quantity: 1},
			{ItemID: "wheat", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 2},
		},
	},
	{
		ID:              "brew_blackberry_ale",
		Name:            "Brewing Fruity Blackberry Ale",
		RequiredStation: "brew_cask",
		OutputItemID:    "ale_mug",
		OutputQuantity:  3,
		DurationTicks:   12,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "blackberries", Quantity: 2},
			{ItemID: "wheat", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 2},
		},
	},
	{
		ID:              "brew_apple_cider",
		Name:            "Brewing Hard Apple Cider",
		RequiredStation: "brew_cask",
		OutputItemID:    "berry_wine",
		OutputQuantity:  2,
		DurationTicks:   14,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "wild_apples", Quantity: 3},
			{ItemID: "sugar", Quantity: 1},
		},
	},
	{
		ID:              "brew_chamomile_tea",
		Name:            "Brewing Calming Chamomile Tea",
		RequiredStation: "apothecary_kit",
		OutputItemID:    "willowbark_tea",
		OutputQuantity:  2,
		DurationTicks:   8,
		EnergyCost:      4,
		RequiredSkillID: "herbalism",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "chamomile", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 1},
		},
	},
	{
		ID:              "herb_roast_mutton",
		Name:            "Herb-Roasted Mutton",
		RequiredStation: "stone_hearth",
		OutputItemID:    "roasted_mutton",
		OutputQuantity:  2,
		DurationTicks:   10,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "raw_mutton", Quantity: 2},
			{ItemID: "culinary_herbs", Quantity: 1},
			{ItemID: "firewood", Quantity: 1},
		},
	},
	{
		ID:              "cure_mutton",
		Name:            "Salt-Cured Mutton",
		RequiredStation: "stone_hearth",
		OutputItemID:    "roasted_mutton",
		OutputQuantity:  2,
		DurationTicks:   12,
		EnergyCost:      4,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "raw_mutton", Quantity: 2},
			{ItemID: "salt", Quantity: 1},
		},
	},
	{
		ID:              "bake_berry_tart",
		Name:            "Baking Sweet Berry Tart",
		RequiredStation: "stone_hearth",
		OutputItemID:    "bread",
		OutputQuantity:  2,
		DurationTicks:   12,
		EnergyCost:      6,
		RequiredSkillID: "crafting",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "flour", Quantity: 1},
			{ItemID: "blackberries", Quantity: 2},
			{ItemID: "butter", Quantity: 1},
			{ItemID: "eggs", Quantity: 1},
		},
	},
	{
		ID:              "bake_honey_cake",
		Name:            "Baking Honey Cake",
		RequiredStation: "stone_hearth",
		OutputItemID:    "bread",
		OutputQuantity:  2,
		DurationTicks:   15,
		EnergyCost:      8,
		RequiredSkillID: "crafting",
		MinSkillLevel:   3,
		Ingredients: []RecipeIngredient{
			{ItemID: "flour", Quantity: 1},
			{ItemID: "honey_jar", Quantity: 1},
			{ItemID: "butter", Quantity: 1},
			{ItemID: "eggs", Quantity: 1},
			{ItemID: "sugar", Quantity: 1},
		},
	},
	{
		ID:              "craft_healing_salve",
		Name:            "Crafting Healing Salve",
		RequiredStation: "apothecary_kit",
		OutputItemID:    "charcoal_draught",
		OutputQuantity:  1,
		DurationTicks:   10,
		EnergyCost:      5,
		RequiredSkillID: "herbalism",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "medicinal_herbs", Quantity: 2},
			{ItemID: "beeswax", Quantity: 1},
		},
	},
	{
		ID:              "brew_framboise_ale",
		Name:            "Brewing Framboise Berry Ale",
		RequiredStation: "brew_cask",
		OutputItemID:    "framboise_ale",
		OutputQuantity:  3,
		DurationTicks:   14,
		EnergyCost:      6,
		RequiredSkillID: "crafting",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "blackberries", Quantity: 2},
			{ItemID: "wild_hops", Quantity: 1},
			{ItemID: "wheat", Quantity: 2},
			{ItemID: "fresh_water", Quantity: 2},
		},
	},
	{
		ID:              "craft_ceramic_bowl",
		Name:            "Firing Ceramic Bowls",
		RequiredStation: "stone_hearth",
		OutputItemID:    "ceramic_bowl",
		OutputQuantity:  2,
		DurationTicks:   10,
		EnergyCost:      5,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "clay_lumps", Quantity: 2},
			{ItemID: "firewood", Quantity: 1},
		},
	},
	{
		ID:              "craft_clay_jug",
		Name:            "Firing Clay Jug",
		RequiredStation: "stone_hearth",
		OutputItemID:    "clay_jug",
		OutputQuantity:  1,
		DurationTicks:   12,
		EnergyCost:      6,
		RequiredSkillID: "crafting",
		MinSkillLevel:   2,
		Ingredients: []RecipeIngredient{
			{ItemID: "clay_lumps", Quantity: 3},
			{ItemID: "firewood", Quantity: 1},
		},
	},
	{
		ID:              "mill_wild_grain",
		Name:            "Milling Wild Grain into Flour",
		RequiredStation: "millstones",
		OutputItemID:    "flour",
		OutputQuantity:  1,
		DurationTicks:   6,
		EnergyCost:      3,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "wild_grain", Quantity: 2},
		},
	},
	{
		ID:              "draft_official_permit",
		Name:            "Drafting Official Town Permit",
		RequiredStation: "clerk_desk",
		OutputItemID:    "official_permit",
		OutputQuantity:  1,
		DurationTicks:   10,
		EnergyCost:      4,
		RequiredSkillID: "crafting",
		MinSkillLevel:   1,
		Ingredients: []RecipeIngredient{
			{ItemID: "parchment", Quantity: 1},
			{ItemID: "ink_bottle", Quantity: 1},
			{ItemID: "official_seal", Quantity: 1}, // Non-consumable reusable tool
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

func ensureMillingData(database *sql.DB) error {
	for _, itemID := range []string{"wheat", "flour"} {
		if err := ensureFarmItem(database, itemID); err != nil {
			return fmt.Errorf("ensure milling item %q: %w", itemID, err)
		}
	}

	foundStation := false
	for _, station := range DefaultWorkstations {
		if station.ID != "millstones" {
			continue
		}
		foundStation = true
		if _, err := database.Exec(`
			INSERT INTO workstations (id, name, location_id, equipment_present, crafting_domain)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, station.ID, station.Name, station.LocationID, station.EquipmentPresent, station.CraftingDomain); err != nil {
			return fmt.Errorf("ensure milling workstation: %w", err)
		}
		break
	}
	if !foundStation {
		return fmt.Errorf("default millstones workstation is missing")
	}

	foundRecipe := false
	for _, recipe := range DefaultRecipes {
		if recipe.ID != "mill_flour" {
			continue
		}
		foundRecipe = true
		if _, err := database.Exec(`
			INSERT INTO recipes (
				id, name, required_station, output_item_id, output_quantity, duration_ticks,
				energy_cost, required_skill_id, min_skill_level
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, recipe.ID, recipe.Name, recipe.RequiredStation, recipe.OutputItemID, recipe.OutputQuantity,
			recipe.DurationTicks, recipe.EnergyCost, recipe.RequiredSkillID, recipe.MinSkillLevel); err != nil {
			return fmt.Errorf("ensure milling recipe: %w", err)
		}
		for _, ingredient := range recipe.Ingredients {
			if _, err := database.Exec(`
				INSERT INTO recipe_ingredients (recipe_id, item_id, quantity)
				VALUES (?, ?, ?)
				ON CONFLICT(recipe_id, item_id) DO NOTHING
			`, recipe.ID, ingredient.ItemID, ingredient.Quantity); err != nil {
				return fmt.Errorf("ensure ingredient %q for milling recipe: %w", ingredient.ItemID, err)
			}
		}
		break
	}
	if !foundRecipe {
		return fmt.Errorf("default mill_flour recipe is missing")
	}
	return nil
}

func initializeMillingRecipeKnowledge(database *sql.DB) error {
	for _, characterID := range []string{"eldon_millwright", "scotus"} {
		var occupation string
		err := database.QueryRow(`SELECT occupation FROM characters WHERE id = ?`, characterID).Scan(&occupation)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return fmt.Errorf("query occupation for %s: %w", characterID, err)
		}
		if err := InitializeCharacterProgression(database, characterID, occupation); err != nil {
			return fmt.Errorf("initialize progression for %s: %w", characterID, err)
		}
	}
	return nil
}

// InitializeCharacterProgression seeds baseline skills and starting specialty recipe knowledge.
// Level 1 recipes are open to all characters regardless of occupation; higher-tier recipes can be
// unlocked by reaching the required skill level or through mentorship via TeachRecipe.
func InitializeCharacterProgression(q Queryable, characterID, occupation string) error {
	for _, skillID := range []string{"foraging", "woodcutting", "herbalism", "crafting"} {
		if _, err := execQueryable(q, `
			INSERT INTO character_skills (character_id, skill_id, level, experience)
			VALUES (?, ?, 1, 0)
			ON CONFLICT(character_id, skill_id) DO NOTHING
		`, characterID, skillID); err != nil {
			return fmt.Errorf("seed %s skill: %w", skillID, err)
		}
	}

	var specialtyRecipes []string
	role := strings.ToLower(occupation)
	switch {
	case strings.Contains(role, "barkeep"):
		specialtyRecipes = append(specialtyRecipes, "brew_elderflower_wine")
	case strings.Contains(role, "blacksmith"), strings.Contains(role, "smith"):
		specialtyRecipes = append(specialtyRecipes, "forge_hammer")
	case strings.Contains(role, "herbalist"), strings.Contains(role, "apothecary"):
		specialtyRecipes = append(specialtyRecipes, "brew_elderflower_syrup", "brew_charcoal_draught")
	case strings.Contains(role, "carpenter"), strings.Contains(role, "lumberjack"):
		specialtyRecipes = append(specialtyRecipes, "craft_wheelbarrow")
	}

	for _, recipeID := range specialtyRecipes {
		if _, err := execQueryable(q, `
			INSERT INTO character_known_recipes (character_id, recipe_id) VALUES (?, ?)
			ON CONFLICT(character_id, recipe_id) DO NOTHING
		`, characterID, recipeID); err != nil {
			return fmt.Errorf("seed recipe knowledge for %s: %w", recipeID, err)
		}
	}
	return nil
}

func InitializeAllCharacterProgression(database *sql.DB) error {
	rows, err := database.Query("SELECT id, occupation FROM characters ORDER BY id")
	if err != nil {
		return fmt.Errorf("query characters for progression initialization: %w", err)
	}
	type character struct {
		id         string
		occupation string
	}
	var characters []character
	for rows.Next() {
		var c character
		if err := rows.Scan(&c.id, &c.occupation); err != nil {
			rows.Close()
			return fmt.Errorf("scan character for progression initialization: %w", err)
		}
		characters = append(characters, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read characters for progression initialization: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close progression character query: %w", err)
	}
	for _, c := range characters {
		if err := InitializeCharacterProgression(database, c.id, c.occupation); err != nil {
			return fmt.Errorf("initialize progression for %s: %w", c.id, err)
		}
	}
	return nil
}

// CanAccessRecipe reports whether a character can attempt a recipe based on open basic craft rules,
// skill mastery, or learned specialty knowledge.
func CanAccessRecipe(q Queryable, characterID string, recipe *Recipe) (bool, string, error) {
	if recipe.MinSkillLevel <= 1 {
		return true, "", nil
	}

	var known int
	err := q.QueryRow("SELECT 1 FROM character_known_recipes WHERE character_id = ? AND recipe_id = ?", characterID, recipe.ID).Scan(&known)
	if err == nil {
		return true, "", nil
	}
	if err != sql.ErrNoRows {
		return false, "failed to check recipe knowledge", err
	}

	skillID := recipe.RequiredSkillID
	if skillID == "" {
		skillID = "crafting"
	}
	skillLvl, _, err := GetSkill(q, characterID, skillID)
	if err != nil {
		return false, fmt.Sprintf("failed to load %s skill", skillID), err
	}
	if skillLvl >= recipe.MinSkillLevel {
		return true, "", nil
	}

	return false, fmt.Sprintf("requires %s level %d or mentorship from someone who knows '%s'", skillID, recipe.MinSkillLevel, recipe.Name), nil
}

// TeachRecipe allows a knowledgeable character to teach a specialty recipe to another character in the same location.
func TeachRecipe(db *sql.DB, teacherID, studentID, recipeID string) error {
	if teacherID == studentID {
		return fmt.Errorf("cannot teach a recipe to yourself")
	}

	recipe, err := GetRecipe(db, recipeID)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin teaching transaction: %w", err)
	}
	defer tx.Rollback()

	var teacherLoc, teacherStatus string
	if err := tx.QueryRow("SELECT current_location, status FROM characters WHERE id = ?", teacherID).Scan(&teacherLoc, &teacherStatus); err != nil {
		return fmt.Errorf("teacher '%s' not found: %w", teacherID, err)
	}
	if teacherStatus != "IDLE" {
		return fmt.Errorf("teacher is currently %s", teacherStatus)
	}

	var studentLoc, studentStatus string
	if err := tx.QueryRow("SELECT current_location, status FROM characters WHERE id = ?", studentID).Scan(&studentLoc, &studentStatus); err != nil {
		return fmt.Errorf("student '%s' not found: %w", studentID, err)
	}
	if studentStatus != "IDLE" {
		return fmt.Errorf("student is currently %s", studentStatus)
	}

	if teacherLoc != studentLoc {
		return fmt.Errorf("teacher and student must be in the same location")
	}

	canTeach, _, err := CanAccessRecipe(tx, teacherID, recipe)
	if err != nil {
		return err
	}
	if !canTeach {
		return fmt.Errorf("teacher does not know recipe '%s'", recipe.Name)
	}

	if _, err := tx.Exec(`
		INSERT INTO character_known_recipes (character_id, recipe_id)
		VALUES (?, ?)
		ON CONFLICT(character_id, recipe_id) DO NOTHING
	`, studentID, recipeID); err != nil {
		return fmt.Errorf("failed to record learned recipe: %w", err)
	}

	return tx.Commit()
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

// CanCraftRecipe checks workstation spatial grounding, recipe access, ingredients, status, and character energy.
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

	// 3. Skill / Knowledge access check
	canAccess, reason, err := CanAccessRecipe(db, characterID, recipe)
	if err != nil {
		return false, reason, err
	}
	if !canAccess {
		return false, reason, nil
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

	// Deduct ingredients immediately from character inventory (skip reusable tools and seals)
	for _, ing := range recipe.Ingredients {
		var curQty int
		var isReusable int
		err := tx.QueryRow("SELECT i.quantity, COALESCE(it.is_reusable, 0) FROM inventories i JOIN items it ON i.item_id = it.id WHERE character_id = ? AND item_id = ?", characterID, ing.ItemID).Scan(&curQty, &isReusable)
		if err != nil || curQty < ing.Quantity {
			return fmt.Errorf("insufficient ingredients for %s", ing.ItemID)
		}

		if isReusable == 1 {
			continue // Do not consume reusable tools or seals
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
	skillID := recipe.RequiredSkillID
	if skillID == "" {
		skillID = "crafting"
	}
	skillLevel, _, err := GetSkill(tx, characterID, skillID)
	if err != nil {
		return fmt.Errorf("failed to load %s skill: %w", skillID, err)
	}
	duration := ScaleDurationForSkill(recipe.DurationTicks, skillLevel)
	endTick := startTick + uint64(duration)
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
func ResolveCrafting(db *sql.DB, characterID, recipeID string, elapsedTicks ...int) (*CraftResolutionResult, error) {
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

	// 3. Award Skill XP
	skillID := recipe.RequiredSkillID
	if skillID == "" {
		skillID = "crafting"
	}
	previousLevel, _, err := GetSkill(db, characterID, skillID)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s skill: %w", skillID, err)
	}
	xpGained := recipe.DurationTicks / 2
	if xpGained < 5 {
		xpGained = 5
	}
	skillLevel, skillExperience, err := AddSkillXP(db, characterID, skillID, xpGained)
	if err != nil {
		return nil, fmt.Errorf("failed to award %s skill experience: %w", skillID, err)
	}

	// 4. Deduct Energy
	_, _ = db.Exec("UPDATE characters SET energy = MAX(0, energy - ?) WHERE id = ?", recipe.EnergyCost, characterID)

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

	ticksSpent := ScaleDurationForSkill(recipe.DurationTicks, previousLevel)
	if len(elapsedTicks) > 0 && elapsedTicks[0] > 0 {
		ticksSpent = elapsedTicks[0]
	}
	narrativeLog := fmt.Sprintf("You crafted %dx %s at %s.", recipe.OutputQuantity, outputName, locName)

	return &CraftResolutionResult{
		RecipeID:        recipe.ID,
		RecipeName:      recipe.Name,
		TicksSpent:      ticksSpent,
		EnergyDeducted:  recipe.EnergyCost,
		SkillID:         skillID,
		SkillXPGained:   xpGained,
		SkillLevel:      skillLevel,
		PreviousLevel:   previousLevel,
		SkillExperience: skillExperience,
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
