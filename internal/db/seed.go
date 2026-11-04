package db

import (
	"database/sql"
	"fmt"
	"log"
)

type LocationSeed struct {
	ID                string
	Name              string
	Description       string
	IsPublic          bool
	IsIndoor          bool
	VehicleAccessTier string // "ALL", "FOOT_AND_WHEELBARROW", "FOOT_ONLY"
	OpenTime          string
	CloseTime         string
	RulesPrompt       string
}

type ResourceSeed struct {
	LocationID          string
	ResourceID          string
	CurrentStock        int
	MaxStock            int
	ReplenishRatePerDay int
}

var DefaultLocations = []LocationSeed{
	{
		ID:                "town_square",
		Name:              "Town Square",
		Description:       "The bustling paved heart of the village where citizens gather, announcements are posted, and paths diverge.",
		IsPublic:          true,
		IsIndoor:          false,
		VehicleAccessTier: "ALL",
		OpenTime:          "00:00",
		CloseTime:         "23:59",
		RulesPrompt:       "Public property. Public announcements are permitted.",
	},
	{
		ID:                "market",
		Name:              "Village Market",
		Description:       "Open-air stalls smelling of spices, vegetables, and fresh goods. Merchants barter here under mayoral oversight.",
		IsPublic:          false,
		IsIndoor:          false,
		VehicleAccessTier: "ALL",
		OpenTime:          "06:00",
		CloseTime:         "18:00",
		RulesPrompt:       "Only trade-related dialogue and transactions during open hours. Mayor tax applies to all sales.",
	},
	{
		ID:                "church",
		Name:              "St. Jude's Church",
		Description:       "A quiet stone sanctuary with stained glass windows offering peace, reflection, and community assembly.",
		IsPublic:          true,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "08:00",
		CloseTime:         "20:00",
		RulesPrompt:       "Keep voices respectful. No commercial trading or aggressive negotiations allowed inside.",
	},
	{
		ID:                "school",
		Name:              "Village Schoolhouse",
		Description:       "A modest timber hall filled with writing slates and reading benches where the village youth gather.",
		IsPublic:          false,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "08:00",
		CloseTime:         "15:00",
		RulesPrompt:       "Quiet study and instruction during school hours. You may discuss in any language.",
	},
	{
		ID:                "town_hall",
		Name:              "Town Hall",
		Description:       "The administrative center housing the Clerk's office, voting registry, and mayoral chambers.",
		IsPublic:          false,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "09:00",
		CloseTime:         "17:00",
		RulesPrompt:       "Official business only. Respect municipal officials and adhere to election protocols.",
	},
	{
		ID:                "tavern",
		Name:              "The Boar's Tusk Tavern",
		Description:       "Warm, smoky, and loud. The center of village social life, offering hot meals, ale, and lodging beds upstairs.",
		IsPublic:          false,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "12:00",
		CloseTime:         "23:59",
		RulesPrompt:       "All patrons must pay for lodging and food.",
	},
	{
		ID:                "town_mill",
		Name:              "The Town Mill",
		Description:       "A water-driven mill by the river where sacks of wheat are ground into flour by the resident millwright.",
		IsPublic:          false,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "06:00",
		CloseTime:         "18:00",
		RulesPrompt:       "Milling fees apply. Business hours only.",
	},
	{
		ID:                "blacksmith",
		Name:              "Forge & Anvil Blacksmith",
		Description:       "Ringing with the sound of hammers and glowing with forge fire, tools and hardware are crafted here.",
		IsPublic:          false,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "07:00",
		CloseTime:         "17:00",
		RulesPrompt:       "Hot coals and heavy machinery. Do not touch unpurchased inventory.",
	},
	{
		ID:                "bakery",
		Name:              "Hearth & Crust Bakery",
		Description:       "Filled with the aroma of rising dough and baked loaves, providing daily sustenance to the village.",
		IsPublic:          false,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "05:00",
		CloseTime:         "16:00",
		RulesPrompt:       "Queue politely for bread purchase.",
	},
	{
		ID:                "lumberjack_lodge",
		Name:              "Pine Ridge Timber Lodge",
		Description:       "Stacked high with logs and axes, serving as the hub for firewood production and forest resources.",
		IsPublic:          false,
		IsIndoor:          false,
		VehicleAccessTier: "ALL",
		OpenTime:          "06:00",
		CloseTime:         "17:00",
		RulesPrompt:       "Safety first. Firewood bundles available for purchase, especially vital during winter.",
	},
	{
		ID:                "herbalist",
		Name:              "Rosemary's Apothecary",
		Description:       "Shelves lined with dried herbs, tinctures, and glass jars containing remedies gathered from the valley.",
		IsPublic:          false,
		IsIndoor:          true,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "08:00",
		CloseTime:         "18:00",
		RulesPrompt:       "Handle delicate mixtures with care. Consult the herbalist before sampling potions.",
	},
	{
		ID:                "stables",
		Name:              "Village Stables",
		Description:       "Straw-strewn stalls housing draft horses and carts used for hauling heavy goods across the valley.",
		IsPublic:          false,
		IsIndoor:          false,
		VehicleAccessTier: "ALL",
		OpenTime:          "05:00",
		CloseTime:         "21:00",
		RulesPrompt:       "Do not startle the animals. Stable fees apply for overnight mount boarding.",
	},
	// RFC 007 Outdoor Foraging Locations
	{
		ID:                "deep_forest",
		Name:              "Deep Forest",
		Description:       "A dense canopy of ancient oaks and pines where firewood, logs, mushrooms, and bird feathers abound.",
		IsPublic:          true,
		IsIndoor:          false,
		VehicleAccessTier: "FOOT_ONLY",
		OpenTime:          "00:00",
		CloseTime:         "23:59",
		RulesPrompt:       "Dense wilderness. Foraging is permitted",
	},
	{
		ID:                "prairie",
		Name:              "Sunlit Prairie",
		Description:       "Vast swaying grasslands rich with wild grain, animal fodder, sweet field berries, and medicinal herbs.",
		IsPublic:          true,
		IsIndoor:          false,
		VehicleAccessTier: "ALL",
		OpenTime:          "00:00",
		CloseTime:         "23:59",
		RulesPrompt:       "Open meadow under the sky. Free foraging for all citizens.",
	},
	{
		ID:                "riverbank",
		Name:              "Whispering Riverbank",
		Description:       "Gentle shoreline where fresh water flows, river reeds grow, and clay lumps can be dredged from the riverbed.",
		IsPublic:          true,
		IsIndoor:          false,
		VehicleAccessTier: "FOOT_AND_WHEELBARROW",
		OpenTime:          "00:00",
		CloseTime:         "23:59",
		RulesPrompt:       "Cool riverbank waters. Clean clay and fresh water free to collect.",
	},
}

// DefaultLocationResources seeds ecological resource pools for foraging (RFC 007 Section 8 & 9)
var DefaultLocationResources = []ResourceSeed{
	{LocationID: "deep_forest", ResourceID: "firewood", CurrentStock: 100, MaxStock: 100, ReplenishRatePerDay: 20},
	{LocationID: "deep_forest", ResourceID: "wood_log", CurrentStock: 50, MaxStock: 50, ReplenishRatePerDay: 10},
	{LocationID: "deep_forest", ResourceID: "logs", CurrentStock: 50, MaxStock: 50, ReplenishRatePerDay: 10}, // alias support
	{LocationID: "prairie", ResourceID: "animal_fodder", CurrentStock: 100, MaxStock: 100, ReplenishRatePerDay: 25},
	{LocationID: "prairie", ResourceID: "wild_grain", CurrentStock: 80, MaxStock: 80, ReplenishRatePerDay: 20},
	{LocationID: "riverbank", ResourceID: "clay_lumps", CurrentStock: 50, MaxStock: 50, ReplenishRatePerDay: 15},
	{LocationID: "riverbank", ResourceID: "fresh_water", CurrentStock: 200, MaxStock: 200, ReplenishRatePerDay: 50},
}

// SeedLocations inserts default locations into the database if they do not exist.
func SeedLocations(db *sql.DB) error {
	query := `
	INSERT INTO locations (id, name, description, is_public_property, is_indoor, vehicle_access_tier, open_time, close_time, rules_prompt)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name=excluded.name,
		description=excluded.description,
		is_public_property=excluded.is_public_property,
		is_indoor=excluded.is_indoor,
		vehicle_access_tier=excluded.vehicle_access_tier,
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
		indoorVal := 0
		if loc.IsIndoor {
			indoorVal = 1
		}
		_, err := stmt.Exec(loc.ID, loc.Name, loc.Description, pubVal, indoorVal, loc.VehicleAccessTier, loc.OpenTime, loc.CloseTime, loc.RulesPrompt)
		if err != nil {
			return fmt.Errorf("failed to seed location %s: %w", loc.ID, err)
		}
	}

	log.Printf("Successfully seeded %d village locations.", len(DefaultLocations))
	return nil
}

// SeedLocationResources populates initial ecological pools for foraging.
func SeedLocationResources(db *sql.DB) error {
	query := `
	INSERT INTO location_resources (location_id, resource_id, current_stock, max_stock, replenish_rate_per_day)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(location_id, resource_id) DO UPDATE SET
		max_stock=excluded.max_stock,
		replenish_rate_per_day=excluded.replenish_rate_per_day;
	`
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare resource seed statement: %w", err)
	}
	defer stmt.Close()

	for _, res := range DefaultLocationResources {
		_, err := stmt.Exec(res.LocationID, res.ResourceID, res.CurrentStock, res.MaxStock, res.ReplenishRatePerDay)
		if err != nil {
			return fmt.Errorf("failed to seed resource %s in %s: %w", res.ResourceID, res.LocationID, err)
		}
	}

	log.Printf("Successfully seeded %d location resource pools.", len(DefaultLocationResources))
	return nil
}

type ConnectionSeed struct {
	FromLocation string
	ToLocation   string
	TravelTicks  int
}

// SeedLocationConnections seeds graph connections and travel ticks between locations.
func SeedLocationConnections(db *sql.DB) error {
	query := `
	INSERT INTO location_connections (from_location_id, to_location_id, travel_ticks)
	VALUES (?, ?, ?)
	ON CONFLICT(from_location_id, to_location_id) DO UPDATE SET
		travel_ticks=excluded.travel_ticks;
	`
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare location connection seed statement: %w", err)
	}
	defer stmt.Close()

	hubLocations := []string{
		"town_square", "market", "tavern", "town_mill", "blacksmith",
		"bakery", "church", "school", "town_hall", "lumberjack_lodge",
		"herbalist", "stables",
	}

	// Connect all hub locations to each other (1 tick)
	for _, h1 := range hubLocations {
		for _, h2 := range hubLocations {
			if h1 != h2 {
				if _, err := stmt.Exec(h1, h2, 1); err != nil {
					return fmt.Errorf("failed to seed hub connection %s -> %s: %w", h1, h2, err)
				}
			}
		}
	}

	// Outer zone connections from prairie
	outerConnections := []struct {
		from  string
		to    string
		ticks int
	}{
		{"town_square", "prairie", 2},
		{"prairie", "town_square", 2},
		{"prairie", "deep_forest", 4},
		{"deep_forest", "prairie", 4},
		{"prairie", "riverbank", 3},
		{"riverbank", "prairie", 3},
		{"prairie", "mine", 5},
		{"mine", "prairie", 5},
	}

	for _, conn := range outerConnections {
		if _, err := stmt.Exec(conn.from, conn.to, conn.ticks); err != nil {
			return fmt.Errorf("failed to seed outer connection %s -> %s (ticks %d): %w", conn.from, conn.to, conn.ticks, err)
		}
	}

	log.Printf("Successfully seeded location graph connections.")
	return nil
}
