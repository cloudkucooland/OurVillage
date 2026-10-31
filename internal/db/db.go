package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// InitDB initializes the SQLite database and executes the schema from RFC 003.
func InitDB(dataSourceName string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite db: %w", err)
	}

	schema := `
-- Locations Table
CREATE TABLE IF NOT EXISTS locations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    owner_id TEXT,
    is_public_property BOOLEAN DEFAULT 0,
    open_time TEXT,
    close_time TEXT,
    rules_prompt TEXT
);

-- Location Operators
CREATE TABLE IF NOT EXISTS location_operators (
    location_id TEXT NOT NULL,
    character_id TEXT NOT NULL,
    granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (location_id, character_id)
);

-- Characters Table
CREATE TABLE IF NOT EXISTS characters (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    persona_prompt TEXT NOT NULL,
    occupation TEXT NOT NULL,
    gold INTEGER NOT NULL DEFAULT 10,
    hunger INTEGER NOT NULL DEFAULT 100,
    current_location TEXT NOT NULL DEFAULT 'town_square',
    is_online BOOLEAN NOT NULL DEFAULT 0,
    controller_type TEXT NOT NULL DEFAULT 'agent',
    mqtt_password TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Items & Inventories Table
CREATE TABLE IF NOT EXISTS items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    is_consumable BOOLEAN DEFAULT 0,
    hunger_restore INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS inventories (
    character_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, item_id)
);
`

	_, err = db.Exec(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to execute schema: %w", err)
	}

	if err := SeedLocations(db); err != nil {
		return nil, fmt.Errorf("failed to seed locations: %w", err)
	}

	if err := SeedItemsAndCharacters(db); err != nil {
		return nil, fmt.Errorf("failed to seed items and characters: %w", err)
	}

	return db, nil
}
