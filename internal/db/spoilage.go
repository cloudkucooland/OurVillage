package db

import (
	"database/sql"
	"log"
)

// ProcessSpoilageTicks degrades freshness of perishable items in character inventories
// and transmutes expired items into rotting waste (RFC 009).
func ProcessSpoilageTicks(database *sql.DB, currentTick uint64) error {
	rows, err := database.Query(`
		SELECT inv.character_id, inv.item_id, inv.quantity, inv.freshness_percent, items.shelf_life_ticks, items.spoilage_item_id
		FROM inventories inv
		JOIN items ON inv.item_id = items.id
		WHERE items.is_perishable = 1 AND items.shelf_life_ticks > 0
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type perishableInv struct {
		characterID    string
		itemID         string
		quantity       int
		freshness      int
		shelfLife      int
		spoilageItemID string
	}

	var itemsToDecay []perishableInv
	for rows.Next() {
		var pi perishableInv
		if err := rows.Scan(&pi.characterID, &pi.itemID, &pi.quantity, &pi.freshness, &pi.shelfLife, &pi.spoilageItemID); err != nil {
			continue
		}
		itemsToDecay = append(itemsToDecay, pi)
	}

	for _, pi := range itemsToDecay {
		decayPerTick := 100.0 / float64(pi.shelfLife)
		newFreshness := float64(pi.freshness) - decayPerTick
		if newFreshness <= 0 {
			spoilageID := pi.spoilageItemID
			if spoilageID == "" {
				spoilageID = "rotting_waste"
			}
			_, _ = database.Exec(`
				DELETE FROM inventories WHERE character_id = ? AND item_id = ?
			`, pi.characterID, pi.itemID)

			_, _ = database.Exec(`
				INSERT INTO inventories (character_id, item_id, quantity, freshness_percent)
				VALUES (?, ?, ?, 100)
				ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
			`, pi.characterID, spoilageID, pi.quantity, pi.quantity)

			log.Printf("[Spoilage] Character %s's %s (%d) spoiled into %s at tick %d", pi.characterID, pi.itemID, pi.quantity, spoilageID, currentTick)
		} else {
			_, _ = database.Exec(`
				UPDATE inventories SET freshness_percent = ? WHERE character_id = ? AND item_id = ?
			`, int(newFreshness), pi.characterID, pi.itemID)
		}
	}
	return nil
}
