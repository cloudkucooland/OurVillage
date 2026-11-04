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
}

var DefaultShopItems = []ShopItemSeed{
	{LocationID: "tavern", ItemID: "ale_mug", PriceGold: 2, StockQuantity: 20},
	{LocationID: "tavern", ItemID: "roasted_mutton", PriceGold: 3, StockQuantity: 15},
	{LocationID: "bakery", ItemID: "bread", PriceGold: 1, StockQuantity: 25},
	{LocationID: "blacksmith", ItemID: "iron_ingot", PriceGold: 4, StockQuantity: 10},
	{LocationID: "blacksmith", ItemID: "smithing_hammer", PriceGold: 10, StockQuantity: 3},
	{LocationID: "herbalist", ItemID: "medicinal_herbs", PriceGold: 3, StockQuantity: 15},
	{LocationID: "herbalist", ItemID: "wild_mushrooms", PriceGold: 2, StockQuantity: 12},
}

func SeedShopInventory(db *sql.DB) error {
	query := `
	INSERT INTO location_shop_items (location_id, item_id, price_gold, stock_quantity)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(location_id, item_id) DO UPDATE SET
		price_gold = excluded.price_gold,
		stock_quantity = excluded.stock_quantity;
	`
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare shop inventory statement: %w", err)
	}
	defer stmt.Close()

	for _, item := range DefaultShopItems {
		_, err := stmt.Exec(item.LocationID, item.ItemID, item.PriceGold, item.StockQuantity)
		if err != nil {
			return fmt.Errorf("failed to seed shop item %s at %s: %w", item.ItemID, item.LocationID, err)
		}
	}
	log.Printf("Successfully seeded %d shop inventory items.", len(DefaultShopItems))
	return nil
}

// BuyItem executes a atomic transaction purchase of items from a location's storefront shop inventory.
func BuyItem(db *sql.DB, characterID string, itemID string, quantity int) error {
	if quantity <= 0 {
		return fmt.Errorf("quantity must be greater than 0")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Get character location and gold
	var locationID string
	var characterGold int
	err = tx.QueryRow("SELECT current_location, gold FROM characters WHERE id = ?", characterID).Scan(&locationID, &characterGold)
	if err != nil {
		return fmt.Errorf("character not found: %w", err)
	}

	// 2. Check shop stock and price
	var priceGold, stockQty int
	err = tx.QueryRow("SELECT price_gold, stock_quantity FROM location_shop_items WHERE location_id = ? AND item_id = ?", locationID, itemID).Scan(&priceGold, &stockQty)
	if err == sql.ErrNoRows {
		return fmt.Errorf("ITEM_NOT_FOR_SALE: item '%s' is not for sale at this location", itemID)
	} else if err != nil {
		return fmt.Errorf("failed to query shop inventory: %w", err)
	}

	if stockQty < quantity {
		return fmt.Errorf("OUT_OF_STOCK: requested %d, available %d", quantity, stockQty)
	}

	totalCost := priceGold * quantity
	if characterGold < totalCost {
		return fmt.Errorf("INSUFFICIENT_GOLD: have %d gold, need %d gold", characterGold, totalCost)
	}

	// 3. Check weight and capacity limits
	weightInfo, err := GetCharacterWeightInfo(tx, characterID)
	if err != nil {
		return fmt.Errorf("failed to calculate weight info: %w", err)
	}

	var itemWeight float64
	err = tx.QueryRow("SELECT weight_kg FROM items WHERE id = ?", itemID).Scan(&itemWeight)
	if err != nil {
		return fmt.Errorf("item definition not found: %w", err)
	}

	addedWeight := float64(quantity) * itemWeight
	if weightInfo.TotalWeightKg+addedWeight > weightInfo.OverburdenLimitKg {
		return fmt.Errorf("EXCEEDS_CARRYING_CAPACITY: cannot carry %.1f kg (overburden limit %.1f kg)", weightInfo.TotalWeightKg+addedWeight, weightInfo.OverburdenLimitKg)
	}

	var existingQty int
	err = tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, itemID).Scan(&existingQty)
	if (err == sql.ErrNoRows || existingQty == 0) && weightInfo.SlotsUsed >= weightInfo.MaxSlots {
		return fmt.Errorf("INVENTORY_FULL: inventory slot limit (%d) reached", weightInfo.MaxSlots)
	}

	// 4. Deduct gold from buyer
	_, err = tx.Exec("UPDATE characters SET gold = gold - ? WHERE id = ?", totalCost, characterID)
	if err != nil {
		return fmt.Errorf("failed to deduct gold from buyer: %w", err)
	}

	// 5. Credit location owner if present
	var ownerID sql.NullString
	err = tx.QueryRow("SELECT owner_id FROM locations WHERE id = ?", locationID).Scan(&ownerID)
	if err == nil && ownerID.Valid && ownerID.String != "" && ownerID.String != characterID {
		_, err = tx.Exec("UPDATE characters SET gold = gold + ? WHERE id = ?", totalCost, ownerID.String)
		if err != nil {
			return fmt.Errorf("failed to credit store owner %s: %w", ownerID.String, err)
		}
	}

	// 6. Reduce shop stock (delete row if stock depleted)
	if stockQty == quantity {
		_, err = tx.Exec("DELETE FROM location_shop_items WHERE location_id = ? AND item_id = ?", locationID, itemID)
	} else {
		_, err = tx.Exec("UPDATE location_shop_items SET stock_quantity = stock_quantity - ? WHERE location_id = ? AND item_id = ?", quantity, locationID, itemID)
	}
	if err != nil {
		return fmt.Errorf("failed to update shop stock: %w", err)
	}

	// 7. Add item to buyer inventory
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

	// 1. Get character current location
	var locationID string
	err = tx.QueryRow("SELECT current_location FROM characters WHERE id = ?", characterID).Scan(&locationID)
	if err != nil {
		return fmt.Errorf("character not found: %w", err)
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
