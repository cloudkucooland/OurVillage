package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestBuyItemStorefront(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize in-memory DB: %v", err)
	}
	defer database.Close()

	// Place eldon_millwright at tavern and give him 10 gold
	_, err = database.Exec("UPDATE characters SET current_location = 'tavern', gold = 10 WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to update test character: %v", err)
	}

	// 1. Successful purchase of ale_mug at tavern (price: 2 gold)
	err = BuyItem(database, "eldon_millwright", "ale_mug", 2)
	if err != nil {
		t.Fatalf("Expected successful purchase, got error: %v", err)
	}

	// Verify gold: started with 10, bought 2 ale_mugs at 2 gold each = cost 4 gold. Remaining = 6 gold.
	var gold int
	err = database.QueryRow("SELECT gold FROM characters WHERE id = 'eldon_millwright'").Scan(&gold)
	if err != nil || gold != 6 {
		t.Errorf("Expected 6 gold remaining, got %d (err: %v)", gold, err)
	}

	// Verify inventory: 2 ale_mugs added
	var qty int
	err = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'ale_mug'").Scan(&qty)
	if err != nil || qty != 2 {
		t.Errorf("Expected 2 ale_mugs in inventory, got %d (err: %v)", qty, err)
	}

	// Verify shop stock: started with 20, reduced by 2 = 18
	var stock int
	err = database.QueryRow("SELECT stock_quantity FROM location_shop_items WHERE location_id = 'tavern' AND item_id = 'ale_mug'").Scan(&stock)
	if err != nil || stock != 18 {
		t.Errorf("Expected 18 stock remaining in tavern, got %d (err: %v)", stock, err)
	}

	// 2. Insufficient gold test
	// Eldon has 6 gold. Try to buy smithing_hammer (costs 10 gold each)
	err = BuyItem(database, "eldon_millwright", "smithing_hammer", 1)
	if err == nil {
		t.Errorf("Expected INSUFFICIENT_GOLD error, got nil")
	}

	// 3. Item not for sale at location test (trying to buy bread at blacksmith instead of bakery)
	_, _ = database.Exec("UPDATE characters SET current_location = 'blacksmith' WHERE id = 'eldon_millwright'")
	err = BuyItem(database, "eldon_millwright", "bread", 1)
	if err == nil {
		t.Errorf("Expected ITEM_NOT_FOR_SALE error, got nil")
	}

	// 4. Out of stock test (buy more than available stock)
	_, _ = database.Exec("UPDATE characters SET current_location = 'tavern' WHERE id = 'eldon_millwright'")
	err = BuyItem(database, "eldon_millwright", "ale_mug", 100)
	if err == nil {
		t.Errorf("Expected OUT_OF_STOCK error, got nil")
	}
}

func TestRestockShopItem(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize in-memory DB: %v", err)
	}
	defer database.Close()

	_, err = database.Exec("UPDATE characters SET current_location = 'tavern' WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to update test character: %v", err)
	}

	_, err = database.Exec("INSERT INTO inventories (character_id, item_id, quantity) VALUES ('eldon_millwright', 'bread', 5)")
	if err != nil {
		t.Fatalf("Failed to insert inventory: %v", err)
	}

	err = RestockShopItem(database, "eldon_millwright", "bread", 3, 2)
	if err != nil {
		t.Fatalf("Expected successful restock, got error: %v", err)
	}

	var invQty int
	err = database.QueryRow("SELECT quantity FROM inventories WHERE character_id = 'eldon_millwright' AND item_id = 'bread'").Scan(&invQty)
	if err != nil || invQty != 2 {
		t.Errorf("Expected 2 bread remaining in inventory, got %d (err: %v)", invQty, err)
	}

	var shopStock, shopPrice int
	err = database.QueryRow("SELECT stock_quantity, price_gold FROM location_shop_items WHERE location_id = 'tavern' AND item_id = 'bread'").Scan(&shopStock, &shopPrice)
	if err != nil || shopStock != 3 || shopPrice != 2 {
		t.Errorf("Expected 3 stock at price 2 in tavern, got stock %d price %d (err: %v)", shopStock, shopPrice, err)
	}

	err = RestockShopItem(database, "eldon_millwright", "bread", 10, 2)
	if err == nil {
		t.Errorf("Expected INSUFFICIENT_INVENTORY error, got nil")
	}
}

func TestSellItemBuysAtPostedPriceAndPreservesStockOnReseed(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		UPDATE characters SET current_location = 'town_mill', gold = 0
		WHERE id = 'luke_farmer'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE characters SET gold = 10 WHERE id = 'eldon_millwright'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity)
		VALUES ('luke_farmer', 'wheat', 3)
		ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = excluded.quantity
	`); err != nil {
		t.Fatal(err)
	}

	totalGold, err := SellItem(database, "luke_farmer", "wheat", 2)
	if err != nil {
		t.Fatalf("SellItem() error = %v", err)
	}
	if totalGold != 2 {
		t.Fatalf("sale total = %d, want 2", totalGold)
	}

	var farmerGold, millwrightGold, wheatInventory, wheatStock int
	for query, dest := range map[string]*int{
		"SELECT gold FROM characters WHERE id = 'luke_farmer'":                                                 &farmerGold,
		"SELECT gold FROM characters WHERE id = 'eldon_millwright'":                                            &millwrightGold,
		"SELECT quantity FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'wheat'":            &wheatInventory,
		"SELECT stock_quantity FROM location_shop_items WHERE location_id = 'town_mill' AND item_id = 'wheat'": &wheatStock,
	} {
		if err := database.QueryRow(query).Scan(dest); err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
	}
	if farmerGold != 2 || millwrightGold != 8 || wheatInventory != 1 || wheatStock != 2 {
		t.Errorf("sale state: farmer gold=%d, millwright gold=%d, carried wheat=%d, shop stock=%d",
			farmerGold, millwrightGold, wheatInventory, wheatStock)
	}

	if err := SeedShopInventory(database); err != nil {
		t.Fatalf("reseed shop inventory: %v", err)
	}
	if err := database.QueryRow(`
		SELECT stock_quantity FROM location_shop_items
		WHERE location_id = 'town_mill' AND item_id = 'wheat'
	`).Scan(&wheatStock); err != nil {
		t.Fatal(err)
	}
	if wheatStock != 2 {
		t.Errorf("shop stock after reseed = %d, want 2", wheatStock)
	}
}

func TestSellItemRejectsUnwantedItemsAndUnfundedShops(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		UPDATE characters SET current_location = 'town_mill', gold = 0 WHERE id = 'luke_farmer'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO inventories (character_id, item_id, quantity)
		VALUES ('luke_farmer', 'wheat', 2), ('luke_farmer', 'flour', 1)
		ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = excluded.quantity
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE characters SET gold = 1 WHERE id = 'eldon_millwright'`); err != nil {
		t.Fatal(err)
	}

	if _, err := SellItem(database, "luke_farmer", "wheat", 2); err == nil {
		t.Fatal("expected sale to fail when the shop owner cannot afford it")
	}
	if _, err := SellItem(database, "luke_farmer", "flour", 1); err == nil {
		t.Fatal("expected sale to fail for an item the shop does not buy")
	}

	var wheatInventory, flourInventory, farmerGold, wheatStock int
	for query, dest := range map[string]*int{
		"SELECT quantity FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'wheat'":            &wheatInventory,
		"SELECT quantity FROM inventories WHERE character_id = 'luke_farmer' AND item_id = 'flour'":            &flourInventory,
		"SELECT gold FROM characters WHERE id = 'luke_farmer'":                                                 &farmerGold,
		"SELECT stock_quantity FROM location_shop_items WHERE location_id = 'town_mill' AND item_id = 'wheat'": &wheatStock,
	} {
		if err := database.QueryRow(query).Scan(dest); err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
	}
	if wheatInventory != 2 || flourInventory != 1 || farmerGold != 0 || wheatStock != 0 {
		t.Errorf("failed sales changed state: wheat=%d flour=%d gold=%d shop stock=%d",
			wheatInventory, flourInventory, farmerGold, wheatStock)
	}
}

func TestMigrateShopBuyPriceColumn(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		CREATE TABLE location_shop_items (
			location_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			price_gold INTEGER NOT NULL,
			stock_quantity INTEGER NOT NULL DEFAULT 10,
			PRIMARY KEY (location_id, item_id)
		)
	`); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(database); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`PRAGMA table_info(location_shop_items)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		found = found || name == "buy_price_gold"
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("migration did not add location_shop_items.buy_price_gold")
	}
}

func TestExistingWorldReceivesSellOffersAndRecipeKnowledge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.db")
	database, err := InitDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := SeedDB(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO characters (
			id, name, persona_prompt, occupation, gold, hunger, energy,
			current_location, home_location_id, controller_type, mqtt_password
		) VALUES ('scotus', 'Scotus', '', 'Monk', 10, 100, 100, 'town_mill', 'town_mill', 'agent', 'test')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE location_shop_items SET stock_quantity = 5
		WHERE location_id = 'town_mill' AND item_id = 'wheat'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE location_shop_items SET price_gold = 4
		WHERE location_id = 'town_mill' AND item_id = 'wheat'
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM recipes WHERE id = 'mill_flour'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM workstations WHERE id = 'millstones'`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = InitDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var eldonKnows, scotusKnows, wheatBuyPrice, wheatStock, wheatSellPrice, recipeCount, workstationCount int
	for query, dest := range map[string]*int{
		"SELECT COUNT(*) FROM character_known_recipes WHERE character_id = 'eldon_millwright' AND recipe_id = 'mill_flour'": &eldonKnows,
		"SELECT COUNT(*) FROM character_known_recipes WHERE character_id = 'scotus' AND recipe_id = 'mill_flour'":           &scotusKnows,
		"SELECT buy_price_gold FROM location_shop_items WHERE location_id = 'town_mill' AND item_id = 'wheat'":              &wheatBuyPrice,
		"SELECT stock_quantity FROM location_shop_items WHERE location_id = 'town_mill' AND item_id = 'wheat'":              &wheatStock,
		"SELECT price_gold FROM location_shop_items WHERE location_id = 'town_mill' AND item_id = 'wheat'":                  &wheatSellPrice,
		"SELECT COUNT(*) FROM recipes WHERE id = 'mill_flour'":                                                              &recipeCount,
		"SELECT COUNT(*) FROM workstations WHERE id = 'millstones'":                                                         &workstationCount,
	} {
		if err := database.QueryRow(query).Scan(dest); err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
	}
	if eldonKnows != 1 || scotusKnows != 1 || wheatBuyPrice != 1 || wheatStock != 5 ||
		wheatSellPrice != 4 || recipeCount != 1 || workstationCount != 1 {
		t.Errorf("existing-world migration: Eldon recipe=%d Scotus recipe=%d wheat buy price=%d sale price=%d stock=%d recipe count=%d workstation count=%d",
			eldonKnows, scotusKnows, wheatBuyPrice, wheatSellPrice, wheatStock, recipeCount, workstationCount)
	}
}
