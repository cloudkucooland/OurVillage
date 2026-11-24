package db

import (
	"database/sql"
	"fmt"
	"time"
)

type NoticeBoard struct {
	ID         string `json:"id"`
	LocationID string `json:"location_id"`
	Name       string `json:"name"`
}

type Contract struct {
	ID                 string `json:"id"`
	BoardID            string `json:"board_id"`
	CreatorID          string `json:"creator_id"`
	AssigneeID         string `json:"assignee_id,omitempty"`
	ContractType       string `json:"contract_type"` // 'BUY_ORDER', 'SELL_ORDER', 'SERVICE_BOUNTY'
	RequiredItemID     string `json:"required_item_id,omitempty"`
	RequiredQuantity   int    `json:"required_quantity"`
	RewardGold         int    `json:"reward_gold"`
	EscrowHeldGold     int    `json:"escrow_held_gold"`
	Status             string `json:"status"` // 'OPEN', 'IN_PROGRESS', 'COMPLETED', 'EXPIRED', 'CANCELLED'
	CreatedAtTick      uint64 `json:"created_at_tick"`
	ExpiresAtTick      uint64 `json:"expires_at_tick"`
	DeliveryLocationID string `json:"delivery_location_id"`
}

var DefaultNoticeBoards = []NoticeBoard{
	{ID: "town_square_board", LocationID: "town_square", Name: "Town Square Notice Board"},
	{ID: "tavern_board", LocationID: "tavern", Name: "Tavern Notice Board"},
}

func SeedNoticeBoards(db *sql.DB) error {
	query := `
	INSERT INTO notice_boards (id, location_id, name)
	VALUES (?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		location_id=excluded.location_id,
		name=excluded.name;
	`
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare notice board statement: %w", err)
	}
	defer stmt.Close()

	for _, b := range DefaultNoticeBoards {
		if _, err := stmt.Exec(b.ID, b.LocationID, b.Name); err != nil {
			return fmt.Errorf("failed to seed notice board %s: %w", b.ID, err)
		}
	}
	return nil
}

// PostContract creates a contract and places creator's reward gold into trustless escrow.
func PostContract(db *sql.DB, creatorID, boardID, contractType, requiredItemID string, requiredQty, rewardGold int, deliveryLocID string, currentTick, expiresTick uint64) (string, error) {
	if rewardGold < 0 {
		return "", fmt.Errorf("reward gold cannot be negative")
	}

	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Check creator gold
	var currentGold int
	err = tx.QueryRow("SELECT gold FROM characters WHERE id = ?", creatorID).Scan(&currentGold)
	if err != nil {
		return "", fmt.Errorf("creator character not found: %w", err)
	}

	if currentGold < rewardGold {
		return "", fmt.Errorf("insufficient gold for escrow: have %d, required %d", currentGold, rewardGold)
	}

	// 2. Deduct gold from creator (escrow lock)
	_, err = tx.Exec("UPDATE characters SET gold = gold - ? WHERE id = ?", rewardGold, creatorID)
	if err != nil {
		return "", fmt.Errorf("failed to escrow gold: %w", err)
	}

	// 3. Insert contract
	contractID := fmt.Sprintf("contract_%d", time.Now().UnixNano())
	_, err = tx.Exec(`
		INSERT INTO contracts (id, board_id, creator_id, contract_type, required_item_id, required_quantity, reward_gold, escrow_held_gold, status, created_at_tick, expires_at_tick, delivery_location_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'OPEN', ?, ?, ?)
	`, contractID, boardID, creatorID, contractType, requiredItemID, requiredQty, rewardGold, rewardGold, currentTick, expiresTick, deliveryLocID)
	if err != nil {
		return "", fmt.Errorf("failed to insert contract: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit contract post: %w", err)
	}

	return contractID, nil
}

// AcceptContract assigns an open contract to an accepting agent.
func AcceptContract(db *sql.DB, characterID, contractID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var status, creatorID string
	err = tx.QueryRow("SELECT status, creator_id FROM contracts WHERE id = ?", contractID).Scan(&status, &creatorID)
	if err != nil {
		return fmt.Errorf("contract not found: %w", err)
	}

	if status != "OPEN" {
		return fmt.Errorf("contract is not open (status: %s)", status)
	}

	if creatorID == characterID {
		return fmt.Errorf("cannot accept your own contract")
	}

	_, err = tx.Exec("UPDATE contracts SET assignee_id = ?, status = 'IN_PROGRESS' WHERE id = ?", characterID, contractID)
	if err != nil {
		return fmt.Errorf("failed to assign contract: %w", err)
	}

	return tx.Commit()
}

// FulfillContract verifies contractor has delivered required items, releases escrowed gold to contractor, and completes contract.
func FulfillContract(db *sql.DB, characterID, contractID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var assigneeID, contractType, requiredItemID string
	var requiredQty, escrowGold int
	var deliveryLocID string
	var status string

	err = tx.QueryRow(`
		SELECT status, assignee_id, contract_type, required_item_id, required_quantity, escrow_held_gold, delivery_location_id 
		FROM contracts WHERE id = ?
	`, contractID).Scan(&status, &assigneeID, &contractType, &requiredItemID, &requiredQty, &escrowGold, &deliveryLocID)
	if err != nil {
		return fmt.Errorf("contract not found: %w", err)
	}

	if status != "IN_PROGRESS" && status != "OPEN" {
		return fmt.Errorf("contract cannot be fulfilled in status: %s", status)
	}

	if assigneeID != "" && assigneeID != characterID {
		return fmt.Errorf("contract is assigned to another agent")
	}

	// For BUY_ORDER / procurement: Contractor must provide required items
	if requiredItemID != "" && requiredQty > 0 {
		var charQty int
		err = tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", characterID, requiredItemID).Scan(&charQty)
		if err != nil || charQty < requiredQty {
			return fmt.Errorf("insufficient items in inventory: have %d, required %d of %s", charQty, requiredQty, requiredItemID)
		}

		// Deduct items from contractor inventory
		if charQty == requiredQty {
			_, _ = tx.Exec("DELETE FROM inventories WHERE character_id = ? AND item_id = ?", characterID, requiredItemID)
		} else {
			_, _ = tx.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", requiredQty, characterID, requiredItemID)
		}

		// Transfer items to delivery location or drop items
		_, _ = tx.Exec(`
			INSERT INTO location_items (location_id, item_id, quantity, dropped_by, dropped_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(location_id, item_id) DO UPDATE SET quantity = quantity + ?
		`, deliveryLocID, requiredItemID, requiredQty, characterID, requiredQty)
	}

	// Release escrowed gold to contractor
	_, err = tx.Exec("UPDATE characters SET gold = gold + ? WHERE id = ?", escrowGold, characterID)
	if err != nil {
		return fmt.Errorf("failed to pay reward gold to contractor: %w", err)
	}

	// Mark contract completed
	_, err = tx.Exec("UPDATE contracts SET status = 'COMPLETED', assignee_id = COALESCE(assignee_id, ?) WHERE id = ?", characterID, contractID)
	if err != nil {
		return fmt.Errorf("failed to update contract status: %w", err)
	}

	return tx.Commit()
}

// CancelContract cancels an open or in-progress contract and refunds escrowed gold to creator.
func CancelContract(db *sql.DB, characterID, contractID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var creatorID, status string
	var escrowGold int

	err = tx.QueryRow("SELECT creator_id, status, escrow_held_gold FROM contracts WHERE id = ?", contractID).Scan(&creatorID, &status, &escrowGold)
	if err != nil {
		return fmt.Errorf("contract not found: %w", err)
	}

	if creatorID != characterID {
		return fmt.Errorf("only contract creator can cancel contract")
	}

	if status == "COMPLETED" || status == "CANCELLED" {
		return fmt.Errorf("contract is already finalized")
	}

	// Refund escrowed gold to creator
	if escrowGold > 0 {
		_, err = tx.Exec("UPDATE characters SET gold = gold + ? WHERE id = ?", escrowGold, creatorID)
		if err != nil {
			return fmt.Errorf("failed to refund escrow gold: %w", err)
		}
	}

	_, err = tx.Exec("UPDATE contracts SET status = 'CANCELLED' WHERE id = ?", contractID)
	if err != nil {
		return fmt.Errorf("failed to update contract status: %w", err)
	}

	return tx.Commit()
}
