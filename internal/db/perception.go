package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PerceptionData encapsulates complete spatial perception and character state for an agent.
type PerceptionData struct {
	Type      string            `json:"type"` // "STATE_SYNC"
	Timestamp time.Time         `json:"timestamp"`
	Character CharacterOverview `json:"character"`
	Location  LocationOverview  `json:"location"`
}

// CharacterOverview captures character attributes, encumbrance, and inventory.
type CharacterOverview struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Occupation        string               `json:"occupation"`
	Gold              int                  `json:"gold"`
	Hunger            int                  `json:"hunger"`
	Energy            int                  `json:"energy"`
	Status            string               `json:"status"`
	CurrentLocation   string               `json:"current_location"`
	EquippedContainer string               `json:"equipped_container,omitempty"`
	CarryingWeightKg  float64              `json:"carrying_weight_kg"`
	Encumbrance       EncumbranceState     `json:"encumbrance"`
	MaxLimitKg        float64              `json:"max_limit_kg"`
	Inventory         []InventoryItemEntry `json:"inventory"`
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
	RecipeID       string             `json:"recipe_id"`
	RecipeName     string             `json:"recipe_name"`
	OutputItemID   string             `json:"output_item_id"`
	OutputQuantity int                `json:"output_quantity"`
	DurationTicks  int                `json:"duration_ticks"`
	EnergyCost     int                `json:"energy_cost"`
	Ingredients    []RecipeIngredient `json:"ingredients"`
}

// ShopItemEntry captures items available for purchase at a location storefront.
type ShopItemEntry struct {
	ItemID    string `json:"item_id"`
	Name      string `json:"name"`
	PriceGold int    `json:"price_gold"`
	StockQty  int    `json:"stock_quantity"`
}

// ConnectedLocationEntry captures adjacent graph nodes and travel ticks.
type ConnectedLocationEntry struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	TravelTicks  int    `json:"travel_ticks"`
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
		SELECT id, name, occupation, gold, hunger, energy, current_location, status, equipped_container_id
		FROM characters
		WHERE id = ?
	`, characterID).Scan(
		&char.ID,
		&char.Name,
		&char.Occupation,
		&char.Gold,
		&char.Hunger,
		&char.Energy,
		&char.CurrentLocation,
		&char.Status,
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
	char.Encumbrance = weightInfo.Encumbrance
	char.MaxLimitKg = weightInfo.OverburdenLimitKg

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
		SELECT si.item_id, itm.name, si.price_gold, si.stock_quantity
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
			if err := shopRows.Scan(&si.ItemID, &si.Name, &si.PriceGold, &si.StockQty); err == nil {
				loc.ShopItems = append(loc.ShopItems, si)
			}
		}
	}

	// 10. Query Available Crafting Recipes at Location Workstations or Universal
	recRows, err := database.Query(`
		SELECT r.id, r.name, r.output_item_id, r.output_quantity, r.duration_ticks, r.energy_cost
		FROM recipes r
		LEFT JOIN workstations w ON r.required_station = w.id
		WHERE r.required_station IS NULL OR r.required_station = '' OR w.location_id = ?
		ORDER BY r.name ASC
	`, char.CurrentLocation)
	if err == nil {
		defer recRows.Close()
		loc.AvailableRecipes = make([]AvailableRecipeEntry, 0)
		for recRows.Next() {
			var rec AvailableRecipeEntry
			if err := recRows.Scan(&rec.RecipeID, &rec.RecipeName, &rec.OutputItemID, &rec.OutputQuantity, &rec.DurationTicks, &rec.EnergyCost); err == nil {
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

	return &PerceptionData{
		Type:      "STATE_SYNC",
		Timestamp: time.Now(),
		Character: char,
		Location:  loc,
	}, nil
}

// GetPublicCharacterDescription constructs a public narrative perception entry for an observing character.
func GetPublicCharacterDescription(db *sql.DB, characterID string) (*CharacterPublicPerception, error) {
	var name, occupation, status string
	var energy, hunger, health int
	var equippedContainer sql.NullString

	err := db.QueryRow(`
		SELECT name, occupation, status, energy, hunger, health, equipped_container_id 
		FROM characters WHERE id = ?
	`, characterID).Scan(&name, &occupation, &status, &energy, &hunger, &health, &equippedContainer)
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
	if equippedContainer.Valid && equippedContainer.String != "" {
		equippedStr = equippedContainer.String
	}

	return &CharacterPublicPerception{
		ID:         characterID,
		Name:       name,
		Occupation: occupation,
		Appearance: fmt.Sprintf("%s the %s %s", name, occupation, desc),
		Equipped:   equippedStr,
	}, nil
}
