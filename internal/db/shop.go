package db

import (
	"database/sql"
	"fmt"
	"log"
)

type ShopItemSeed struct {
	LocationID    string
	ItemID        string
	PriceGold     int
	StockQuantity int
	BuyPriceGold  int
}

var DefaultShopItems = []ShopItemSeed{
	{LocationID: "tavern", ItemID: "bread", PriceGold: 2, StockQuantity: 25},
	{LocationID: "tavern", ItemID: "ale_mug", PriceGold: 2, StockQuantity: 20},
	{LocationID: "tavern", ItemID: "roasted_mutton", PriceGold: 3, StockQuantity: 15},
	{LocationID: "tavern", ItemID: "spiced_elderflower_syrup", PriceGold: 4, StockQuantity: 10},
	{LocationID: "tavern", ItemID: "mint_tea", PriceGold: 2, StockQuantity: 15},
	{LocationID: "tavern", ItemID: "elderflower_wine", PriceGold: 5, StockQuantity: 10},
	{LocationID: "bakery", ItemID: "bread", PriceGold: 1, StockQuantity: 25},
	{LocationID: "bakery", ItemID: "flour", PriceGold: 3, StockQuantity: 0, BuyPriceGold: 2},
	{LocationID: "market", ItemID: "vegetable_seeds", PriceGold: 1, StockQuantity: 50},
	{LocationID: "blacksmith", ItemID: "iron_ingot", PriceGold: 4, StockQuantity: 10},
	{LocationID: "blacksmith", ItemID: "smithing_hammer", PriceGold: 10, StockQuantity: 3},
	{LocationID: "herbalist", ItemID: "medicinal_herbs", PriceGold: 3, StockQuantity: 15},
	{LocationID: "herbalist", ItemID: "wild_mushrooms", PriceGold: 2, StockQuantity: 12},
	{LocationID: "herbalist", ItemID: "elderflowers", PriceGold: 2, StockQuantity: 15},
	{LocationID: "herbalist", ItemID: "wild_mint", PriceGold: 2, StockQuantity: 12},
	{LocationID: "herbalist", ItemID: "honey_jar", PriceGold: 5, StockQuantity: 8},
	{LocationID: "town_mill", ItemID: "wheat", PriceGold: 2, StockQuantity: 0, BuyPriceGold: 1},
	{LocationID: "town_mill", ItemID: "flour", PriceGold: 3, StockQuantity: 0},
	{LocationID: "stables", ItemID: "raw_mutton", PriceGold: 2, StockQuantity: 15},
	{LocationID: "stables", ItemID: "fresh_milk", PriceGold: 2, StockQuantity: 10},
	{LocationID: "market", ItemID: "wild_apples", PriceGold: 3, StockQuantity: 15},
	{LocationID: "market", ItemID: "dried_fruit", PriceGold: 4, StockQuantity: 10},
	{LocationID: "market", ItemID: "warm_spices", PriceGold: 8, StockQuantity: 5},
	{LocationID: "market", ItemID: "salt", PriceGold: 2, StockQuantity: 20},
	{LocationID: "market", ItemID: "sugar", PriceGold: 4, StockQuantity: 10},
	{LocationID: "tavern", ItemID: "wild_hops", PriceGold: 3, StockQuantity: 10},
	{LocationID: "tavern", ItemID: "blackberries", PriceGold: 2, StockQuantity: 12},
	{LocationID: "tavern", ItemID: "framboise_ale", PriceGold: 4, StockQuantity: 10},
	{LocationID: "bakery", ItemID: "butter", PriceGold: 3, StockQuantity: 10},
	{LocationID: "bakery", ItemID: "eggs", PriceGold: 2, StockQuantity: 15},
	{LocationID: "market", ItemID: "ceramic_bowl", PriceGold: 3, StockQuantity: 10},
	{LocationID: "market", ItemID: "clay_jug", PriceGold: 5, StockQuantity: 8},
	{LocationID: "herbalist", ItemID: "chamomile", PriceGold: 2, StockQuantity: 12},
	{LocationID: "herbalist", ItemID: "chamomile_seeds", PriceGold: 3, StockQuantity: 12},
	{LocationID: "herbalist", ItemID: "hop_seeds", PriceGold: 4, StockQuantity: 10},
	{LocationID: "herbalist", ItemID: "herb_seeds", PriceGold: 2, StockQuantity: 15},
	{LocationID: "herbalist", ItemID: "culinary_herbs", PriceGold: 2, StockQuantity: 15},
	{LocationID: "herbalist", ItemID: "beeswax", PriceGold: 3, StockQuantity: 10},
	{LocationID: "herbalist", ItemID: "flax_fiber", PriceGold: 2, StockQuantity: 15},
	{LocationID: "lumberjack_lodge", ItemID: "pine_resin", PriceGold: 3, StockQuantity: 15},
}

func SeedShopInventory(db *sql.DB) error {
	query := `
	INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity, buy_price_gold)
	VALUES (?, ?, ?, ?, NULLIF(?, 0))
	ON CONFLICT(location_id, item_id) DO UPDATE SET
		price_gold = excluded.price_gold,
		buy_price_gold = excluded.buy_price_gold;
	`
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare shop inventory statement: %w", err)
	}
	defer stmt.Close()

	for _, item := range DefaultShopItems {
		_, err := stmt.Exec(item.LocationID, item.ItemID, item.PriceGold, item.StockQuantity, item.BuyPriceGold)
		if err != nil {
			return fmt.Errorf("failed to seed shop item %s at %s: %w", item.ItemID, item.LocationID, err)
		}
	}
	log.Printf("Successfully seeded %d shop inventory items.", len(DefaultShopItems))
	return nil
}

func seedMillingShopItems(db *sql.DB) error {
	for _, item := range DefaultShopItems {
		if item.LocationID != "town_mill" {
			continue
		}
		if _, err := db.Exec(`
			INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity, buy_price_gold)
			VALUES (?, ?, ?, ?, NULLIF(?, 0))
			ON CONFLICT(location_id, item_id) DO UPDATE SET
				buy_price_gold = excluded.buy_price_gold
		`, item.LocationID, item.ItemID, item.PriceGold, item.StockQuantity, item.BuyPriceGold); err != nil {
			return fmt.Errorf("ensure mill shop item %s: %w", item.ItemID, err)
		}
	}
	return nil
}

// ensureBakerySetup adds the baker to existing worlds and configures the bakery's
// flour buy offer without resetting its current stock or a configured offer.
func ensureBakerySetup(db *sql.DB) error {
	if err := SeedCrafting(db); err != nil {
		return fmt.Errorf("ensure bakery recipes and workstation: %w", err)
	}
	if _, err := db.Exec(`
		INSERT INTO characters (
			id, name, persona_prompt, occupation, gold, hunger, energy,
			current_location, home_location_id, work_location_id, controller_type, mqtt_password
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, "ada_baker", "Ada the Baker",
		"Ada is a patient, practical baker who runs Hearth & Crust Bakery. She values good flour, fresh bread, and fair trade with the village's farmers and miller.",
		"Baker", 25, 90, 100, "bakery", "bakery", "bakery", "agent", "secret_ada_123"); err != nil {
		return fmt.Errorf("ensure baker character: %w", err)
	}
	for _, item := range []struct {
		id       string
		quantity int
	}{{"flour", 4}, {"fresh_water", 2}} {
		if _, err := db.Exec(`
			INSERT INTO inventories (character_id, item_id, quantity)
			VALUES ('ada_baker', ?, ?)
			ON CONFLICT(character_id, item_id) DO NOTHING
		`, item.id, item.quantity); err != nil {
			return fmt.Errorf("seed baker inventory %s: %w", item.id, err)
		}
	}
	if err := InitializeCharacterProgression(db, "ada_baker", "Baker"); err != nil {
		return fmt.Errorf("initialize baker progression: %w", err)
	}
	if _, err := db.Exec(`
		UPDATE locations SET owner_id = 'ada_baker'
		WHERE id = 'bakery' AND (owner_id IS NULL OR owner_id = '')
	`); err != nil {
		return fmt.Errorf("assign bakery owner: %w", err)
	}
	if _, err := db.Exec(`
		UPDATE locations SET rules_prompt = ?
		WHERE id = 'bakery' AND rules_prompt = 'Queue politely for bread purchase.'
	`, "The bakery buys flour for 2 gold per bag and sells bread. Bread is baked from flour and fresh water."); err != nil {
		return fmt.Errorf("update bakery guidance: %w", err)
	}
	if _, err := db.Exec(`
		INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity, buy_price_gold)
		VALUES ('bakery', 'flour', 3, 0, 2)
		ON CONFLICT(location_id, item_id) DO UPDATE SET
			buy_price_gold = COALESCE(location_shop_items.buy_price_gold, excluded.buy_price_gold)
	`); err != nil {
		return fmt.Errorf("configure bakery flour offer: %w", err)
	}
	if _, err := db.Exec(`
		INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity)
		VALUES ('bakery', 'bread', 1, 25)
		ON CONFLICT(location_id, item_id) DO NOTHING
	`); err != nil {
		return fmt.Errorf("ensure initial bakery bread stock: %w", err)
	}
	return nil
}

// BuyItem executes a atomic transaction purchase of items from a location's storefront shop inventory.
func BuyItem(db *sql.DB, characterID string, itemID string, quantity int) (int64, error) {
	if quantity <= 0 {
		return 0, fmt.Errorf("quantity must be greater than 0")
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Get character location and gold
	var locationID string
	var characterGold int
	err = tx.QueryRow("SELECT current_location, gold FROM characters WHERE id = ?", characterID).Scan(&locationID, &characterGold)
	if err != nil {
		return 0, fmt.Errorf("character not found: %w", err)
	}

	// 2. Check shop stock and price
	var priceGold, stockQty int
	err = tx.QueryRow("SELECT price_gold, stock_quantity FROM location_shop_items WHERE location_id = ? AND item_id = ?", locationID, itemID).Scan(&priceGold, &stockQty)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("ITEM_NOT_FOR_SALE: item '%s' is not for sale at this location", itemID)
	} else if err != nil {
		return 0, fmt.Errorf("failed to query shop inventory: %w", err)
	}

	if stockQty < quantity {
		return 0, fmt.Errorf("OUT_OF_STOCK: requested %d, available %d", quantity, stockQty)
	}

	totalCost := priceGold * quantity
	var ownerID sql.NullString
	if err := tx.QueryRow("SELECT owner_id FROM locations WHERE id = ?", locationID).Scan(&ownerID); err != nil {
		return 0, fmt.Errorf("failed to query shop owner: %w", err)
	}
	isOwnerPurchase := ownerID.Valid && ownerID.String == characterID
	if !isOwnerPurchase && characterGold < totalCost {
		return 0, fmt.Errorf("INSUFFICIENT_GOLD: have %d gold, need %d gold", characterGold, totalCost)
	}

	// 3. Check weight and capacity limits
	weightInfo, err := GetCharacterWeightInfo(tx, characterID)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate weight info: %w", err)
	}

	var itemWeight float64
	err = tx.QueryRow("SELECT weight_kg FROM items WHERE id = ?", itemID).Scan(&itemWeight)
	if err != nil {
		return 0, fmt.Errorf("item definition not found: %w", err)
	}

	addedWeight := float64(quantity) * itemWeight
	if weightInfo.TotalWeightKg+addedWeight > weightInfo.OverburdenLimitKg {
		return 0, fmt.Errorf("EXCEEDS_CARRYING_CAPACITY: cannot carry %.1f kg (overburden limit %.1f kg)", weightInfo.TotalWeightKg+addedWeight, weightInfo.OverburdenLimitKg)
	}

	var existingQty int
	err = tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, itemID).Scan(&existingQty)
	if (err == sql.ErrNoRows || existingQty == 0) && weightInfo.SlotsUsed >= weightInfo.MaxSlots {
		return 0, fmt.Errorf("INVENTORY_FULL: inventory slot limit (%d) reached", weightInfo.MaxSlots)
	}

	// 4. Move gold only for purchases between different characters. An owner
	// retrieving shop stock is an internal transfer, not a sale to themselves.
	if !isOwnerPurchase {
		if _, err = tx.Exec("UPDATE characters SET gold = gold - ? WHERE id = ?", totalCost, characterID); err != nil {
			return 0, fmt.Errorf("failed to deduct gold from buyer: %w", err)
		}
	}

	// 5. Credit location owner when the buyer is a different character.
	if ownerID.Valid && ownerID.String != "" && !isOwnerPurchase {
		_, err = tx.Exec("UPDATE characters SET gold = gold + ? WHERE id = ?", totalCost, ownerID.String)
		if err != nil {
			return 0, fmt.Errorf("failed to credit store owner %s: %w", ownerID.String, err)
		}
	}

	// 6. Reduce stock but keep the offer row so any buy offer remains active.
	_, err = tx.Exec("UPDATE location_shop_items SET stock_quantity = stock_quantity - ? WHERE location_id = ? AND item_id = ?", quantity, locationID, itemID)
	if err != nil {
		return 0, fmt.Errorf("failed to update shop stock: %w", err)
	}

	// 7. Add item to buyer inventory
	_, err = tx.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity)
		VALUES (?, ?, ?)
		ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
	`, characterID, itemID, quantity, quantity)
	if err != nil {
		return 0, fmt.Errorf("failed to update character inventory: %w", err)
	}

	return int64(totalCost), tx.Commit()
}

// SellItem sells items to the current location's shop at its posted buy price.
func SellItem(db *sql.DB, characterID, itemID string, quantity int) (int64, error) {
	if quantity <= 0 {
		return 0, fmt.Errorf("quantity must be greater than 0")
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var locationID, status string
	var ownerID sql.NullString
	err = tx.QueryRow(`
		SELECT c.current_location, c.status, l.owner_id
		FROM characters c
		JOIN locations l ON l.id = c.current_location
		WHERE c.id = ?
	`, characterID).Scan(&locationID, &status, &ownerID)
	if err != nil {
		return 0, fmt.Errorf("character or current location not found: %w", err)
	}
	if status != "IDLE" {
		return 0, fmt.Errorf("CHARACTER_BUSY: character is currently %s", status)
	}
	if !ownerID.Valid || ownerID.String == "" {
		return 0, fmt.Errorf("SHOP_NO_OWNER: this shop has no owner to pay for items")
	}
	if ownerID.String == characterID {
		return 0, fmt.Errorf("CANNOT_SELL_TO_OWN_SHOP: use RESTOCK to list items in your shop")
	}

	var buyPrice sql.NullInt64
	err = tx.QueryRow(`
		SELECT buy_price_gold
		FROM location_shop_items
		WHERE location_id = ? AND item_id = ?
	`, locationID, itemID).Scan(&buyPrice)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("ITEM_NOT_BOUGHT: item '%s' is not wanted at this location", itemID)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to query shop buy price: %w", err)
	}
	if !buyPrice.Valid || buyPrice.Int64 <= 0 {
		return 0, fmt.Errorf("ITEM_NOT_BOUGHT: item '%s' is not wanted at this location", itemID)
	}

	var inventoryQuantity int
	err = tx.QueryRow(`
		SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?
	`, characterID, itemID).Scan(&inventoryQuantity)
	if err == sql.ErrNoRows || inventoryQuantity < quantity {
		return 0, fmt.Errorf("INSUFFICIENT_INVENTORY: not enough %s to sell", itemID)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to query character inventory: %w", err)
	}

	maxInt64 := int64(^uint64(0) >> 1)
	if int64(quantity) > maxInt64/buyPrice.Int64 {
		return 0, fmt.Errorf("SALE_VALUE_TOO_LARGE: sale total exceeds supported gold amount")
	}
	totalGold := buyPrice.Int64 * int64(quantity)

	result, err := tx.Exec(`
		UPDATE characters SET gold = gold - ?
		WHERE id = ? AND gold >= ?
	`, totalGold, ownerID.String, totalGold)
	if err != nil {
		return 0, fmt.Errorf("failed to debit shop owner: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check shop owner funds: %w", err)
	}
	if rowsAffected == 0 {
		return 0, fmt.Errorf("SHOP_INSUFFICIENT_GOLD: shop owner cannot afford this purchase")
	}

	if inventoryQuantity == quantity {
		result, err = tx.Exec(`DELETE FROM inventories WHERE character_id = ? AND item_id = ? AND quantity = ?`, characterID, itemID, quantity)
	} else {
		result, err = tx.Exec(`
			UPDATE inventories SET quantity = quantity - ?
			WHERE character_id = ? AND item_id = ? AND quantity >= ?
		`, quantity, characterID, itemID, quantity)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to deduct sold items: %w", err)
	}
	rowsAffected, err = result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check sold item deduction: %w", err)
	}
	if rowsAffected == 0 {
		return 0, fmt.Errorf("INSUFFICIENT_INVENTORY: not enough %s to sell", itemID)
	}

	if _, err := tx.Exec(`UPDATE characters SET gold = gold + ? WHERE id = ?`, totalGold, characterID); err != nil {
		return 0, fmt.Errorf("failed to credit seller: %w", err)
	}
	result, err = tx.Exec(`
		UPDATE location_shop_items SET stock_quantity = stock_quantity + ?
		WHERE location_id = ? AND item_id = ? AND buy_price_gold = ?
	`, quantity, locationID, itemID, buyPrice.Int64)
	if err != nil {
		return 0, fmt.Errorf("failed to add sold items to shop stock: %w", err)
	}
	rowsAffected, err = result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check shop stock update: %w", err)
	}
	if rowsAffected == 0 {
		return 0, fmt.Errorf("ITEM_NOT_BOUGHT: item '%s' is no longer wanted at this location", itemID)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit sale: %w", err)
	}
	return totalGold, nil
}

// RestockShopItem transfers items from a character's personal inventory into a location's shop inventory.
func RestockShopItem(db *sql.DB, characterID string, itemID string, quantity int, priceGold int) error {
	if quantity <= 0 {
		return fmt.Errorf("quantity must be greater than 0")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Get character current location and verify they own its shop.
	var locationID string
	var ownerID sql.NullString
	err = tx.QueryRow(`
		SELECT c.current_location, l.owner_id
		FROM characters c
		JOIN locations l ON l.id = c.current_location
		WHERE c.id = ?
	`, characterID).Scan(&locationID, &ownerID)
	if err != nil {
		return fmt.Errorf("character not found: %w", err)
	}
	if !ownerID.Valid || ownerID.String != characterID {
		return fmt.Errorf("SHOP_NOT_OWNER: only the shop owner can restock this location")
	}

	// 2. Check character inventory
	var charQty int
	err = tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, itemID).Scan(&charQty)
	if err == sql.ErrNoRows || charQty < quantity {
		return fmt.Errorf("INSUFFICIENT_INVENTORY: character has %d of item '%s', needed %d", charQty, itemID, quantity)
	}

	// 3. Determine price (if priceGold <= 0, use existing shop price or default item value)
	var finalPrice int = priceGold
	if finalPrice <= 0 {
		err = tx.QueryRow("SELECT price_gold FROM location_shop_items WHERE location_id = ? AND item_id = ?", locationID, itemID).Scan(&finalPrice)
		if err != nil {
			err = tx.QueryRow("SELECT value_gold FROM items WHERE id = ?", itemID).Scan(&finalPrice)
			if err != nil || finalPrice <= 0 {
				finalPrice = 1
			}
		}
	}

	// 4. Deduct items from character inventory
	if charQty == quantity {
		_, err = tx.Exec("DELETE FROM inventories WHERE character_id = ? AND item_id = ?", characterID, itemID)
	} else {
		_, err = tx.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", quantity, characterID, itemID)
	}
	if err != nil {
		return fmt.Errorf("failed to deduct items from character inventory: %w", err)
	}

	// 5. Upsert location shop items stock
	_, err = tx.Exec(`
		INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(location_id, item_id) DO UPDATE SET
			stock_quantity = stock_quantity + ?,
			price_gold = CASE WHEN ? > 0 THEN ? ELSE price_gold END
	`, locationID, itemID, finalPrice, quantity, quantity, finalPrice, finalPrice)
	if err != nil {
		return fmt.Errorf("failed to update location shop inventory: %w", err)
	}

	return tx.Commit()
}
