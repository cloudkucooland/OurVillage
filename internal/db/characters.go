package db

import (
	"database/sql"
	"fmt"
	"log"
)

type ItemSeed struct {
	ID            string
	Name          string
	Description   string
	IsConsumable  bool
	HungerRestore int
}

type CharacterSeed struct {
	ID             string
	Name           string
	PersonaPrompt  string
	Occupation     string
	Gold           int
	Hunger         int
	Location       string
	ControllerType string // "agent" or "password" / "human"
	MQTTPassword   string
	Inventory      map[string]int // item_id -> quantity
}

var DefaultItems = []ItemSeed{
	{ID: "wheat", Name: "Sack of Wheat", Description: "Raw grain ready to be milled into flour.", IsConsumable: false, HungerRestore: 0},
	{ID: "flour", Name: "Bag of Flour", Description: "Fine milled flour ready for baking bread.", IsConsumable: false, HungerRestore: 0},
	{ID: "bread", Name: "Fresh Loaf of Bread", Description: "Warm, crusty bread that restores hunger.", IsConsumable: true, HungerRestore: 25},
	{ID: "ale_mug", Name: "Mug of Ale", Description: "Foamy, refreshing village ale.", IsConsumable: true, HungerRestore: 10},
	{ID: "roasted_mutton", Name: "Roasted Mutton", Description: "Hearty cooked meat.", IsConsumable: true, HungerRestore: 50},
	{ID: "iron_wrench", Name: "Iron Wrench", Description: "A sturdy tool for maintaining mill machinery.", IsConsumable: false, HungerRestore: 0},
	{ID: "iron_ingot", Name: "Iron Ingot", Description: "Raw metal for forging tools and weapons.", IsConsumable: false, HungerRestore: 0},
	{ID: "smithing_hammer", Name: "Smithing Hammer", Description: "Heavy hammer for blacksmithing work.", IsConsumable: false, HungerRestore: 0},
	{ID: "coal", Name: "Lump of Coal", Description: "Fuel for the forge and heaters.", IsConsumable: false, HungerRestore: 0},
	{ID: "vegetable_basket", Name: "Basket of Vegetables", Description: "Fresh carrots, potatoes, and greens.", IsConsumable: true, HungerRestore: 15},
	{ID: "parchment", Name: "Parchment & Quill", Description: "Materials for official clerk records and letters.", IsConsumable: false, HungerRestore: 0},
	{ID: "ink_bottle", Name: "Bottle of Black Ink", Description: "Standard writing ink.", IsConsumable: false, HungerRestore: 0},
	{ID: "official_seal", Name: "Official Town Seal", Description: "Brass stamp used to authenticate municipal deeds.", IsConsumable: false, HungerRestore: 0},
}

var DefaultCharacters = []CharacterSeed{
	{
		ID:             "eldon_millwright",
		Name:           "Eldon the Millwright",
		PersonaPrompt:  "Eldon is a hardworking, pragmatic millwright who runs the town mill. He values fair trade, punctuality, and good wheat.",
		Occupation:     "Millwright",
		Gold:           15,
		Hunger:         90,
		Location:       "town_mill",
		ControllerType: "agent",
		MQTTPassword:   "secret_eldon_123",
		Inventory:      map[string]int{"wheat": 4, "flour": 2, "iron_wrench": 1},
	},
	{
		ID:             "martha_barkeep",
		Name:           "Martha the Barkeep",
		PersonaPrompt:  "Martha is the sharp-witted owner of The Boar's Tusk Tavern. She knows every village rumor and keeps a close eye on her ale casks.",
		Occupation:     "Barkeep",
		Gold:           30,
		Hunger:         100,
		Location:       "tavern",
		ControllerType: "agent",
		MQTTPassword:   "secret_martha_123",
		Inventory:      map[string]int{"ale_mug": 10, "roasted_mutton": 5, "bread": 3},
	},
	{
		ID:             "gideon_forge",
		Name:           "Gideon the Blacksmith",
		PersonaPrompt:  "Gideon is a burly, taciturn smith who creates durable tools and weapons. He respects honest physical labor above all.",
		Occupation:     "Blacksmith",
		Gold:           25,
		Hunger:         85,
		Location:       "blacksmith",
		ControllerType: "agent",
		MQTTPassword:   "secret_gideon_123",
		Inventory:      map[string]int{"iron_ingot": 3, "smithing_hammer": 1, "coal": 2},
	},
	{
		ID:             "luke_farmer",
		Name:           "Luke the Farmer",
		PersonaPrompt:  "Luke is an optimistic tenant farmer who works the fields outside town, always worrying about the weather and crop yields.",
		Occupation:     "Farmer",
		Gold:           12,
		Hunger:         75,
		Location:       "market",
		ControllerType: "agent",
		MQTTPassword:   "secret_luke_123",
		Inventory:      map[string]int{"wheat": 10, "vegetable_basket": 2},
	},
	{
		ID:             "eleanor_clerk",
		Name:           "Eleanor the Town Clerk",
		PersonaPrompt:  "Eleanor is meticulous and bureaucratic, managing property deeds, store licenses, and election ballots with strict adherence to town code.",
		Occupation:     "Town Clerk",
		Gold:           40,
		Hunger:         100,
		Location:       "town_hall",
		ControllerType: "human", // Can be played by a human player!
		MQTTPassword:   "secret_eleanor_123",
		Inventory:      map[string]int{"parchment": 5, "ink_bottle": 5, "official_seal": 1},
	},
}

// SeedItemsAndCharacters seeds items, initial characters, and their starting inventories.
func SeedItemsAndCharacters(db *sql.DB) error {
	// Seed Items
	itemQuery := `
	INSERT INTO items (id, name, description, is_consumable, hunger_restore)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		description=excluded.description,
		is_consumable=excluded.is_consumable,
		hunger_restore=excluded.hunger_restore;
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
		_, err := itemStmt.Exec(item.ID, item.Name, item.Description, consumableVal, item.HungerRestore)
		if err != nil {
			return fmt.Errorf("failed to seed item %s: %w", item.ID, err)
		}
	}

	// Seed Characters
	charQuery := `
	INSERT INTO characters (id, name, persona_prompt, occupation, gold, hunger, current_location, controller_type, mqtt_password)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		persona_prompt=excluded.persona_prompt,
		occupation=excluded.occupation,
		gold=excluded.gold,
		hunger=excluded.hunger,
		current_location=excluded.current_location,
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

	for _, char := range DefaultCharacters {
		_, err := charStmt.Exec(char.ID, char.Name, char.PersonaPrompt, char.Occupation, char.Gold, char.Hunger, char.Location, char.ControllerType, char.MQTTPassword)
		if err != nil {
			return fmt.Errorf("failed to seed character %s: %w", char.ID, err)
		}

		for itemId, qty := range char.Inventory {
			_, err := invStmt.Exec(char.ID, itemId, qty)
			if err != nil {
				return fmt.Errorf("failed to seed inventory for character %s, item %s: %w", char.ID, itemId, err)
			}
		}
	}

	log.Printf("Successfully seeded %d items and %d characters with inventories.", len(DefaultItems), len(DefaultCharacters))
	return nil
}
