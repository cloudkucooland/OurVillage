package db

import (
	"database/sql"
	"fmt"
	"log"
)

type ItemSeed struct {
	ID             string
	Name           string
	Description    string
	WeightKg       float64
	IsConsumable   bool
	HungerRestore  int
	HealthRestore  int
	IsContainer    bool
	CapacityKg     float64
	SlotLimit      int
	IsPerishable   bool
	ShelfLifeTicks int
	SpoilageItemID string
}

type CharacterSeed struct {
	ID                  string
	Name                string
	PersonaPrompt       string
	Occupation          string
	Gold                int
	Hunger              int
	Energy              int
	Location            string
	HomeLocationID      string
	EquippedContainerID string
	ControllerType      string // "agent" or "password" / "human"
	MQTTPassword        string
	Inventory           map[string]int // item_id -> quantity
}

var DefaultItems = []ItemSeed{
	// Spoilage waste item (RFC 009)
	{ID: "rotting_waste", Name: "Rotting Waste", Description: "Spoiled organic matter emitting a foul odor.", WeightKg: 0.2, IsConsumable: false, HungerRestore: 0},

	// Existing village items with metric mass (RFC 007)
	{ID: "wheat", Name: "Sack of Wheat", Description: "Raw grain that can be planted or milled into flour.", WeightKg: 0.5, IsConsumable: false, HungerRestore: 0},
	{ID: "flour", Name: "Bag of Flour", Description: "Fine milled flour ready for baking bread.", WeightKg: 0.5, IsConsumable: false, HungerRestore: 0},
	{ID: "bread", Name: "Fresh Loaf of Bread", Description: "Warm, crusty bread that restores hunger.", WeightKg: 0.3, IsConsumable: true, HungerRestore: 25, IsPerishable: true, ShelfLifeTicks: 96, SpoilageItemID: "rotting_waste"},
	{ID: "ale_mug", Name: "Mug of Ale", Description: "Foamy, refreshing village ale.", WeightKg: 0.5, IsConsumable: true, HungerRestore: 10, IsPerishable: true, ShelfLifeTicks: 96, SpoilageItemID: "rotting_waste"},
	{ID: "roasted_mutton", Name: "Roasted Mutton", Description: "Hearty cooked meat.", WeightKg: 0.5, IsConsumable: true, HungerRestore: 50, IsPerishable: true, ShelfLifeTicks: 96, SpoilageItemID: "rotting_waste"},
	{ID: "iron_wrench", Name: "Iron Wrench", Description: "A sturdy tool for maintaining mill machinery.", WeightKg: 2.0, IsConsumable: false, HungerRestore: 0},
	{ID: "iron_ingot", Name: "Iron Ingot", Description: "Raw metal for forging tools and weapons.", WeightKg: 3.0, IsConsumable: false, HungerRestore: 0},
	{ID: "smithing_hammer", Name: "Smithing Hammer", Description: "Heavy hammer for blacksmithing work.", WeightKg: 2.5, IsConsumable: false, HungerRestore: 0},
	{ID: "coal", Name: "Lump of Coal", Description: "Fuel for the forge and heaters.", WeightKg: 1.0, IsConsumable: false, HungerRestore: 0},
	{ID: "vegetable_basket", Name: "Basket of Vegetables", Description: "Fresh carrots, potatoes, and greens.", WeightKg: 1.0, IsConsumable: true, HungerRestore: 15, IsPerishable: true, ShelfLifeTicks: 96, SpoilageItemID: "rotting_waste"},
	{ID: "vegetable_seeds", Name: "Vegetable Seeds", Description: "Seeds for planting a mixed vegetable crop in a communal field plot.", WeightKg: 0.1},
	{ID: "parchment", Name: "Parchment & Quill", Description: "Materials for official clerk records and letters.", WeightKg: 0.1, IsConsumable: false, HungerRestore: 0},
	{ID: "ink_bottle", Name: "Bottle of Black Ink", Description: "Standard writing ink.", WeightKg: 0.2, IsConsumable: false, HungerRestore: 0},
	{ID: "official_seal", Name: "Official Town Seal", Description: "Brass stamp used to authenticate municipal deeds.", WeightKg: 0.5, IsConsumable: false, HungerRestore: 0},

	// RFC 007 Container Gear
	{ID: "woven_basket", Name: "Woven Basket", Description: "Woven from river reeds; ideal for berries and herbs.", WeightKg: 0.5, IsContainer: true, CapacityKg: 30.0, SlotLimit: 8},
	{ID: "leather_backpack", Name: "Leather Backpack", Description: "Crafted by Tanner/Tailor; worn in back slot.", WeightKg: 1.5, IsContainer: true, CapacityKg: 45.0, SlotLimit: 12},
	{ID: "wheelbarrow", Name: "Wheelbarrow", Description: "Sturdy wooden wheelbarrow requiring two hands to push.", WeightKg: 8.0, IsContainer: true, CapacityKg: 100.0, SlotLimit: 20},
	{ID: "bucket", Name: "Wooden Bucket", Description: "A sturdy wooden bucket bound with iron strips.", WeightKg: 1.5, IsContainer: true, CapacityKg: 10.0, SlotLimit: 2},
	{ID: "wooden_stool", Name: "Wooden Stool", Description: "A simple, hand-crafted wooden stool for sitting.", WeightKg: 3.0},
	{ID: "wooden_bowl", Name: "Wooden Bowl", Description: "A smooth carved wooden bowl for food or broth.", WeightKg: 0.5},
	{ID: "draft_horse_cart", Name: "Draft Horse & Cart", Description: "A heavy cart hitched to a draft horse for hauling bulk goods.", WeightKg: 45.0, IsContainer: true, CapacityKg: 350.0, SlotLimit: 40},

	// RFC 007 Foraging Primary and Secondary Items
	{ID: "firewood", Name: "Firewood Bundle", Description: "Dry split logs essential for heating and cooking.", WeightKg: 1.0},
	{ID: "wood_log", Name: "Wood Log", Description: "Heavy raw timber felled from trees.", WeightKg: 5.0},
	{ID: "logs", Name: "Logs", Description: "Heavy raw timber felled from trees.", WeightKg: 5.0},
	{ID: "wild_mushrooms", Name: "Wild Mushrooms", Description: "Edible forest fungi gathered from damp decaying bark.", WeightKg: 0.2, IsConsumable: true, HungerRestore: 5, IsPerishable: true, ShelfLifeTicks: 96, SpoilageItemID: "rotting_waste"},
	{ID: "bird_feather", Name: "Bird Feather", Description: "A soft plumage fallen from the overhead forest canopy.", WeightKg: 0.05},
	{ID: "animal_fodder", Name: "Animal Fodder", Description: "Dry grasses and roughage suitable for feeding livestock.", WeightKg: 0.5},
	{ID: "wild_grain", Name: "Wild Grain", Description: "Uncultivated cereal grains gathered from the prairie.", WeightKg: 0.5},
	{ID: "field_berries", Name: "Field Berries", Description: "Sweet, juicy wild berries picked from meadow brambles.", WeightKg: 0.2, IsConsumable: true, HungerRestore: 10, IsPerishable: true, ShelfLifeTicks: 96, SpoilageItemID: "rotting_waste"},
	{ID: "medicinal_herbs", Name: "Medicinal Herbs", Description: "Fragrant wild herbs with soothing and healing properties.", WeightKg: 0.1},
	{ID: "clay_lumps", Name: "Clay Lumps", Description: "Malleable natural clay dug from the riverbed.", WeightKg: 1.0},
	{ID: "fresh_water", Name: "Fresh Water", Description: "Clean, cool drinking water drawn from the river.", WeightKg: 1.0, IsConsumable: true, HungerRestore: 5, IsPerishable: true, ShelfLifeTicks: 96, SpoilageItemID: "rotting_waste"},
	{ID: "freshwater_pearl", Name: "Freshwater Pearl", Description: "A rare, lustrous pearl discovered inside a riverbed mussel.", WeightKg: 0.05},
	{ID: "river_reeds", Name: "River Reeds", Description: "Long, pliable reeds harvested along the shoreline.", WeightKg: 0.2},

	// RFC 008 Crafting Items
	{ID: "iron_ore", Name: "Iron Ore", Description: "Raw unrefined iron rock dug from hillside quarries.", WeightKg: 2.0},
	{ID: "berry_wine", Name: "Jug of Berry Wine", Description: "Sweet fermented wine made from wild prairie berries.", WeightKg: 1.0, IsConsumable: true, HungerRestore: 10, IsPerishable: true, ShelfLifeTicks: 192, SpoilageItemID: "rotting_waste"},
	{ID: "elderflowers", Name: "Elderflower Blossoms", Description: "Delicate white elderflower blossoms picked from river hedgerows.", WeightKg: 0.1, IsConsumable: false, HungerRestore: 0},
	{ID: "wild_mint", Name: "Wild Mint Leaves", Description: "Fragrant fresh green mint leaves with a cooling aroma.", WeightKg: 0.05, IsConsumable: false, HungerRestore: 0},
	{ID: "honey_jar", Name: "Jar of Wildflower Honey", Description: "Golden, sweet honey harvested from forest beehives.", WeightKg: 0.5, IsConsumable: true, HungerRestore: 20, IsPerishable: true, ShelfLifeTicks: 192, SpoilageItemID: "rotting_waste"},
	{ID: "spiced_elderflower_syrup", Name: "Spiced Elderflower Syrup", Description: "Sweet, aromatic cordial brewed from elderflowers, honey, and river spices.", WeightKg: 0.5, IsConsumable: true, HungerRestore: 15},
	{ID: "mint_tea", Name: "Mug of Mint Tea", Description: "A warm, soothing herbal infusion of wild mint and fresh water.", WeightKg: 0.5, IsConsumable: true, HungerRestore: 10},
	{ID: "elderflower_wine", Name: "Jug of Elderflower Wine", Description: "Delicate floral fruit wine with hints of honey and elder blossoms.", WeightKg: 1.0, IsConsumable: true, HungerRestore: 15},
	{ID: "willowbark_tea", Name: "Willowbark Tea", Description: "A bitter herbal infusion used to reduce fevers and heal body wear.", WeightKg: 0.5, IsConsumable: true, HungerRestore: 5, HealthRestore: 20},
	{ID: "charcoal_draught", Name: "Activated Charcoal Draught", Description: "A medicinal draught to neutralize toxins and restore vitality.", WeightKg: 0.5, IsConsumable: true, HungerRestore: 5, HealthRestore: 25},
}

var DefaultCharacters = []CharacterSeed{
	{
		ID:                  "eldon_millwright",
		Name:                "Eldon the Millwright",
		PersonaPrompt:       "Eldon is a hardworking, pragmatic millwright who runs the town mill. He values fair trade, punctuality, and good wheat.",
		Occupation:          "Millwright",
		Gold:                15,
		Hunger:              90,
		Energy:              100,
		Location:            "town_mill",
		HomeLocationID:      "eldons_room",
		EquippedContainerID: "",
		ControllerType:      "agent",
		MQTTPassword:        "secret_eldon_123",
		Inventory:           map[string]int{"wheat": 4, "flour": 2, "iron_wrench": 1},
	},
	{
		ID:                  "martha_barkeep",
		Name:                "Martha the Barkeep",
		PersonaPrompt:       "Martha is the sharp-witted owner of The Boar's Tusk Tavern. She knows every village rumor and keeps a close eye on her ale casks.",
		Occupation:          "Barkeep",
		Gold:                30,
		Hunger:              100,
		Energy:              100,
		Location:            "tavern",
		HomeLocationID:      "marthas_room",
		EquippedContainerID: "",
		ControllerType:      "agent",
		MQTTPassword:        "secret_martha_123",
		Inventory:           map[string]int{"ale_mug": 10, "roasted_mutton": 5, "bread": 3},
	},
	{
		ID:                  "gideon_forge",
		Name:                "Gideon the Blacksmith",
		PersonaPrompt:       "Gideon is a burly, taciturn smith who creates durable tools and weapons. He respects honest physical labor above all.",
		Occupation:          "Blacksmith",
		Gold:                25,
		Hunger:              85,
		Energy:              100,
		Location:            "blacksmith",
		HomeLocationID:      "gideons_room",
		EquippedContainerID: "wheelbarrow",
		ControllerType:      "agent",
		MQTTPassword:        "secret_gideon_123",
		Inventory:           map[string]int{"iron_ingot": 3, "smithing_hammer": 1, "coal": 2, "wheelbarrow": 1},
	},
	{
		ID:                  "luke_farmer",
		Name:                "Luke the Farmer",
		PersonaPrompt:       "Luke is an optimistic tenant farmer who works the fields outside town, always worrying about the weather and crop yields.",
		Occupation:          "Farmer",
		Gold:                12,
		Hunger:              75,
		Energy:              100,
		Location:            "market",
		HomeLocationID:      "lukes_room",
		EquippedContainerID: "woven_basket",
		ControllerType:      "agent",
		MQTTPassword:        "secret_luke_123",
		Inventory:           map[string]int{"wheat": 10, "vegetable_seeds": 10, "vegetable_basket": 2, "woven_basket": 1},
	},
	{
		ID:                  "eleanor_clerk",
		Name:                "Eleanor the Town Clerk",
		PersonaPrompt:       "Eleanor is meticulous and bureaucratic, managing property deeds, store licenses, and election ballots with strict adherence to town code.",
		Occupation:          "Town Clerk",
		Gold:                40,
		Hunger:              100,
		Energy:              100,
		Location:            "town_hall",
		HomeLocationID:      "clerks_office",
		EquippedContainerID: "",
		ControllerType:      "human", // Can be played by a human player!
		MQTTPassword:        "secret_eleanor_123",
		Inventory:           map[string]int{"parchment": 5, "ink_bottle": 5, "official_seal": 1},
	},
}

// SeedItemsAndCharacters seeds items, initial characters, and their starting inventories.
func SeedItemsAndCharacters(db *sql.DB) error {
	// Seed Items
	itemQuery := `
	INSERT INTO items (id, name, description, weight_kg, is_consumable, hunger_restore, health_restore, is_container, capacity_kg, slot_limit, is_perishable, shelf_life_ticks, spoilage_item_id)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		description=excluded.description,
		weight_kg=excluded.weight_kg,
		is_consumable=excluded.is_consumable,
		hunger_restore=excluded.hunger_restore,
		health_restore=excluded.health_restore,
		is_container=excluded.is_container,
		capacity_kg=excluded.capacity_kg,
		slot_limit=excluded.slot_limit,
		is_perishable=excluded.is_perishable,
		shelf_life_ticks=excluded.shelf_life_ticks,
		spoilage_item_id=excluded.spoilage_item_id;
	`
	itemStmt, err := db.Prepare(itemQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare item seed statement: %w", err)
	}
	defer itemStmt.Close()

	for _, item := range DefaultItems {
		consumableVal := 0
		if item.IsConsumable {
			consumableVal = 1
		}
		containerVal := 0
		if item.IsContainer {
			containerVal = 1
		}
		perishableVal := 0
		if item.IsPerishable {
			perishableVal = 1
		}
		shelfLife := item.ShelfLifeTicks
		if shelfLife <= 0 && item.IsPerishable {
			shelfLife = 96
		}
		spoilageID := item.SpoilageItemID
		if spoilageID == "" {
			spoilageID = "rotting_waste"
		}
		_, err := itemStmt.Exec(item.ID, item.Name, item.Description, item.WeightKg, consumableVal, item.HungerRestore, item.HealthRestore, containerVal, item.CapacityKg, item.SlotLimit, perishableVal, shelfLife, spoilageID)
		if err != nil {
			return fmt.Errorf("failed to seed item %s: %w", item.ID, err)
		}
	}

	// Seed Characters
	charQuery := `
	INSERT INTO characters (id, name, persona_prompt, occupation, gold, hunger, energy, current_location, home_location_id, equipped_container_id, controller_type, mqtt_password)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		persona_prompt=excluded.persona_prompt,
		occupation=excluded.occupation,
		gold=excluded.gold,
		hunger=excluded.hunger,
		energy=excluded.energy,
		current_location=excluded.current_location,
		home_location_id=excluded.home_location_id,
		equipped_container_id=excluded.equipped_container_id,
		controller_type=excluded.controller_type,
		mqtt_password=excluded.mqtt_password;
	`
	charStmt, err := db.Prepare(charQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare character seed statement: %w", err)
	}
	defer charStmt.Close()

	invQuery := `
	INSERT INTO inventories (character_id, item_id, quantity)
	VALUES (?, ?, ?)
	ON CONFLICT(character_id, item_id) DO UPDATE SET
		quantity=excluded.quantity;
	`
	invStmt, err := db.Prepare(invQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare inventory seed statement: %w", err)
	}
	defer invStmt.Close()

	skillQuery := `
	INSERT INTO character_skills (character_id, skill_id, level, experience)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(character_id, skill_id) DO NOTHING;
	`
	skillStmt, err := db.Prepare(skillQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare character skill seed statement: %w", err)
	}
	defer skillStmt.Close()

	for _, char := range DefaultCharacters {
		var containerVal *string
		if char.EquippedContainerID != "" {
			containerVal = &char.EquippedContainerID
		}
		_, err := charStmt.Exec(char.ID, char.Name, char.PersonaPrompt, char.Occupation, char.Gold, char.Hunger, char.Energy, char.Location, char.HomeLocationID, containerVal, char.ControllerType, char.MQTTPassword)
		if err != nil {
			return fmt.Errorf("failed to seed character %s: %w", char.ID, err)
		}

		for itemId, qty := range char.Inventory {
			_, err := invStmt.Exec(char.ID, itemId, qty)
			if err != nil {
				return fmt.Errorf("failed to seed inventory for character %s, item %s: %w", char.ID, itemId, err)
			}
		}

		// Initialize default skills at level 1
		for _, skillID := range []string{"foraging", "woodcutting", "herbalism", "crafting"} {
			if _, err := skillStmt.Exec(char.ID, skillID, 1, 0); err != nil {
				return fmt.Errorf("failed to seed %s skill for character %s: %w", skillID, char.ID, err)
			}
		}
	}

	log.Printf("Successfully seeded %d items and %d characters with inventories.", len(DefaultItems), len(DefaultCharacters))
	return nil
}
