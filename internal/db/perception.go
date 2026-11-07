package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PerceptionData encapsulates complete spatial perception and character state for an agent.
type PerceptionData struct {
	Type          string                    `json:"type"` // "STATE_SYNC"
	Timestamp     time.Time                 `json:"timestamp"`
	Character     CharacterOverview         `json:"character"`
	Location      LocationOverview          `json:"location"`
	LocationGraph []LocationConnectionEntry `json:"location_graph"`
}

// CharacterOverview captures character attributes, encumbrance, and inventory.
type CharacterOverview struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	Occupation        string                `json:"occupation"`
	Gold              int                   `json:"gold"`
	Hunger            int                   `json:"hunger"`
	Energy            int                   `json:"energy"`
	Status            string                `json:"status"`
	CurrentLocation   string                `json:"current_location"`
	HomeLocationID    string                `json:"home_location_id,omitempty"`
	HomeLocationName  string                `json:"home_location_name,omitempty"`
	EquippedContainer string                `json:"equipped_container,omitempty"`
	CarryingWeightKg  float64               `json:"carrying_weight_kg"`
	MaxCapacityKg     float64               `json:"max_capacity_kg"`
	OverburdenLimitKg float64               `json:"overburden_limit_kg"`
	SlotsUsed         int                   `json:"slots_used"`
	MaxSlots          int                   `json:"max_slots"`
	Encumbrance       EncumbranceState      `json:"encumbrance"`
	MaxLimitKg        float64               `json:"max_limit_kg"`
	Skills            []CharacterSkillEntry `json:"skills"`
	Inventory         []InventoryItemEntry  `json:"inventory"`
}

type CharacterSkillEntry struct {
	SkillID       string `json:"skill_id"`
	Level         int    `json:"level"`
	Experience    int    `json:"experience"`
	XPIntoLevel   int    `json:"xp_into_level"`
	XPToNextLevel int    `json:"xp_to_next_level"`
}

// InventoryItemEntry captures an inventory item with mass.
type InventoryItemEntry struct {
	ItemID        string  `json:"item_id"`
	Name          string  `json:"name"`
	Quantity      int     `json:"quantity"`
	WeightKg      float64 `json:"weight_kg"`
	TotalWeightKg float64 `json:"total_weight_kg"`
}

// LocationOverview captures the surrounding physical room state.
type LocationOverview struct {
	ID                 string                      `json:"id"`
	Name               string                      `json:"name"`
	Description        string                      `json:"description"`
	RulesPrompt        string                      `json:"rules_prompt,omitempty"`
	CharactersPresent  []CharacterPublicPerception `json:"characters_present"`
	GroundItems        []GroundItemEntry           `json:"ground_items"`
	Workstations       []WorkstationEntry          `json:"workstations"`
	ResourcePools      []ResourcePoolEntry         `json:"resource_pools"`
	ConnectedLocations []ConnectedLocationEntry    `json:"connected_locations"`
	ShopItems          []ShopItemEntry             `json:"shop_items"`
	AvailableRecipes   []AvailableRecipeEntry      `json:"available_recipes"`
}

// AvailableRecipeEntry captures crafting recipes available at location workstations.
type AvailableRecipeEntry struct {
	RecipeID          string             `json:"recipe_id"`
	RecipeName        string             `json:"recipe_name"`
	OutputItemID      string             `json:"output_item_id"`
	OutputQuantity    int                `json:"output_quantity"`
	DurationTicks     int                `json:"duration_ticks"`
	BaseDurationTicks int                `json:"base_duration_ticks"`
	EnergyCost        int                `json:"energy_cost"`
	RequiredSkillID   string             `json:"required_skill_id,omitempty"`
	SkillLevel        int                `json:"skill_level,omitempty"`
	MinSkillLevel     int                `json:"min_skill_level,omitempty"`
	Ingredients       []RecipeIngredient `json:"ingredients"`
}

// ShopItemEntry captures items available for purchase at a location storefront.
type ShopItemEntry struct {
	ItemID    string  `json:"item_id"`
	Name      string  `json:"name"`
	PriceGold int     `json:"price_gold"`
	StockQty  int     `json:"stock_quantity"`
	WeightKg  float64 `json:"weight_kg"`
}

// ConnectedLocationEntry captures adjacent graph nodes and travel ticks.
type ConnectedLocationEntry struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	TravelTicks  int    `json:"travel_ticks"`
}

type LocationConnectionEntry struct {
	FromLocationID string `json:"from_location_id"`
	ToLocationID   string `json:"to_location_id"`
}

// CharacterPublicPerception captures the observable physical traits of another agent in the room.
type CharacterPublicPerception struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Occupation string `json:"occupation"`
	Appearance string `json:"appearance"` // Coarse narrative summary
	Equipped   string `json:"equipped"`   // Publicly visible container/gear
}

// GroundItemEntry represents an unburdened item on the location floor.
type GroundItemEntry struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// WorkstationEntry represents fixed crafting equipment in the location.
type WorkstationEntry struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CraftingDomain string `json:"crafting_domain"`
}

// ResourcePoolEntry represents ecological resource deposits in the location.
type ResourcePoolEntry struct {
	ResourceID   string `json:"resource_id"`
	CurrentStock int    `json:"current_stock"`
	MaxStock     int    `json:"max_stock"`
}

// GetCharacterPerception queries SQLite and constructs full perception state for the specified character.
func GetCharacterPerception(database *sql.DB, characterID string) (*PerceptionData, error) {
	// 1. Query Character Base Info
	var char CharacterOverview
	var equippedContainer sql.NullString

	err := database.QueryRow(`
		SELECT c.id, c.name, c.occupation, c.gold, c.hunger, c.energy,
		       c.current_location, c.status, c.home_location_id, COALESCE(home.name, ''), c.equipped_container_id
		FROM characters c
		LEFT JOIN locations home ON home.id = c.home_location_id
		WHERE c.id = ?
	`, characterID).Scan(
		&char.ID,
		&char.Name,
		&char.Occupation,
		&char.Gold,
		&char.Hunger,
		&char.Energy,
		&char.CurrentLocation,
		&char.Status,
		&char.HomeLocationID,
		&char.HomeLocationName,
		&equippedContainer,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("character %q not found", characterID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to query character: %w", err)
	}

	if equippedContainer.Valid {
		char.EquippedContainer = equippedContainer.String
	}

	// 2. Query Encumbrance & Carrying Weight
	weightInfo, err := GetCharacterWeightInfo(database, characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate weight info: %w", err)
	}
	char.CarryingWeightKg = weightInfo.TotalWeightKg
	char.MaxCapacityKg = weightInfo.MaxCapacityKg
	char.OverburdenLimitKg = weightInfo.OverburdenLimitKg
	char.SlotsUsed = weightInfo.SlotsUsed
	char.MaxSlots = weightInfo.MaxSlots
	char.Encumbrance = weightInfo.Encumbrance
	char.MaxLimitKg = weightInfo.OverburdenLimitKg

	skillRows, err := database.Query(`
		SELECT skill_id, level, experience FROM character_skills
		WHERE character_id = ? ORDER BY skill_id
	`, characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query character skills: %w", err)
	}
	defer skillRows.Close()
	char.Skills = make([]CharacterSkillEntry, 0)
	for skillRows.Next() {
		var skill CharacterSkillEntry
		if err := skillRows.Scan(&skill.SkillID, &skill.Level, &skill.Experience); err != nil {
			return nil, fmt.Errorf("failed to scan character skill: %w", err)
		}
		if skill.Level < 100 {
			skill.XPIntoLevel = skill.Experience % 100
			skill.XPToNextLevel = 100 - skill.XPIntoLevel
		}
		char.Skills = append(char.Skills, skill)
	}
	if err := skillRows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read character skills: %w", err)
	}

	// 3. Query Inventory Items
	invRows, err := database.Query(`
		SELECT inv.item_id, itm.name, inv.quantity, itm.weight_kg
		FROM inventories inv
		JOIN items itm ON inv.item_id = itm.id
		WHERE inv.character_id = ? AND inv.quantity > 0
		ORDER BY itm.name ASC
	`, characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query inventory: %w", err)
	}
	defer invRows.Close()

	char.Inventory = make([]InventoryItemEntry, 0)
	for invRows.Next() {
		var item InventoryItemEntry
		if err := invRows.Scan(&item.ItemID, &item.Name, &item.Quantity, &item.WeightKg); err != nil {
			return nil, fmt.Errorf("failed to scan inventory row: %w", err)
		}
		item.TotalWeightKg = float64(item.Quantity) * item.WeightKg
		char.Inventory = append(char.Inventory, item)
	}

	// 4. Query Location Info
	var loc LocationOverview
	var rulesPrompt sql.NullString
	err = database.QueryRow(`
		SELECT id, name, description, rules_prompt
		FROM locations
		WHERE id = ?
	`, char.CurrentLocation).Scan(
		&loc.ID,
		&loc.Name,
		&loc.Description,
		&rulesPrompt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query location %q: %w", char.CurrentLocation, err)
	}
	if rulesPrompt.Valid {
		loc.RulesPrompt = rulesPrompt.String
	}

	// Query Connected Locations (Spatial Graph Adjacency)
	connRows, err := database.Query(`
		SELECT lc.to_location_id, l.name, lc.travel_ticks
		FROM location_connections lc
		JOIN locations l ON lc.to_location_id = l.id
		WHERE lc.from_location_id = ?
		ORDER BY lc.travel_ticks ASC, l.name ASC
	`, char.CurrentLocation)
	if err == nil {
		defer connRows.Close()
		loc.ConnectedLocations = make([]ConnectedLocationEntry, 0)
		for connRows.Next() {
			var conn ConnectedLocationEntry
			if err := connRows.Scan(&conn.LocationID, &conn.LocationName, &conn.TravelTicks); err == nil {
				loc.ConnectedLocations = append(loc.ConnectedLocations, conn)
			}
		}
	}

	// 5. Query Other Characters Present in Room (Public Perception Descriptions)
	charRows, err := database.Query(`
		SELECT id
		FROM characters
		WHERE current_location = ? AND id != ? AND is_online = 1
		ORDER BY name ASC
	`, char.CurrentLocation, characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query characters in room: %w", err)
	}
	defer charRows.Close()

	loc.CharactersPresent = make([]CharacterPublicPerception, 0)
	for charRows.Next() {
		var otherID string
		if err := charRows.Scan(&otherID); err != nil {
			return nil, fmt.Errorf("failed to scan other character ID: %w", err)
		}

		pubDesc, err := GetPublicCharacterDescription(database, otherID)
		if err != nil {
			return nil, fmt.Errorf("failed to generate public description for %s: %w", otherID, err)
		}
		loc.CharactersPresent = append(loc.CharactersPresent, *pubDesc)
	}

	// 6. Query Ground Items at Location
	groundRows, err := database.Query(`
		SELECT li.item_id, itm.name, li.quantity
		FROM location_items li
		JOIN items itm ON li.item_id = itm.id
		WHERE li.location_id = ? AND li.quantity > 0
		ORDER BY itm.name ASC
	`, char.CurrentLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to query ground items: %w", err)
	}
	defer groundRows.Close()

	loc.GroundItems = make([]GroundItemEntry, 0)
	for groundRows.Next() {
		var g GroundItemEntry
		if err := groundRows.Scan(&g.ItemID, &g.Name, &g.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan ground item: %w", err)
		}
		loc.GroundItems = append(loc.GroundItems, g)
	}

	// 7. Query Workstations at Location
	wsRows, err := database.Query(`
		SELECT id, name, crafting_domain
		FROM workstations
		WHERE location_id = ?
		ORDER BY name ASC
	`, char.CurrentLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to query workstations: %w", err)
	}
	defer wsRows.Close()

	loc.Workstations = make([]WorkstationEntry, 0)
	for wsRows.Next() {
		var ws WorkstationEntry
		if err := wsRows.Scan(&ws.ID, &ws.Name, &ws.CraftingDomain); err != nil {
			return nil, fmt.Errorf("failed to scan workstation: %w", err)
		}
		loc.Workstations = append(loc.Workstations, ws)
	}

	// 8. Query Resource Pools at Location
	resRows, err := database.Query(`
		SELECT resource_id, current_stock, max_stock
		FROM location_resources
		WHERE location_id = ?
		ORDER BY resource_id ASC
	`, char.CurrentLocation)
	if err != nil {
		return nil, fmt.Errorf("failed to query resource pools: %w", err)
	}
	defer resRows.Close()

	loc.ResourcePools = make([]ResourcePoolEntry, 0)
	for resRows.Next() {
		var r ResourcePoolEntry
		if err := resRows.Scan(&r.ResourceID, &r.CurrentStock, &r.MaxStock); err != nil {
			return nil, fmt.Errorf("failed to scan resource pool: %w", err)
		}
		loc.ResourcePools = append(loc.ResourcePools, r)
	}

	// 9. Query Shop Items at Location
	shopRows, err := database.Query(`
		SELECT si.item_id, itm.name, si.price_gold, si.stock_quantity, itm.weight_kg
		FROM location_shop_items si
		JOIN items itm ON si.item_id = itm.id
		WHERE si.location_id = ? AND si.stock_quantity > 0
		ORDER BY itm.name ASC
	`, char.CurrentLocation)
	if err == nil {
		defer shopRows.Close()
		loc.ShopItems = make([]ShopItemEntry, 0)
		for shopRows.Next() {
			var si ShopItemEntry
			if err := shopRows.Scan(&si.ItemID, &si.Name, &si.PriceGold, &si.StockQty, &si.WeightKg); err == nil {
				loc.ShopItems = append(loc.ShopItems, si)
			}
		}
	}

	// 10. Query known recipes that meet skill requirements and are usable at this location.
	recRows, err := database.Query(`
		SELECT r.id, r.name, r.output_item_id, r.output_quantity, r.duration_ticks, r.energy_cost,
		       COALESCE(r.required_skill_id, ''), COALESCE(cs.level, 1), COALESCE(r.min_skill_level, 1)
		FROM recipes r
		JOIN character_known_recipes kr ON kr.recipe_id = r.id AND kr.character_id = ?
		LEFT JOIN workstations w ON r.required_station = w.id
		LEFT JOIN character_skills cs ON cs.character_id = kr.character_id AND cs.skill_id = r.required_skill_id
		WHERE (r.required_station IS NULL OR r.required_station = '' OR w.location_id = ?)
		  AND (r.required_skill_id IS NULL OR r.required_skill_id = '' OR COALESCE(cs.level, 1) >= COALESCE(r.min_skill_level, 1))
		ORDER BY r.name ASC
	`, characterID, char.CurrentLocation)
	if err == nil {
		defer recRows.Close()
		loc.AvailableRecipes = make([]AvailableRecipeEntry, 0)
		for recRows.Next() {
			var rec AvailableRecipeEntry
			if err := recRows.Scan(&rec.RecipeID, &rec.RecipeName, &rec.OutputItemID, &rec.OutputQuantity, &rec.BaseDurationTicks, &rec.EnergyCost, &rec.RequiredSkillID, &rec.SkillLevel, &rec.MinSkillLevel); err == nil {
				rec.DurationTicks = ScaleDurationForSkill(rec.BaseDurationTicks, rec.SkillLevel)
				ingRows, err := database.Query("SELECT item_id, quantity FROM recipe_ingredients WHERE recipe_id = ?", rec.RecipeID)
				if err == nil {
					defer ingRows.Close()
					for ingRows.Next() {
						var ing RecipeIngredient
						if err := ingRows.Scan(&ing.ItemID, &ing.Quantity); err == nil {
							rec.Ingredients = append(rec.Ingredients, ing)
						}
					}
				}
				loc.AvailableRecipes = append(loc.AvailableRecipes, rec)
			}
		}
	}

	graphRows, err := database.Query(`
		SELECT lc.from_location_id, lc.to_location_id
		FROM location_connections lc
		JOIN characters c ON c.id = ?
		JOIN locations target ON target.id = lc.to_location_id
		WHERE c.equipped_container_id IS NULL
		   OR c.equipped_container_id = ''
		   OR c.equipped_container_id NOT IN ('wheelbarrow', 'draft_horse_cart')
		   OR (c.equipped_container_id = 'wheelbarrow' AND target.vehicle_access_tier != 'FOOT_ONLY')
		   OR (c.equipped_container_id = 'draft_horse_cart' AND target.is_indoor = 0 AND target.vehicle_access_tier = 'ALL')
		ORDER BY lc.from_location_id, lc.to_location_id
	`, characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query location graph: %w", err)
	}
	defer graphRows.Close()
	locationGraph := make([]LocationConnectionEntry, 0)
	for graphRows.Next() {
		var connection LocationConnectionEntry
		if err := graphRows.Scan(&connection.FromLocationID, &connection.ToLocationID); err != nil {
			return nil, fmt.Errorf("failed to scan location graph: %w", err)
		}
		locationGraph = append(locationGraph, connection)
	}
	if err := graphRows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read location graph: %w", err)
	}

	return &PerceptionData{
		Type:          "STATE_SYNC",
		Timestamp:     time.Now(),
		Character:     char,
		Location:      loc,
		LocationGraph: locationGraph,
	}, nil
}

// GetPublicCharacterDescription constructs a public narrative perception entry for an observing character.
func GetPublicCharacterDescription(db *sql.DB, characterID string) (*CharacterPublicPerception, error) {
	var name, occupation, status string
	var energy, hunger, health int
	var equippedName string

	err := db.QueryRow(`
		SELECT c.name, c.occupation, c.status, c.energy, c.hunger, c.health,
			COALESCE(i.name, c.equipped_container_id, '')
		FROM characters c
		LEFT JOIN items i ON i.id = c.equipped_container_id
		WHERE c.id = ?
	`, characterID).Scan(&name, &occupation, &status, &energy, &hunger, &health, &equippedName)
	if err != nil {
		return nil, fmt.Errorf("character %q not found for public description: %w", characterID, err)
	}

	// Translate precise stats into public narrative indicators
	var traits []string
	if energy < 30 {
		traits = append(traits, "looks visibly exhausted")
	} else if energy < 60 {
		traits = append(traits, "appears somewhat tired")
	}

	if hunger < 25 {
		traits = append(traits, "looks gaunt and hungry")
	}

	if health < 50 {
		traits = append(traits, "is moving sluggishly with visible wear")
	}

	if status != "IDLE" {
		traits = append(traits, fmt.Sprintf("is currently %s", strings.ToLower(status)))
	}

	desc := "appears healthy and well-rested."
	if len(traits) > 0 {
		desc = strings.Join(traits, ", ") + "."
	}

	equippedStr := "unassisted (no container)"
	if equippedName != "" {
		equippedStr = equippedName
	}

	return &CharacterPublicPerception{
		ID:         characterID,
		Name:       name,
		Occupation: occupation,
		Appearance: fmt.Sprintf("%s the %s %s", name, occupation, desc),
		Equipped:   equippedStr,
	}, nil
}
