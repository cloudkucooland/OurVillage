package db

import (
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
