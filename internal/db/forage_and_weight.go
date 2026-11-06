package db

import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// Queryable is an interface implemented by both *sql.DB and *sql.Tx
type Queryable interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

type EncumbranceState string

const (
	EncumbranceUnencumbered EncumbranceState = "UNENCUMBERED"
	EncumbranceEncumbered   EncumbranceState = "ENCUMBERED"
	EncumbranceOverburdened EncumbranceState = "OVERBURDENED"
)

const (
	BaseUnassistedCapacityKg = 20.0
	BaseUnassistedSlotLimit  = 5
	OverburdenMultiplier     = 1.5 // Total weight > capacity * 1.5 is OVERBURDENED
)

type WeightInfo struct {
	TotalWeightKg       float64          `json:"total_weight_kg"`
	MaxCapacityKg       float64          `json:"max_capacity_kg"`
	OverburdenLimitKg   float64          `json:"overburden_limit_kg"`
	Encumbrance         EncumbranceState `json:"encumbrance"`
	SlotsUsed           int              `json:"slots_used"`
	MaxSlots            int              `json:"max_slots"`
	EquippedContainerID string           `json:"equipped_container_id,omitempty"`
}

type LocationItem struct {
	ItemID    string    `json:"item_id"`
	Name      string    `json:"name"`
	Quantity  int       `json:"quantity"`
	WeightKg  float64   `json:"weight_kg"`
	DroppedBy string    `json:"dropped_by"`
	DroppedAt time.Time `json:"dropped_at"`
}

type LocationResource struct {
	LocationID          string `json:"location_id"`
	ResourceID          string `json:"resource_id"`
	CurrentStock        int    `json:"current_stock"`
	MaxStock            int    `json:"max_stock"`
	ReplenishRatePerDay int    `json:"replenish_rate_per_day"`
}

type SecondaryDropRule struct {
	LocationID       string
	PrimaryTarget    string
	SecondaryItemID  string
	BaseRatePerHour  float64 // e.g. 0.03 for 3.0%
	DiscoveryContext string
}

var SecondaryDropRules = []SecondaryDropRule{
	{LocationID: "deep_forest", PrimaryTarget: "firewood", SecondaryItemID: "wild_mushrooms", BaseRatePerHour: 0.03, DiscoveryContext: "Found on damp decaying bark."},
	{LocationID: "deep_forest", PrimaryTarget: "wood_log", SecondaryItemID: "wild_mushrooms", BaseRatePerHour: 0.03, DiscoveryContext: "Found on damp decaying bark."},
	{LocationID: "deep_forest", PrimaryTarget: "logs", SecondaryItemID: "wild_mushrooms", BaseRatePerHour: 0.03, DiscoveryContext: "Found on damp decaying bark."},
	{LocationID: "deep_forest", PrimaryTarget: "firewood", SecondaryItemID: "bird_feather", BaseRatePerHour: 0.05, DiscoveryContext: "Fallen from overhead canopy."},
	{LocationID: "deep_forest", PrimaryTarget: "wood_log", SecondaryItemID: "bird_feather", BaseRatePerHour: 0.05, DiscoveryContext: "Fallen from overhead canopy."},
	{LocationID: "deep_forest", PrimaryTarget: "logs", SecondaryItemID: "bird_feather", BaseRatePerHour: 0.05, DiscoveryContext: "Fallen from overhead canopy."},
	{LocationID: "prairie", PrimaryTarget: "animal_fodder", SecondaryItemID: "medicinal_herbs", BaseRatePerHour: 0.04, DiscoveryContext: "Hidden among meadow grasses."},
	{LocationID: "prairie", PrimaryTarget: "wild_grain", SecondaryItemID: "field_berries", BaseRatePerHour: 0.035, DiscoveryContext: "Shaded under wild brambles."},
	{LocationID: "riverbank", PrimaryTarget: "clay_lumps", SecondaryItemID: "freshwater_pearl", BaseRatePerHour: 0.005, DiscoveryContext: "Uncovered in riverbed mussel."},
	{LocationID: "riverbank", PrimaryTarget: "fresh_water", SecondaryItemID: "river_reeds", BaseRatePerHour: 0.06, DiscoveryContext: "Gathered along the shoreline."},
}

type ForageYieldItem struct {
	ItemID               string  `json:"item_id"`
	Quantity             int     `json:"quantity"`
	WeightKg             float64 `json:"weight_kg"`
	IsSecondaryDiscovery bool    `json:"is_secondary_discovery,omitempty"`
}

type ForageResolutionResult struct {
	TicksSpent      int               `json:"ticks_spent"`
	EnergyDeducted  int               `json:"energy_deducted"`
	SkillID         string            `json:"skill_id"`
	SkillXPGained   int               `json:"skill_xp_gained"`
	SkillLevel      int               `json:"skill_level"`
	PreviousLevel   int               `json:"previous_skill_level"`
	SkillExperience int               `json:"skill_experience"`
	Yield           []ForageYieldItem `json:"yield"`
	TotalWeightKg   float64           `json:"total_weight_kg"`
	MaxCapacityKg   float64           `json:"max_capacity_kg"`
	NarrativeLog    string            `json:"narrative_log"`
	NewSkillLevel   int               `json:"new_skill_level"`
	PrimaryYieldQty int               `json:"primary_yield_qty"`
}

// GetCharacterWeightInfo calculates current inventory weight, equipped container limits, and encumbrance state over any Queryable (db or tx).
func GetCharacterWeightInfo(q Queryable, characterID string) (*WeightInfo, error) {
	var equippedContainerID sql.NullString
	err := q.QueryRow("SELECT equipped_container_id FROM characters WHERE id = ?", characterID).Scan(&equippedContainerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get character equipped container: %w", err)
	}

	maxCapacity := BaseUnassistedCapacityKg
	maxSlots := BaseUnassistedSlotLimit
	containerWeight := 0.0
	equippedID := ""

	if equippedContainerID.Valid && equippedContainerID.String != "" {
		equippedID = equippedContainerID.String
		var weightKg, capKg float64
		var slotLimit int
		err := q.QueryRow("SELECT weight_kg, capacity_kg, slot_limit FROM items WHERE id = ?", equippedID).Scan(&weightKg, &capKg, &slotLimit)
		if err == nil {
			maxCapacity = capKg
			maxSlots = slotLimit
			containerWeight = weightKg
		}
	}

	// Calculate total weight and slots used from inventories
	rows, err := q.Query(`
		SELECT i.item_id, i.quantity, it.weight_kg 
		FROM inventories i 
		JOIN items it ON i.item_id = it.id 
		WHERE i.character_id = ? AND i.quantity > 0
	`, characterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query character inventory: %w", err)
	}
	defer rows.Close()

	totalItemsWeight := 0.0
	slotsUsed := 0
	equippedFoundInInventory := false

	for rows.Next() {
		var itemID string
		var qty int
		var itemWeight float64
		if err := rows.Scan(&itemID, &qty, &itemWeight); err != nil {
			return nil, fmt.Errorf("failed to scan inventory item: %w", err)
		}
		totalItemsWeight += float64(qty) * itemWeight
		slotsUsed++
		if itemID == equippedID {
			equippedFoundInInventory = true
		}
	}

	totalWeight := totalItemsWeight
	if equippedID != "" && !equippedFoundInInventory {
		totalWeight += containerWeight
	}

	totalWeight = math.Round(totalWeight*10.0) / 10.0
	overburdenLimit := math.Round(maxCapacity*OverburdenMultiplier*10.0) / 10.0

	encumbrance := EncumbranceUnencumbered
	if totalWeight > overburdenLimit {
		encumbrance = EncumbranceOverburdened
	} else if totalWeight > maxCapacity {
		encumbrance = EncumbranceEncumbered
	}

	return &WeightInfo{
		TotalWeightKg:       totalWeight,
		MaxCapacityKg:       maxCapacity,
		OverburdenLimitKg:   overburdenLimit,
		Encumbrance:         encumbrance,
		SlotsUsed:           slotsUsed,
		MaxSlots:            maxSlots,
		EquippedContainerID: equippedID,
	}, nil
}

// CanAccessLocation evaluates if the character's equipped container/vehicle is permitted in the target location.
func CanAccessLocation(db *sql.DB, characterID string, targetLocationID string) (bool, string, error) {
	var equippedContainerID sql.NullString
	err := db.QueryRow("SELECT equipped_container_id FROM characters WHERE id = ?", characterID).Scan(&equippedContainerID)
	if err != nil {
		return false, "", fmt.Errorf("failed to get character container: %w", err)
	}

	if !equippedContainerID.Valid || equippedContainerID.String == "" {
		return true, "", nil
	}

	containerID := equippedContainerID.String

	var locName, accessTier string
	var isIndoor int
	err = db.QueryRow("SELECT name, vehicle_access_tier, is_indoor FROM locations WHERE id = ?", targetLocationID).Scan(&locName, &accessTier, &isIndoor)
	if err != nil {
		return false, "", fmt.Errorf("failed to get location spatial attributes: %w", err)
	}

	switch containerID {
	case "draft_horse_cart":
		if isIndoor == 1 {
			return false, fmt.Sprintf("Cannot bring Draft Horse & Cart inside indoor location %s; must remain hitched outside.", locName), nil
		}
		if accessTier != "ALL" {
			return false, fmt.Sprintf("Draft Horse & Cart requires vehicle-accessible outdoor roads (location '%s' is tier %s).", locName, accessTier), nil
		}
	case "wheelbarrow":
		if accessTier == "FOOT_ONLY" {
			return false, fmt.Sprintf("Wheelbarrow cannot access %s: location is restricted to foot traffic only.", locName), nil
		}
	}

	return true, "", nil
}

// CanTravelLocation checks if there is a graph connection between currentLocation and targetLocation, returning reachability and travel ticks.
func CanTravelLocation(db *sql.DB, currentLocationID string, targetLocationID string) (bool, int, error) {
	var travelTicks int
	err := db.QueryRow(`
		SELECT travel_ticks FROM location_connections
		WHERE from_location_id = ? AND to_location_id = ?
	`, currentLocationID, targetLocationID).Scan(&travelTicks)
	if err == sql.ErrNoRows {
		return false, 0, nil
	} else if err != nil {
		return false, 0, err
	}
	return true, travelTicks, nil
}

// DropItem removes an item from character inventory and places it on the ground in location_items within a safe transaction.
func DropItem(db *sql.DB, characterID string, itemID string, quantity int) error {
	if quantity <= 0 {
		return fmt.Errorf("quantity must be greater than 0")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var locationID string
	var equippedContainer sql.NullString
	err = tx.QueryRow("SELECT current_location, equipped_container_id FROM characters WHERE id = ?", characterID).Scan(&locationID, &equippedContainer)
	if err != nil {
		return fmt.Errorf("character not found: %w", err)
	}

	var currentQty int
	err = tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, itemID).Scan(&currentQty)
	if err == sql.ErrNoRows || currentQty < quantity {
		return fmt.Errorf("insufficient items in inventory")
	} else if err != nil {
		return fmt.Errorf("failed to query inventory: %w", err)
	}

	if currentQty == quantity {
		_, err = tx.Exec("DELETE FROM inventories WHERE character_id = ? AND item_id = ?", characterID, itemID)
	} else {
		_, err = tx.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", quantity, characterID, itemID)
	}
	if err != nil {
		return fmt.Errorf("failed to deduct inventory: %w", err)
	}

	if equippedContainer.Valid && equippedContainer.String == itemID && currentQty == quantity {
		_, err = tx.Exec("UPDATE characters SET equipped_container_id = NULL WHERE id = ?", characterID)
		if err != nil {
			return fmt.Errorf("failed to unequip dropped container: %w", err)
		}
	}

	_, err = tx.Exec(`
		INSERT INTO location_items (location_id, item_id, quantity, dropped_by, dropped_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(location_id, item_id) DO UPDATE SET
			quantity = quantity + ?,
			dropped_by = excluded.dropped_by,
			dropped_at = CURRENT_TIMESTAMP
	`, locationID, itemID, quantity, characterID, quantity)
	if err != nil {
		return fmt.Errorf("failed to update location items: %w", err)
	}

	return tx.Commit()
}

// PickupItem transfers an item from location ground to character inventory within a safe transaction.
func PickupItem(db *sql.DB, characterID string, itemID string, quantity int) error {
	if quantity <= 0 {
		return fmt.Errorf("quantity must be greater than 0")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Get current location
	var locationID string
	err = tx.QueryRow("SELECT current_location FROM characters WHERE id = ?", characterID).Scan(&locationID)
	if err != nil {
		return fmt.Errorf("character not found: %w", err)
	}

	// 2. Pass tx into GetCharacterWeightInfo so all reads operate inside the active transaction
	weightInfo, err := GetCharacterWeightInfo(tx, characterID)
	if err != nil {
		return fmt.Errorf("failed to calculate character weight info: %w", err)
	}

	// 3. Check item weight and ground availability
	var groundQty int
	err = tx.QueryRow("SELECT quantity FROM location_items WHERE location_id = ? AND item_id = ?", locationID, itemID).Scan(&groundQty)
	if err == sql.ErrNoRows || groundQty < quantity {
		return fmt.Errorf("ground item not found or insufficient quantity")
	} else if err != nil {
		return fmt.Errorf("failed to check location items: %w", err)
	}

	var itemWeight float64
	err = tx.QueryRow("SELECT weight_kg FROM items WHERE id = ?", itemID).Scan(&itemWeight)
	if err != nil {
		return fmt.Errorf("item definition not found: %w", err)
	}

	// 4. Check capacity and slot constraints
	addedWeight := float64(quantity) * itemWeight
	if weightInfo.TotalWeightKg+addedWeight > weightInfo.OverburdenLimitKg {
		return fmt.Errorf("EXCEEDS_CARRYING_CAPACITY: cannot carry %.1f kg (overburden limit %.1f kg)", weightInfo.TotalWeightKg+addedWeight, weightInfo.OverburdenLimitKg)
	}

	var existingQty int
	err = tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, itemID).Scan(&existingQty)
	if (err == sql.ErrNoRows || existingQty == 0) && weightInfo.SlotsUsed >= weightInfo.MaxSlots {
		return fmt.Errorf("INVENTORY_FULL: inventory slot limit (%d) reached", weightInfo.MaxSlots)
	}

	// Deduct from location ground
	if groundQty == quantity {
		_, err = tx.Exec("DELETE FROM location_items WHERE location_id = ? AND item_id = ?", locationID, itemID)
	} else {
		_, err = tx.Exec("UPDATE location_items SET quantity = quantity - ? WHERE location_id = ? AND item_id = ?", quantity, locationID, itemID)
	}
	if err != nil {
		return fmt.Errorf("failed to deduct from location items: %w", err)
	}

	// Add to character inventory
	_, err = tx.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity)
		VALUES (?, ?, ?)
		ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
	`, characterID, itemID, quantity, quantity)
	if err != nil {
		return fmt.Errorf("failed to update character inventory: %w", err)
	}

	return tx.Commit()
}

// EquipContainer sets the character's active container equipment.
func EquipContainer(db *sql.DB, characterID, itemID string) error {
	var isContainer int
	err := db.QueryRow("SELECT is_container FROM items WHERE id = ?", itemID).Scan(&isContainer)
	if err != nil || isContainer != 1 {
		return fmt.Errorf("item %s is not a valid container", itemID)
	}

	var hasItem int
	err = db.QueryRow("SELECT COUNT(*) FROM inventories WHERE character_id = ? AND item_id = ? AND quantity > 0", characterID, itemID).Scan(&hasItem)
	if err != nil || hasItem == 0 {
		return fmt.Errorf("character does not possess container %s in inventory", itemID)
	}

	_, err = db.Exec("UPDATE characters SET equipped_container_id = ? WHERE id = ?", itemID, characterID)
	if err != nil {
		return fmt.Errorf("failed to equip container: %w", err)
	}
	return nil
}

// UnequipContainer clears the character's equipped container.
func UnequipContainer(db *sql.DB, characterID string) error {
	_, err := db.Exec("UPDATE characters SET equipped_container_id = NULL WHERE id = ?", characterID)
	if err != nil {
		return fmt.Errorf("failed to unequip container: %w", err)
	}
	return nil
}

// GetSkill retrieves current skill level and experience points over any Queryable (*sql.DB or *sql.Tx).
func GetSkill(q Queryable, characterID, skillID string) (int, int, error) {
	var level, exp int
	err := q.QueryRow("SELECT level, experience FROM character_skills WHERE character_id = ? AND skill_id = ?", characterID, skillID).Scan(&level, &exp)
	if err == sql.ErrNoRows {
		if _, err := execQueryable(q, "INSERT INTO character_skills (character_id, skill_id, level, experience) VALUES (?, ?, 1, 0)", characterID, skillID); err != nil {
			return 1, 0, fmt.Errorf("failed to initialize character skill: %w", err)
		}
		return 1, 0, nil
	} else if err != nil {
		return 1, 0, fmt.Errorf("failed to query character skill: %w", err)
	}
	return level, exp, nil
}

// AddSkillXP awards experience to a skill and scales the level (1-100) over any Queryable (*sql.DB or *sql.Tx).
func AddSkillXP(q Queryable, characterID, skillID string, xpGained int) (int, int, error) {
	currLevel, currExp, err := GetSkill(q, characterID, skillID)
	if err != nil {
		return 1, 0, err
	}

	newExp := currExp + xpGained
	newLevel := 1 + newExp/100
	if newLevel > 100 {
		newLevel = 100
	}
	if newLevel < currLevel {
		newLevel = currLevel
	}

	_, err = execQueryable(q, `
		INSERT INTO character_skills (character_id, skill_id, level, experience)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(character_id, skill_id) DO UPDATE SET
			level = excluded.level,
			experience = excluded.experience
	`, characterID, skillID, newLevel, newExp)
	if err != nil {
		return currLevel, currExp, fmt.Errorf("failed to update character skill: %w", err)
	}

	return newLevel, newExp, nil
}

// ScaleDurationForSkill reduces action time with diminishing returns, capped at a 30% reduction.
func ScaleDurationForSkill(baseTicks, skillLevel int) int {
	if baseTicks <= 1 {
		return max(baseTicks, 1)
	}
	if skillLevel < 1 {
		skillLevel = 1
	}
	reduction := 0.30 * float64(skillLevel-1) / float64(skillLevel+9)
	scaled := int(math.Round(float64(baseTicks) * (1 - reduction)))
	if scaled < 1 {
		return 1
	}
	return scaled
}

// GetSkillForResource maps a target resource to its associated skill domain.
func GetSkillForResource(resourceID string) string {
	switch resourceID {
	case "firewood", "wood_log", "logs":
		return "woodcutting"
	case "medicinal_herbs", "wild_mushrooms":
		return "herbalism"
	default:
		return "foraging"
	}
}

// GetBaseSuccessRate returns the hourly baseline yield rate for primary resources.
func GetBaseSuccessRate(resourceID string) float64 {
	switch resourceID {
	case "firewood":
		return 1.2
	case "wood_log", "logs":
		return 0.8
	case "animal_fodder":
		return 2.0
	case "wild_grain":
		return 1.5
	case "clay_lumps":
		return 1.0
	case "fresh_water":
		return 3.0
	default:
		return 1.0
	}
}

// GetWeatherMultiplier computes weather synergy for resource recovery.
func GetWeatherMultiplier(resourceID string, weather string) float64 {
	w := strings.ToLower(weather)
	switch {
	case strings.Contains(w, "rain") || strings.Contains(w, "storm"):
		switch resourceID {
		case "fresh_water", "wild_mushrooms", "medicinal_herbs":
			return 1.5
		case "firewood":
			return 0.8
		default:
			return 1.0
		}
	case strings.Contains(w, "clear") || strings.Contains(w, "dry") || strings.Contains(w, "sunny"):
		switch resourceID {
		case "firewood":
			return 1.2
		case "wood_log", "logs":
			return 1.1
		default:
			return 1.0
		}
	default:
		return 1.0
	}
}

// DepleteLocationResource decrements available stock down to 0 within any Queryable.
func DepleteLocationResource(q Queryable, locationID, resourceID string, amount int) error {
	_, err := execQueryable(q, "UPDATE location_resources SET current_stock = MAX(0, current_stock - ?) WHERE location_id = ? AND resource_id = ?", amount, locationID, resourceID)
	return err
}

// ReplenishLocationResources increases stock according to daily replenishment and weather synergies.
func ReplenishLocationResources(db *sql.DB, weather string) error {
	rows, err := db.Query("SELECT location_id, resource_id, current_stock, max_stock, replenish_rate_per_day FROM location_resources")
	if err != nil {
		return fmt.Errorf("failed to query location resources: %w", err)
	}
	defer rows.Close()

	type resUpdate struct {
		locationID string
		resourceID string
		addAmount  int
		maxStock   int
	}

	var updates []resUpdate
	w := strings.ToLower(weather)

	for rows.Next() {
		var locID, resID string
		var curStock, maxStock, dailyRate int
		if err := rows.Scan(&locID, &resID, &curStock, &maxStock, &dailyRate); err != nil {
			return err
		}

		hourlyReplenish := float64(dailyRate) / 24.0
		if strings.Contains(w, "rain") || strings.Contains(w, "storm") {
			if resID == "fresh_water" {
				hourlyReplenish *= 2.0
			} else if resID == "wild_mushrooms" || resID == "medicinal_herbs" {
				hourlyReplenish *= 1.5
			}
		} else if strings.Contains(w, "clear") || strings.Contains(w, "sunny") || strings.Contains(w, "dry") {
			if resID == "firewood" || resID == "wood_log" || resID == "logs" {
				hourlyReplenish *= 1.2
			}
		}

		add := int(math.Ceil(hourlyReplenish))
		if add < 1 {
			add = 1
		}

		updates = append(updates, resUpdate{
			locationID: locID,
			resourceID: resID,
			addAmount:  add,
			maxStock:   maxStock,
		})
	}

	for _, u := range updates {
		_, _ = db.Exec("UPDATE location_resources SET current_stock = MIN(?, current_stock + ?) WHERE location_id = ? AND resource_id = ?", u.maxStock, u.addAmount, u.locationID, u.resourceID)
	}

	return nil
}

// ResolveForaging evaluates duration foraging formulas, ecological stock, drops, skill XP, and narrative output within a transaction.
func ResolveForaging(
	db *sql.DB,
	characterID string,
	locationID string,
	targetResource string,
	ticksSpent int,
	weather string,
	isCancelled bool,
) (*ForageResolutionResult, error) {
	if ticksSpent < 0 {
		ticksSpent = 0
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start foraging transaction: %w", err)
	}
	defer tx.Rollback()

	skillID := GetSkillForResource(targetResource)
	skillLevel, _, err := GetSkill(tx, characterID, skillID)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s skill: %w", skillID, err)
	}

	var currentStock, maxStock int
	err = tx.QueryRow("SELECT current_stock, max_stock FROM location_resources WHERE location_id = ? AND resource_id = ?", locationID, targetResource).Scan(&currentStock, &maxStock)
	stockDepletionMult := 1.0
	if err == nil {
		if currentStock <= 0 {
			stockDepletionMult = 0.20
		}
	}

	baseRate := GetBaseSuccessRate(targetResource) * stockDepletionMult
	skillMult := 1.0 + (float64(skillLevel) * 0.01)
	weatherMult := GetWeatherMultiplier(targetResource, weather)
	rngFactor := 0.95 + rand.Float64()*0.10

	expectedYield := (float64(ticksSpent) / 60.0) * baseRate * skillMult * weatherMult * rngFactor
	intYield := int(math.Floor(expectedYield))
	remainder := expectedYield - float64(intYield)
	if rand.Float64() < remainder {
		intYield++
	}

	primaryQty := intYield
	if currentStock > 0 && primaryQty > currentStock {
		primaryQty = currentStock
	}

	if primaryQty > 0 {
		_ = DepleteLocationResource(tx, locationID, targetResource, primaryQty)
	}

	var yieldItems []ForageYieldItem

	var primaryWeight float64
	var primaryName string
	err = tx.QueryRow("SELECT name, weight_kg FROM items WHERE id = ?", targetResource).Scan(&primaryName, &primaryWeight)
	if err != nil {
		primaryName = targetResource
		primaryWeight = 1.0
	}

	if primaryQty > 0 {
		yieldItems = append(yieldItems, ForageYieldItem{
			ItemID:               targetResource,
			Quantity:             primaryQty,
			WeightKg:             primaryWeight,
			IsSecondaryDiscovery: false,
		})
	}

	hoursSpent := float64(ticksSpent) / 60.0
	var discoveredSecondaries []string
	for _, rule := range SecondaryDropRules {
		if rule.LocationID == locationID && (rule.PrimaryTarget == targetResource || (targetResource == "firewood" && rule.PrimaryTarget == "logs")) {
			effectiveRatePerHour := rule.BaseRatePerHour + (float64(skillLevel) * 0.0005)
			chance := 1.0 - math.Pow(1.0-effectiveRatePerHour, math.Max(hoursSpent, 0.1))
			if rand.Float64() < chance {
				var secWeight float64
				var secName string
				err := tx.QueryRow("SELECT name, weight_kg FROM items WHERE id = ?", rule.SecondaryItemID).Scan(&secName, &secWeight)
				if err != nil {
					secName = rule.SecondaryItemID
					secWeight = 0.2
				}
				yieldItems = append(yieldItems, ForageYieldItem{
					ItemID:               rule.SecondaryItemID,
					Quantity:             1,
					WeightKg:             secWeight,
					IsSecondaryDiscovery: true,
				})
				discoveredSecondaries = append(discoveredSecondaries, secName)
			}
		}
	}

	xpGained := int(math.Round(float64(ticksSpent) * 0.25))
	if xpGained < 1 && ticksSpent > 0 {
		xpGained = 1
	}
	newSkillLevel, newSkillExperience, err := AddSkillXP(tx, characterID, skillID, xpGained)
	if err != nil {
		return nil, fmt.Errorf("failed to award %s skill experience: %w", skillID, err)
	}

	initialWeightInfo, _ := GetCharacterWeightInfo(tx, characterID)
	baseEnergy := (float64(ticksSpent) / 60.0) * (10.0 - (float64(skillLevel) * 0.05))
	if initialWeightInfo != nil && initialWeightInfo.Encumbrance == EncumbranceEncumbered {
		baseEnergy *= 2.0
	}
	energyDeducted := int(math.Round(baseEnergy))
	if energyDeducted < 1 && ticksSpent > 0 {
		energyDeducted = 1
	}
	hungerDeducted := int(math.Round(float64(ticksSpent) / 60.0 * 5.0))
	if hungerDeducted < 1 && ticksSpent > 0 {
		hungerDeducted = 1
	}

	_, _ = tx.Exec("UPDATE characters SET energy = MAX(0, energy - ?), hunger = MAX(0, hunger - ?) WHERE id = ?", energyDeducted, hungerDeducted, characterID)

	for _, item := range yieldItems {
		wInfo, _ := GetCharacterWeightInfo(tx, characterID)
		addedWeight := float64(item.Quantity) * item.WeightKg

		canCarry := true
		if wInfo != nil {
			if wInfo.TotalWeightKg+addedWeight > wInfo.OverburdenLimitKg {
				canCarry = false
			}
			var existingQty int
			err := tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, item.ItemID).Scan(&existingQty)
			if (err == sql.ErrNoRows || existingQty == 0) && wInfo.SlotsUsed >= wInfo.MaxSlots {
				canCarry = false
			}
		}

		if canCarry {
			_, _ = tx.Exec(`
				INSERT INTO inventories (character_id, item_id, quantity)
				VALUES (?, ?, ?)
				ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
			`, characterID, item.ItemID, item.Quantity, item.Quantity)
		} else {
			_, _ = tx.Exec(`
				INSERT INTO location_items (location_id, item_id, quantity, dropped_by, dropped_at)
				VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
				ON CONFLICT(location_id, item_id) DO UPDATE SET quantity = quantity + ?
			`, locationID, item.ItemID, item.Quantity, characterID, item.Quantity)
		}
	}

	finalWeightInfo, _ := GetCharacterWeightInfo(tx, characterID)
	finalWeight := 0.0
	maxCap := BaseUnassistedCapacityKg
	if finalWeightInfo != nil {
		finalWeight = finalWeightInfo.TotalWeightKg
		maxCap = finalWeightInfo.MaxCapacityKg
	}

	var locName string
	err = tx.QueryRow("SELECT name FROM locations WHERE id = ?", locationID).Scan(&locName)
	if err != nil {
		locName = locationID
	}

	timeDesc := ""
	if ticksSpent == 60 {
		timeDesc = "1 hour"
	} else if ticksSpent > 60 {
		timeDesc = fmt.Sprintf("%.1f hours", float64(ticksSpent)/60.0)
	} else {
		timeDesc = fmt.Sprintf("%d ticks", ticksSpent)
	}

	var narrativeParts []string
	if isCancelled {
		narrativeParts = append(narrativeParts, fmt.Sprintf("You stopped foraging early after %s in %s.", timeDesc, locName))
	} else {
		narrativeParts = append(narrativeParts, fmt.Sprintf("You spent %s gathering %s in %s.", timeDesc, targetResource, locName))
	}

	if primaryQty > 0 {
		narrativeParts = append(narrativeParts, fmt.Sprintf("You secured %d %s", primaryQty, primaryName))
	} else {
		narrativeParts = append(narrativeParts, fmt.Sprintf("You did not secure any %s this time", targetResource))
	}

	if len(discoveredSecondaries) > 0 {
		narrativeParts[len(narrativeParts)-1] = narrativeParts[len(narrativeParts)-1] + fmt.Sprintf(" and discovered %s.", strings.Join(discoveredSecondaries, ", "))
	} else {
		narrativeParts[len(narrativeParts)-1] = narrativeParts[len(narrativeParts)-1] + "."
	}

	narrativeLog := strings.Join(narrativeParts, " ")

	_, _ = tx.Exec(`
		UPDATE characters 
		SET status = 'IDLE', action_end_tick = 0, action_start_tick = 0, action_target = '', action_intent_id = '' 
		WHERE id = ?
	`, characterID)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit foraging transaction: %w", err)
	}

	return &ForageResolutionResult{
		TicksSpent:      ticksSpent,
		EnergyDeducted:  energyDeducted,
		SkillID:         skillID,
		SkillXPGained:   xpGained,
		SkillLevel:      newSkillLevel,
		PreviousLevel:   skillLevel,
		SkillExperience: newSkillExperience,
		Yield:           yieldItems,
		TotalWeightKg:   finalWeight,
		MaxCapacityKg:   maxCap,
		NarrativeLog:    narrativeLog,
		NewSkillLevel:   newSkillLevel,
		PrimaryYieldQty: primaryQty,
	}, nil
}
