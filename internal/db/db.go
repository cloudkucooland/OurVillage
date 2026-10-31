package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// InitDB initializes the SQLite database, migrates any missing columns, and seeds default records.
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
    is_indoor BOOLEAN DEFAULT 0,
    vehicle_access_tier TEXT DEFAULT 'ALL', -- 'ALL', 'FOOT_AND_WHEELBARROW', 'FOOT_ONLY'
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

-- Location Ground Items (Unburdened / Dropped Items - RFC 007)
CREATE TABLE IF NOT EXISTS location_items (
    location_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    dropped_by TEXT,
    dropped_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (location_id, item_id)
);

-- Location Dynamic Ecological Resource Pools (RFC 007)
CREATE TABLE IF NOT EXISTS location_resources (
    location_id TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    current_stock INTEGER NOT NULL DEFAULT 100,
    max_stock INTEGER NOT NULL DEFAULT 100,
    replenish_rate_per_day INTEGER NOT NULL DEFAULT 20,
    PRIMARY KEY (location_id, resource_id)
);

-- Workstations Table (RFC 008)
CREATE TABLE IF NOT EXISTS workstations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    location_id TEXT NOT NULL,
    equipment_present TEXT NOT NULL,
    crafting_domain TEXT NOT NULL,
    FOREIGN KEY (location_id) REFERENCES locations(id) ON DELETE CASCADE
);

-- Recipes Table (RFC 008 Multi-Stage Crafting)
CREATE TABLE IF NOT EXISTS recipes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    required_station TEXT, -- NULL if craftable anywhere
    output_item_id TEXT NOT NULL,
    output_quantity INTEGER NOT NULL DEFAULT 1,
    duration_ticks INTEGER NOT NULL DEFAULT 20,
    energy_cost INTEGER NOT NULL DEFAULT 5,
    required_skill_id TEXT,
    min_skill_level INTEGER DEFAULT 1,
    FOREIGN KEY (output_item_id) REFERENCES items(id)
);

-- Recipe Ingredients Table (RFC 008)
CREATE TABLE IF NOT EXISTS recipe_ingredients (
    recipe_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (recipe_id, item_id),
    FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE,
    FOREIGN KEY (item_id) REFERENCES items(id)
);

-- Characters Table
CREATE TABLE IF NOT EXISTS characters (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    persona_prompt TEXT NOT NULL,
    occupation TEXT NOT NULL,
    gold INTEGER NOT NULL DEFAULT 10,
    hunger INTEGER NOT NULL DEFAULT 100,
    energy INTEGER NOT NULL DEFAULT 100,
    current_location TEXT NOT NULL DEFAULT 'town_square',
    equipped_container_id TEXT,
    status TEXT NOT NULL DEFAULT 'IDLE', -- 'IDLE', 'FORAGING', 'RESTING', 'CRAFTING'
    action_end_tick INTEGER DEFAULT 0,
    action_start_tick INTEGER DEFAULT 0,
    action_target TEXT DEFAULT '',
    action_intent_id TEXT DEFAULT '',
    is_online BOOLEAN NOT NULL DEFAULT 0,
    controller_type TEXT NOT NULL DEFAULT 'agent',
    mqtt_password TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Character Skills Table (RFC 007 Skill-based progression)
CREATE TABLE IF NOT EXISTS character_skills (
    character_id TEXT NOT NULL,
    skill_id TEXT NOT NULL, -- 'foraging', 'woodcutting', 'herbalism', 'crafting', etc.
    level INTEGER NOT NULL DEFAULT 1,
    experience INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, skill_id)
);

-- Items Table (Mass, Container & Consumable Properties - RFC 007)
CREATE TABLE IF NOT EXISTS items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    weight_kg REAL NOT NULL DEFAULT 0.1,
    is_consumable BOOLEAN DEFAULT 0,
    hunger_restore INTEGER DEFAULT 0,
    is_container BOOLEAN DEFAULT 0,
    capacity_kg REAL DEFAULT 0.0,
    slot_limit INTEGER DEFAULT 0
);

-- Inventories Table
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

	// Apply migrations for any existing databases
	if err := migrateSchema(db); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	if err := SeedLocations(db); err != nil {
		return nil, fmt.Errorf("failed to seed locations: %w", err)
	}

	if err := SeedLocationResources(db); err != nil {
		return nil, fmt.Errorf("failed to seed location resources: %w", err)
	}

	if err := SeedItemsAndCharacters(db); err != nil {
		return nil, fmt.Errorf("failed to seed items and characters: %w", err)
	}

	if err := SeedCrafting(db); err != nil {
		return nil, fmt.Errorf("failed to seed crafting recipes and workstations: %w", err)
	}

	return db, nil
}

func migrateSchema(db *sql.DB) error {
	columnsToAdd := []struct {
		table  string
		column string
		def    string
	}{
		{"locations", "is_indoor", "BOOLEAN DEFAULT 0"},
		{"locations", "vehicle_access_tier", "TEXT DEFAULT 'ALL'"},
		{"characters", "energy", "INTEGER NOT NULL DEFAULT 100"},
		{"characters", "equipped_container_id", "TEXT"},
		{"characters", "status", "TEXT NOT NULL DEFAULT 'IDLE'"},
		{"characters", "action_end_tick", "INTEGER DEFAULT 0"},
		{"characters", "action_start_tick", "INTEGER DEFAULT 0"},
		{"characters", "action_target", "TEXT DEFAULT ''"},
		{"characters", "action_intent_id", "TEXT DEFAULT ''"},
		{"items", "weight_kg", "REAL NOT NULL DEFAULT 0.1"},
		{"items", "is_container", "BOOLEAN DEFAULT 0"},
		{"items", "capacity_kg", "REAL DEFAULT 0.0"},
		{"items", "slot_limit", "INTEGER DEFAULT 0"},
	}

	for _, col := range columnsToAdd {
		query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", col.table, col.column, col.def)
		_, _ = db.Exec(query) // Ignore error if column already exists
	}

	return nil
}
