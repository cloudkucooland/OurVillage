package db

import (
	"database/sql"
	"fmt"
	"log"
)

type LocationSeed struct {
	ID                 string
	Name               string
	Description        string
	IsPublic           bool
	OpenTime           string
	CloseTime          string
	RulesPrompt        string
}

var DefaultLocations = []LocationSeed{
	{
		ID:          "town_square",
		Name:        "Town Square",
		Description: "The bustling paved heart of the village where citizens gather, announcements are posted, and paths diverge.",
		IsPublic:    true,
		OpenTime:    "00:00",
		CloseTime:   "23:59",
		RulesPrompt: "Public property. No fighting or blocking thoroughfares. Public preaching and announcements are permitted.",
	},
	{
		ID:          "market",
		Name:        "Village Market",
		Description: "Open-air stalls smelling of spices, vegetables, and fresh goods. Merchants barter here under mayoral oversight.",
		IsPublic:    false,
		OpenTime:    "06:00",
		CloseTime:   "18:00",
		RulesPrompt: "Only trade-related dialogue and transactions during open hours. Mayor tax applies to all sales.",
	},
	{
		ID:          "church",
		Name:        "St. Jude's Church",
		Description: "A quiet stone sanctuary with stained glass windows offering peace, reflection, and community assembly.",
		IsPublic:    true,
		OpenTime:    "08:00",
		CloseTime:   "20:00",
		RulesPrompt: "Keep voices respectful. No commercial trading or aggressive negotiations allowed inside.",
	},
	{
		ID:          "school",
		Name:        "Village Schoolhouse",
		Description: "A modest timber hall filled with writing slates and reading benches where the village youth gather.",
		IsPublic:    false,
		OpenTime:    "08:00",
		CloseTime:   "15:00",
		RulesPrompt: "Quiet study and instruction during school hours. Unauthorized loitering is discouraged.",
	},
	{
		ID:          "town_hall",
		Name:        "Town Hall",
		Description: "The administrative center housing the Clerk's office, voting registry, and mayoral chambers.",
		IsPublic:    false,
		OpenTime:    "09:00",
		CloseTime:   "17:00",
		RulesPrompt: "Official business only. Respect municipal officials and adhere to election protocols.",
	},
	{
		ID:          "tavern",
		Name:        "The Boar's Tusk Tavern",
		Description: "Warm, smoky, and loud. The center of village social life, offering hot meals, ale, and lodging beds upstairs.",
		IsPublic:    false,
		OpenTime:    "12:00",
		CloseTime:   "23:59",
		RulesPrompt: "All patrons must pay for lodging and food. Brawling will result in removal by curfew guards.",
	},
	{
		ID:          "town_mill",
		Name:        "The Town Mill",
		Description: "A water-driven mill by the river where sacks of wheat are ground into flour by the resident millwright.",
		IsPublic:    false,
		OpenTime:    "06:00",
		CloseTime:   "18:00",
		RulesPrompt: "Milling fees apply (1-2 gold per batch). Business hours only.",
	},
	{
		ID:          "blacksmith",
		Name:        "Forge & Anvil Blacksmith",
		Description: "Ringing with the sound of hammers and glowing with forge fire, tools and hardware are crafted here.",
		IsPublic:    false,
		OpenTime:    "07:00",
		CloseTime:   "17:00",
		RulesPrompt: "Hot coals and heavy machinery. Do not touch unpurchased inventory.",
	},
	{
		ID:          "bakery",
		Name:        "Hearth & Crust Bakery",
		Description: "Filled with the aroma of rising dough and baked loaves, providing daily sustenance to the village.",
		IsPublic:    false,
		OpenTime:    "05:00",
		CloseTime:   "16:00",
		RulesPrompt: "Queue politely for bread purchase. Priority given to daily grain suppliers.",
	},
	{
		ID:          "lumberjack_lodge",
		Name:        "Pine Ridge Timber Lodge",
		Description: "Stacked high with logs and axes, serving as the hub for firewood production and forest resources.",
		IsPublic:    false,
		OpenTime:    "06:00",
		CloseTime:   "17:00",
		RulesPrompt: "Safety first. Firewood bundles available for purchase, especially vital during winter.",
	},
	{
		ID:          "herbalist",
		Name:        "Rosemary's Apothecary",
		Description: "Shelves lined with dried herbs, tinctures, and glass jars containing remedies gathered from the valley.",
		IsPublic:    false,
		OpenTime:    "08:00",
		CloseTime:   "18:00",
		RulesPrompt: "Handle delicate mixtures with care. Consult the herbalist before sampling potions.",
	},
	{
		ID:          "stables",
		Name:        "Village Stables",
		Description: "Straw-strewn stalls housing draft horses and carts used for hauling heavy goods across the valley.",
		IsPublic:    false,
		OpenTime:    "05:00",
		CloseTime:   "21:00",
		RulesPrompt: "Do not startle the animals. Stable fees apply for overnight mount boarding.",
	},
}

// SeedLocations inserts default locations into the database if they do not exist.
func SeedLocations(db *sql.DB) error {
	query := `
	INSERT INTO locations (id, name, description, is_public_property, open_time, close_time, rules_prompt)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		description=excluded.description,
		is_public_property=excluded.is_public_property,
		open_time=excluded.open_time,
		close_time=excluded.close_time,
		rules_prompt=excluded.rules_prompt;
	`

	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare location seed statement: %w", err)
	}
	defer stmt.Close()

	for _, loc := range DefaultLocations {
		pubVal := 0
		if loc.IsPublic {
			pubVal = 1
		}
		_, err := stmt.Exec(loc.ID, loc.Name, loc.Description, pubVal, loc.OpenTime, loc.CloseTime, loc.RulesPrompt)
		if err != nil {
			return fmt.Errorf("failed to seed location %s: %w", loc.ID, err)
		}
	}

	log.Printf("Successfully seeded %d village locations.", len(DefaultLocations))
	return nil
}
