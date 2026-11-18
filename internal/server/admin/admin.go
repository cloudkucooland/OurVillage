package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"agentmud/internal/db"
)

type AdminCommandExecutor struct {
	db             *sql.DB
	setWeather     func(string)
	setTemperature func(float64)
}

func NewAdminCommandExecutor(db *sql.DB, setWeather func(string), setTemperature func(float64)) *AdminCommandExecutor {
	return &AdminCommandExecutor{
		db:             db,
		setWeather:     setWeather,
		setTemperature: setTemperature,
	}
}

func (a *AdminCommandExecutor) SetWeather(weather string) error {
	w := strings.ToLower(strings.TrimSpace(weather))
	switch w {
	case "clear", "rain", "snow", "storm", "fog", "cloudy", "hail", "windy", "heatwave", "drought", "frost":
	default:
		return fmt.Errorf("invalid weather %q (use clear, cloudy, drought, fog, frost, hail, heatwave, rain, snow, storm, or windy)", weather)
	}
	if a.setWeather != nil {
		a.setWeather(w)
	}
	return nil
}

func (a *AdminCommandExecutor) SetTemperature(tempStr string) error {
	t, err := strconv.ParseFloat(strings.TrimSpace(tempStr), 64)
	if err != nil {
		return fmt.Errorf("invalid temperature %q (use a number, e.g. 21.5)", tempStr)
	}
	if math.IsNaN(t) || math.IsInf(t, 0) {
		return fmt.Errorf("invalid temperature %q (must be finite)", tempStr)
	}
	if a.setTemperature != nil {
		a.setTemperature(t)
	}
	return nil
}

func (a *AdminCommandExecutor) SetLocationDescription(locID, desc string) error {
	if strings.TrimSpace(desc) == "" {
		return fmt.Errorf("description cannot be empty")
	}
	return updateExisting(a.db, "locations", locID, "UPDATE locations SET description = ? WHERE id = ?", desc, locID)
}

func (a *AdminCommandExecutor) SetLocationOwner(locID, charID string) error {
	if err := requireExists(a.db, "locations", locID); err != nil {
		return err
	}
	var owner any
	if charID != "" {
		if err := requireExists(a.db, "characters", charID); err != nil {
			return err
		}
		owner = charID
	}
	_, err := a.db.Exec("UPDATE locations SET owner_id = ? WHERE id = ?", owner, locID)
	return err
}

func (a *AdminCommandExecutor) SetLocationPublic(locID string, isPublic bool) error {
	value := 0
	if isPublic {
		value = 1
	}
	return updateExisting(a.db, "locations", locID, "UPDATE locations SET is_public_property = ? WHERE id = ?", value, locID)
}

func (a *AdminCommandExecutor) SetLocationIndoor(locID string, isIndoor bool) error {
	value := 0
	if isIndoor {
		value = 1
	}
	return updateExisting(a.db, "locations", locID, "UPDATE locations SET is_indoor = ? WHERE id = ?", value, locID)
}

func (a *AdminCommandExecutor) SetLocationAccess(locID, accessTier string) error {
	tier := strings.ToUpper(accessTier)
	switch tier {
	case "ALL", "FOOT_AND_WHEELBARROW", "FOOT_ONLY":
	default:
		return fmt.Errorf("invalid access tier %q (use ALL, FOOT_AND_WHEELBARROW, or FOOT_ONLY)", accessTier)
	}
	return updateExisting(a.db, "locations", locID, "UPDATE locations SET vehicle_access_tier = ? WHERE id = ?", tier, locID)
}

func (a *AdminCommandExecutor) SetLocationHours(locID, openTime, closeTime string) error {
	if _, err := time.Parse("15:04", openTime); err != nil {
		return fmt.Errorf("invalid opening time %q (use HH:MM, 00:00-23:59)", openTime)
	}
	if _, err := time.Parse("15:04", closeTime); err != nil {
		return fmt.Errorf("invalid closing time %q (use HH:MM, 00:00-23:59)", closeTime)
	}
	return updateExisting(a.db, "locations", locID,
		"UPDATE locations SET open_time = ?, close_time = ? WHERE id = ?", openTime, closeTime, locID)
}

func (a *AdminCommandExecutor) SetLocationRules(locID, rules string) error {
	if strings.TrimSpace(rules) == "" {
		return fmt.Errorf("location rules cannot be empty")
	}
	return updateExisting(a.db, "locations", locID, "UPDATE locations SET rules_prompt = ? WHERE id = ?", rules, locID)
}

func (a *AdminCommandExecutor) AddItemToCharacter(charID, itemID string, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("quantity must be > 0")
	}
	if err := requireExists(a.db, "characters", charID); err != nil {
		return err
	}
	if err := requireExists(a.db, "items", itemID); err != nil {
		return err
	}
	_, err := a.db.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity)
		VALUES (?, ?, ?)
		ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
	`, charID, itemID, qty, qty)
	return err
}

func (a *AdminCommandExecutor) RemoveItemFromCharacter(charID, itemID string, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("quantity must be > 0")
	}
	var currentQty int
	err := a.db.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", charID, itemID).Scan(&currentQty)
	if err != nil {
		return fmt.Errorf("character does not have item %q", itemID)
	}
	if currentQty < qty {
		return fmt.Errorf("character has %d of %q, cannot remove %d", currentQty, itemID, qty)
	}

	if currentQty == qty {
		_, err = a.db.Exec("DELETE FROM inventories WHERE character_id = ? AND item_id = ?", charID, itemID)
	} else {
		_, err = a.db.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", qty, charID, itemID)
	}
	return err
}

func (a *AdminCommandExecutor) SetCharacterGold(charID string, amount int) error {
	if amount < 0 {
		return fmt.Errorf("gold cannot be negative")
	}
	return updateExisting(a.db, "characters", charID, "UPDATE characters SET gold = ? WHERE id = ?", amount, charID)
}

func (a *AdminCommandExecutor) FullHealCharacter(charID string) error {
	return updateExisting(a.db, "characters", charID, `
		UPDATE characters
		SET health = 100, hunger = 100, status = 'IDLE',
			action_target = '', action_start_tick = 0, action_end_tick = 0, action_intent_id = ''
		WHERE id = ?
	`, charID)
}

func (a *AdminCommandExecutor) SetLocationResource(locationID, resourceID string, currentStock, maxStock, replenishRate int) error {
	if !validID(locationID) || !validID(resourceID) {
		return fmt.Errorf("location and resource ids must contain only letters, numbers, underscores, and hyphens")
	}
	if currentStock < 0 || maxStock < 0 || replenishRate < 0 {
		return fmt.Errorf("resource stock and replenishment values must be >= 0")
	}
	if currentStock > maxStock {
		return fmt.Errorf("current stock (%d) cannot exceed max stock (%d)", currentStock, maxStock)
	}
	if err := requireExists(a.db, "locations", locationID); err != nil {
		return err
	}
	if err := requireExists(a.db, "items", resourceID); err != nil {
		return err
	}
	_, err := a.db.Exec(`
		INSERT INTO location_resources (location_id, resource_id, current_stock, max_stock, replenish_rate_per_day)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(location_id, resource_id) DO UPDATE SET
			current_stock = excluded.current_stock,
			max_stock = excluded.max_stock,
			replenish_rate_per_day = excluded.replenish_rate_per_day
	`, locationID, resourceID, currentStock, maxStock, replenishRate)
	if err != nil {
		return fmt.Errorf("set resource %q at %q: %w", resourceID, locationID, err)
	}
	return nil
}

func (a *AdminCommandExecutor) SetShopItem(locationID, itemID string, priceGold, stockQuantity int) error {
	if !validID(locationID) || !validID(itemID) {
		return fmt.Errorf("location and item ids must contain only letters, numbers, underscores, and hyphens")
	}
	if priceGold < 0 {
		return fmt.Errorf("price must be >= 0")
	}
	if stockQuantity < 0 {
		return fmt.Errorf("stock quantity must be >= 0")
	}
	if err := requireExists(a.db, "locations", locationID); err != nil {
		return err
	}
	if err := requireExists(a.db, "items", itemID); err != nil {
		return err
	}
	_, err := a.db.Exec(`
		INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(location_id, item_id) DO UPDATE SET
			price_gold = excluded.price_gold,
			stock_quantity = excluded.stock_quantity
	`, locationID, itemID, priceGold, stockQuantity)
	if err != nil {
		return fmt.Errorf("set shop item %q at %q: %w", itemID, locationID, err)
	}
	return nil
}

type ItemDefinition struct {
	ID             string
	Name           string
	WeightKg       float64
	Description    string
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

func (a *AdminCommandExecutor) CreateItem(item ItemDefinition) error {
	if err := validateItem(item); err != nil {
		return err
	}
	if item.IsPerishable {
		if err := requireExists(a.db, "items", item.SpoilageItemID); err != nil {
			return fmt.Errorf("spoilage item: %w", err)
		}
	}
	_, err := a.db.Exec(`INSERT INTO items
		(id, name, description, weight_kg, is_consumable, hunger_restore, health_restore, is_container, capacity_kg, slot_limit, is_perishable, shelf_life_ticks, spoilage_item_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.Name, item.Description, item.WeightKg,
		boolInt(item.IsConsumable), item.HungerRestore, item.HealthRestore, boolInt(item.IsContainer), item.CapacityKg,
		item.SlotLimit, boolInt(item.IsPerishable), item.ShelfLifeTicks, nullableString(item.SpoilageItemID))
	if err != nil {
		return fmt.Errorf("create item %q: %w", item.ID, err)
	}
	return nil
}

func (a *AdminCommandExecutor) UpdateItem(item ItemDefinition) error {
	if err := validateItem(item); err != nil {
		return err
	}
	if item.IsPerishable {
		if err := requireExists(a.db, "items", item.SpoilageItemID); err != nil {
			return fmt.Errorf("spoilage item: %w", err)
		}
	}
	if err := requireExists(a.db, "items", item.ID); err != nil {
		return err
	}
	_, err := a.db.Exec(`UPDATE items SET name = ?, description = ?, weight_kg = ?, is_consumable = ?,
		hunger_restore = ?, health_restore = ?, is_container = ?, capacity_kg = ?, slot_limit = ?,
		is_perishable = ?, shelf_life_ticks = ?, spoilage_item_id = ? WHERE id = ?`, item.Name, item.Description,
		item.WeightKg, boolInt(item.IsConsumable), item.HungerRestore, item.HealthRestore, boolInt(item.IsContainer),
		item.CapacityKg, item.SlotLimit, boolInt(item.IsPerishable), item.ShelfLifeTicks,
		nullableString(item.SpoilageItemID), item.ID)
	if err != nil {
		return fmt.Errorf("update item %q: %w", item.ID, err)
	}
	return nil
}

func validateItem(item ItemDefinition) error {
	if !validID(item.ID) {
		return fmt.Errorf("item id must contain only letters, numbers, underscores, and hyphens")
	}
	if strings.TrimSpace(item.Name) == "" {
		return fmt.Errorf("item name is required")
	}
	if strings.TrimSpace(item.Description) == "" {
		return fmt.Errorf("item description is required")
	}
	if item.WeightKg < 0 || math.IsNaN(item.WeightKg) || math.IsInf(item.WeightKg, 0) {
		return fmt.Errorf("weight must be a finite number >= 0")
	}
	if item.HungerRestore < 0 {
		return fmt.Errorf("hunger restore must be >= 0")
	}
	if item.HealthRestore < 0 {
		return fmt.Errorf("health restore must be >= 0")
	}
	if !item.IsConsumable && (item.HungerRestore != 0 || item.HealthRestore != 0) {
		return fmt.Errorf("hunger and health restoration require a consumable item")
	}
	if item.CapacityKg < 0 || math.IsNaN(item.CapacityKg) || math.IsInf(item.CapacityKg, 0) {
		return fmt.Errorf("container capacity must be a finite number >= 0")
	}
	if item.SlotLimit < 0 {
		return fmt.Errorf("container slot limit must be >= 0")
	}
	if item.IsContainer && (item.CapacityKg <= 0 || item.SlotLimit <= 0) {
		return fmt.Errorf("container capacity and slot limit must both be > 0")
	}
	if !item.IsContainer && (item.CapacityKg != 0 || item.SlotLimit != 0) {
		return fmt.Errorf("capacity and slot limit require a container item")
	}
	if item.IsPerishable {
		if item.ShelfLifeTicks <= 0 {
			return fmt.Errorf("perishable item shelf life must be > 0 ticks")
		}
		if !validID(item.SpoilageItemID) {
			return fmt.Errorf("perishable items require a spoilage item id")
		}
	} else if item.ShelfLifeTicks < 0 {
		return fmt.Errorf("shelf life must be >= 0")
	}
	return nil
}

func (a *AdminCommandExecutor) DeleteItem(itemID string) error {
	if err := requireExists(a.db, "items", itemID); err != nil {
		return err
	}
	checks := []struct{ table, column string }{
		{"inventories", "item_id"}, {"recipe_ingredients", "item_id"}, {"recipes", "output_item_id"}, {"items", "spoilage_item_id"},
		{"location_shop_items", "item_id"}, {"location_items", "item_id"},
		{"farm_crops", "seed_item_id"}, {"farm_crops", "harvest_item_id"},
	}
	for _, check := range checks {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", check.table, check.column)
		if err := a.db.QueryRow(query, itemID).Scan(&count); err != nil {
			return fmt.Errorf("check item usage: %w", err)
		}
		if count > 0 {
			return fmt.Errorf("cannot delete item %q: it is referenced by %s", itemID, check.table)
		}
	}
	if _, err := a.db.Exec("DELETE FROM items WHERE id = ?", itemID); err != nil {
		return fmt.Errorf("delete item %q: %w", itemID, err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (a *AdminCommandExecutor) CreateLocation(locationID, name, description string) error {
	if !validID(locationID) {
		return fmt.Errorf("location id must contain only letters, numbers, underscores, and hyphens")
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(description) == "" {
		return fmt.Errorf("location name and description are required")
	}
	_, err := a.db.Exec(`
		INSERT INTO locations (id, name, description, is_public_property, is_indoor, vehicle_access_tier, open_time, close_time)
		VALUES (?, ?, ?, 1, 0, 'ALL', '00:00', '23:59')
	`, locationID, name, description)
	if err != nil {
		return fmt.Errorf("create location %q: %w", locationID, err)
	}
	return nil
}

func (a *AdminCommandExecutor) ConnectLocations(fromID, toID string, travelTicks int) error {
	if !validID(fromID) || !validID(toID) {
		return fmt.Errorf("location ids must contain only letters, numbers, underscores, and hyphens")
	}
	if fromID == toID {
		return fmt.Errorf("locations must be different")
	}
	if travelTicks <= 0 {
		return fmt.Errorf("travel ticks must be > 0")
	}
	if err := requireExists(a.db, "locations", fromID); err != nil {
		return err
	}
	if err := requireExists(a.db, "locations", toID); err != nil {
		return err
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	for _, edge := range [][2]string{{fromID, toID}, {toID, fromID}} {
		_, err = tx.Exec(`
			INSERT INTO location_connections (from_location_id, to_location_id, travel_ticks)
			VALUES (?, ?, ?)
			ON CONFLICT(from_location_id, to_location_id) DO UPDATE SET travel_ticks = excluded.travel_ticks
		`, edge[0], edge[1], travelTicks)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("connect locations %q and %q: %w", fromID, toID, err)
		}
	}
	return tx.Commit()
}

func (a *AdminCommandExecutor) CreateCharacter(characterID, name, occupation, locationID, persona string) (string, error) {
	if !validID(characterID) {
		return "", fmt.Errorf("character id must contain only letters, numbers, underscores, and hyphens")
	}
	if strings.TrimSpace(name) == "" ||
		strings.TrimSpace(occupation) == "" || strings.TrimSpace(persona) == "" {
		return "", fmt.Errorf("character name, occupation, and persona are required")
	}
	if !validID(locationID) {
		return "", fmt.Errorf("location id must contain only letters, numbers, underscores, and hyphens")
	}
	if err := requireExists(a.db, "locations", locationID); err != nil {
		return "", err
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate character password: %w", err)
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	tx, err := a.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin character creation: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO characters (id, name, persona_prompt, occupation, current_location, controller_type, mqtt_password)
		VALUES (?, ?, ?, ?, ?, 'agent', ?)
	`, characterID, name, persona, occupation, locationID, password)
	if err != nil {
		return "", fmt.Errorf("create character %q: %w", characterID, err)
	}
	if err := db.InitializeCharacterProgression(tx, characterID, occupation); err != nil {
		return "", fmt.Errorf("initialize progression for character %q: %w", characterID, err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit character creation: %w", err)
	}
	return password, nil
}

// MoveCharacter relocates an idle character to an existing location.
func (a *AdminCommandExecutor) MoveCharacter(characterID, locationID string) error {
	if err := requireExists(a.db, "characters", characterID); err != nil {
		return err
	}
	if err := requireExists(a.db, "locations", locationID); err != nil {
		return err
	}
	result, err := a.db.Exec(`
		UPDATE characters
		SET current_location = ?
		WHERE id = ? AND status = 'IDLE'
	`, locationID, characterID)
	if err != nil {
		return fmt.Errorf("move character %q to %q: %w", characterID, locationID, err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check character move: %w", err)
	}
	if updated == 0 {
		return fmt.Errorf("character %q has an active action; wait until it is idle before moving them", characterID)
	}
	return nil
}

type RecipeDefinition struct {
	ID, Name, RequiredStation, OutputItemID, RequiredSkillID string
	OutputQuantity, DurationTicks, EnergyCost, MinSkillLevel int
	Ingredients                                              []db.RecipeIngredient
}

func (a *AdminCommandExecutor) CreateRecipe(recipe RecipeDefinition) error {
	return a.saveRecipe(recipe, false)
}

func (a *AdminCommandExecutor) UpdateRecipe(recipe RecipeDefinition) error {
	return a.saveRecipe(recipe, true)
}

func (a *AdminCommandExecutor) saveRecipe(recipe RecipeDefinition, update bool) error {
	if !validID(recipe.ID) {
		return fmt.Errorf("recipe id must contain only letters, numbers, underscores, and hyphens")
	}
	if strings.TrimSpace(recipe.Name) == "" || !validID(recipe.OutputItemID) {
		return fmt.Errorf("recipe name and valid output item id are required")
	}
	if recipe.OutputQuantity <= 0 || recipe.DurationTicks <= 0 || recipe.EnergyCost < 0 || recipe.MinSkillLevel < 0 {
		return fmt.Errorf("output quantity and duration must be > 0; energy and minimum skill level must be >= 0")
	}
	if err := requireExists(a.db, "items", recipe.OutputItemID); err != nil {
		return err
	}
	if recipe.RequiredStation != "" {
		if err := requireExists(a.db, "workstations", recipe.RequiredStation); err != nil {
			return err
		}
	}
	if len(recipe.Ingredients) == 0 {
		return fmt.Errorf("at least one ingredient is required")
	}
	seen := make(map[string]bool, len(recipe.Ingredients))
	for _, ingredient := range recipe.Ingredients {
		if !validID(ingredient.ItemID) || ingredient.Quantity <= 0 {
			return fmt.Errorf("ingredient ids must be valid and quantities must be > 0")
		}
		if seen[ingredient.ItemID] {
			return fmt.Errorf("ingredient %q is listed more than once", ingredient.ItemID)
		}
		seen[ingredient.ItemID] = true
		if err := requireExists(a.db, "items", ingredient.ItemID); err != nil {
			return err
		}
	}

	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("begin recipe save: %w", err)
	}
	defer tx.Rollback()
	if update {
		if _, err := tx.Exec(`UPDATE recipes SET name = ?, required_station = ?, output_item_id = ?,
			output_quantity = ?, duration_ticks = ?, energy_cost = ?, required_skill_id = ?, min_skill_level = ? WHERE id = ?`,
			recipe.Name, nullableString(recipe.RequiredStation), recipe.OutputItemID, recipe.OutputQuantity,
			recipe.DurationTicks, recipe.EnergyCost, nullableString(recipe.RequiredSkillID), recipe.MinSkillLevel, recipe.ID); err != nil {
			return fmt.Errorf("update recipe %q: %w", recipe.ID, err)
		}
		if err := requireExistsTx(tx, "recipes", recipe.ID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM recipe_ingredients WHERE recipe_id = ?", recipe.ID); err != nil {
			return fmt.Errorf("replace ingredients for recipe %q: %w", recipe.ID, err)
		}
	} else if _, err := tx.Exec(`INSERT INTO recipes
		(id, name, required_station, output_item_id, output_quantity, duration_ticks, energy_cost, required_skill_id, min_skill_level)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, recipe.ID, recipe.Name, nullableString(recipe.RequiredStation), recipe.OutputItemID,
		recipe.OutputQuantity, recipe.DurationTicks, recipe.EnergyCost, nullableString(recipe.RequiredSkillID), recipe.MinSkillLevel); err != nil {
		return fmt.Errorf("create recipe %q: %w", recipe.ID, err)
	}
	for _, ingredient := range recipe.Ingredients {
		if _, err := tx.Exec("INSERT INTO recipe_ingredients (recipe_id, item_id, quantity) VALUES (?, ?, ?)", recipe.ID, ingredient.ItemID, ingredient.Quantity); err != nil {
			return fmt.Errorf("save ingredient %q for recipe %q: %w", ingredient.ItemID, recipe.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit recipe %q: %w", recipe.ID, err)
	}
	return nil
}

func (a *AdminCommandExecutor) DeleteRecipe(recipeID string) error {
	if err := requireExists(a.db, "recipes", recipeID); err != nil {
		return err
	}
	_, err := a.db.Exec("DELETE FROM recipes WHERE id = ?", recipeID)
	if err != nil {
		return fmt.Errorf("delete recipe %q: %w", recipeID, err)
	}
	return nil
}

func (a *AdminCommandExecutor) TeachRecipe(characterID, recipeID string) error {
	if err := requireExists(a.db, "characters", characterID); err != nil {
		return err
	}
	if err := requireExists(a.db, "recipes", recipeID); err != nil {
		return err
	}
	if _, err := a.db.Exec(`INSERT INTO character_known_recipes (character_id, recipe_id) VALUES (?, ?)
		ON CONFLICT(character_id, recipe_id) DO NOTHING`, characterID, recipeID); err != nil {
		return fmt.Errorf("teach recipe %q to %q: %w", recipeID, characterID, err)
	}
	return nil
}

func (a *AdminCommandExecutor) SetCharacterSkill(characterID, skillID string, level, experience int) error {
	if !validID(skillID) {
		return fmt.Errorf("skill id must contain only letters, numbers, underscores, and hyphens")
	}
	if level < 1 || level > 100 {
		return fmt.Errorf("skill level must be between 1 and 100")
	}
	if experience < 0 {
		return fmt.Errorf("skill experience must be >= 0")
	}
	if err := requireExists(a.db, "characters", characterID); err != nil {
		return err
	}
	_, err := a.db.Exec(`INSERT INTO character_skills (character_id, skill_id, level, experience)
		VALUES (?, ?, ?, ?) ON CONFLICT(character_id, skill_id) DO UPDATE SET
		level = excluded.level, experience = excluded.experience`, characterID, skillID, level, experience)
	if err != nil {
		return fmt.Errorf("set %s skill for %s: %w", skillID, characterID, err)
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func requireExistsTx(tx *sql.Tx, table, id string) error {
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE id = ?", table)
	var exists int
	if err := tx.QueryRow(query, id).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s %q not found", strings.TrimSuffix(table, "s"), id)
		}
		return err
	}
	return nil
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, ch := range id {
		if (ch < 'a' || ch > 'z') &&
			(ch < 'A' || ch > 'Z') &&
			(ch < '0' || ch > '9') &&
			ch != '_' && ch != '-' {
			return false
		}
	}
	return true
}

func requireExists(db *sql.DB, table, id string) error {
	var exists int
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE id = ?", table)
	if err := db.QueryRow(query, id).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s %q not found", strings.TrimSuffix(table, "s"), id)
		}
		return err
	}
	return nil
}

func updateExisting(db *sql.DB, table, id, query string, args ...any) error {
	if err := requireExists(db, table, id); err != nil {
		return err
	}
	_, err := db.Exec(query, args...)
	return err
}
