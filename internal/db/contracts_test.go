package db

import (
	"testing"
)

func TestNoticeBoardAndContractLifecycle(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize in-memory DB: %v", err)
	}
	defer database.Close()

	// 1. Give Eldon 30 gold
	_, err = database.Exec("UPDATE characters SET gold = 30 WHERE id = 'eldon_millwright'")
	if err != nil {
		t.Fatalf("Failed to update test character: %v", err)
	}

	// 2. Eldon posts a buy order for 5 firewood with 20 gold reward (escrowed)
	contractID, err := PostContract(database, "eldon_millwright", "town_square_board", "BUY_ORDER", "firewood", 5, 20, "town_mill", 1, 100)
	if err != nil {
		t.Fatalf("Failed to post contract: %v", err)
	}

	// Verify Eldon's gold is reduced from 30 to 10 due to escrow
	var eldonGold int
	_ = database.QueryRow("SELECT gold FROM characters WHERE id = 'eldon_millwright'").Scan(&eldonGold)
	if eldonGold != 10 {
		t.Errorf("Expected Eldon gold to be 10 after escrow, got %d", eldonGold)
	}

	// 3. Martha accepts the contract
	err = AcceptContract(database, "martha_barkeep", contractID)
	if err != nil {
		t.Fatalf("Failed to accept contract: %v", err)
	}

	// Verify contract status is IN_PROGRESS and assignee is martha_barkeep
	var status, assignee string
	_ = database.QueryRow("SELECT status, assignee_id FROM contracts WHERE id = ?", contractID).Scan(&status, &assignee)
	if status != "IN_PROGRESS" || assignee != "martha_barkeep" {
		t.Errorf("Expected contract IN_PROGRESS assigned to martha_barkeep, got status=%s, assignee=%s", status, assignee)
	}

	// 4. Give Martha 5 firewood in inventory
	_, _ = database.Exec("INSERT OR REPLACE INTO inventories (character_id, item_id, quantity) VALUES ('martha_barkeep', 'firewood', 5)")

	// 5. Martha fulfills the contract
	err = FulfillContract(database, "martha_barkeep", contractID)
	if err != nil {
		t.Fatalf("Failed to fulfill contract: %v", err)
	}

	// Verify Martha received 20 gold reward
	var marthaGold int
	_ = database.QueryRow("SELECT gold FROM characters WHERE id = 'martha_barkeep'").Scan(&marthaGold)
	if marthaGold != 50 { // started with 30 + 20 reward = 50
		t.Errorf("Expected Martha gold to be 50, got %d", marthaGold)
	}

	// Verify contract status is COMPLETED
	_ = database.QueryRow("SELECT status FROM contracts WHERE id = ?", contractID).Scan(&status)
	if status != "COMPLETED" {
		t.Errorf("Expected contract COMPLETED, got %s", status)
	}
}
