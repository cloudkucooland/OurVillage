package admin

import (
	"database/sql"
	"fmt"
	"strings"
)

type AdminCommandExecutor struct {
	db         *sql.DB
	setWeather func(string)
}

func NewAdminCommandExecutor(db *sql.DB, setWeather func(string)) *AdminCommandExecutor {
	return &AdminCommandExecutor{
		db:         db,
		setWeather: setWeather,
	}
}

func (a *AdminCommandExecutor) SetWeather(weather string) error {
	w := strings.ToLower(weather)
	if w != "clear" && w != "rain" && w != "snow" && w != "storm" {
		return fmt.Errorf("invalid weather %q", weather)
	}
	if a.setWeather != nil {
		a.setWeather(w)
	}
	return nil
}

func (a *AdminCommandExecutor) SetLocationDescription(locID, desc string) error {
	_, err := a.db.Exec("UPDATE locations SET description = ? WHERE id = ?", desc, locID)
	return err
}

func (a *AdminCommandExecutor) SetLocationOwner(locID, charID string) error {
	_, err := a.db.Exec("UPDATE locations SET owner_id = ? WHERE id = ?", charID, locID)
	return err
}

func (a *AdminCommandExecutor) AddItemToCharacter(charID, itemID string, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("quantity must be > 0")
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
	_, err := a.db.Exec("UPDATE characters SET gold = ? WHERE id = ?", amount, charID)
	return err
}
