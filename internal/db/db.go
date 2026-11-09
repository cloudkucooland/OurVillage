package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// EventLogEntry represents an objective physical occurrence recorded by the server.
type EventLogEntry struct {
	ID          int64           `json:"id"`
	Tick        uint64          `json:"tick"`
	Timestamp   time.Time       `json:"timestamp"`
	EventType   string          `json:"event_type"` // e.g. 'TRADE', 'CONTRACT_FULFILL', 'TRESPASS', 'CHAT'
	ActorID     string          `json:"actor_id"`
	TargetID    string          `json:"target_id,omitempty"`
	LocationID  string          `json:"location_id"`
	DetailsJSON json.RawMessage `json:"details_json"`
}

// InitDB initializes the SQLite database, migrates any missing columns, and optionally seeds default records.
func InitDB(dataSourceName string, seedArgs ...bool) (*sql.DB, error) {
	shouldSeed := true
	if len(seedArgs) > 0 {
		shouldSeed = seedArgs[0]
	}
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

-- Location Connections Graph Table (Spatial Graph)
CREATE TABLE IF NOT EXISTS location_connections (
    from_location_id TEXT NOT NULL,
    to_location_id TEXT NOT NULL,
    travel_ticks INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (from_location_id, to_location_id),
    FOREIGN KEY (from_location_id) REFERENCES locations(id) ON DELETE CASCADE,
    FOREIGN KEY (to_location_id) REFERENCES locations(id) ON DELETE CASCADE
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

-- Location Shop Inventory Table (Storefront Sales)
CREATE TABLE IF NOT EXISTS location_shop_items (
    location_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    price_gold INTEGER NOT NULL,
    stock_quantity INTEGER NOT NULL DEFAULT 10,
    buy_price_gold INTEGER,
    PRIMARY KEY (location_id, item_id),
    FOREIGN KEY (location_id) REFERENCES locations(id) ON DELETE CASCADE,
    FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
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
    health INTEGER NOT NULL DEFAULT 100,
    current_location TEXT NOT NULL DEFAULT 'town_square',
    home_location_id TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS character_known_recipes (
    character_id TEXT NOT NULL,
    recipe_id TEXT NOT NULL,
    PRIMARY KEY (character_id, recipe_id),
    FOREIGN KEY (character_id) REFERENCES characters(id) ON DELETE CASCADE,
    FOREIGN KEY (recipe_id) REFERENCES recipes(id) ON DELETE CASCADE
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

-- Communal farm crops and plots
CREATE TABLE IF NOT EXISTS farm_crops (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    seed_item_id TEXT NOT NULL,
    harvest_item_id TEXT NOT NULL,
    growth_ticks INTEGER NOT NULL,
    yield_quantity INTEGER NOT NULL,
    FOREIGN KEY (seed_item_id) REFERENCES items(id),
    FOREIGN KEY (harvest_item_id) REFERENCES items(id)
);

CREATE TABLE IF NOT EXISTS farm_plots (
    id TEXT PRIMARY KEY,
    location_id TEXT NOT NULL,
    crop_id TEXT,
    planted_tick INTEGER NOT NULL DEFAULT 0,
    mature_tick INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (location_id) REFERENCES locations(id) ON DELETE CASCADE,
    FOREIGN KEY (crop_id) REFERENCES farm_crops(id)
);

-- Objective Event Ledger (RFC 013: Server Audit Trail)
CREATE TABLE IF NOT EXISTS event_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tick INTEGER NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    event_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    target_id TEXT,
    location_id TEXT NOT NULL,
    details_json TEXT NOT NULL,
    FOREIGN KEY (actor_id) REFERENCES characters(id),
    FOREIGN KEY (target_id) REFERENCES characters(id),
    FOREIGN KEY (location_id) REFERENCES locations(id)
);

CREATE INDEX IF NOT EXISTS idx_event_logs_actor ON event_logs(actor_id);
CREATE INDEX IF NOT EXISTS idx_event_logs_target ON event_logs(target_id);
CREATE INDEX IF NOT EXISTS idx_event_logs_location ON event_logs(location_id);

-- Server State Table
CREATE TABLE IF NOT EXISTS server_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
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

	if shouldSeed {
		if err := SeedLocations(db); err != nil {
			return nil, fmt.Errorf("failed to seed locations: %w", err)
		}

		if err := SeedLocationResources(db); err != nil {
			return nil, fmt.Errorf("failed to seed location resources: %w", err)
		}

		if err := SeedLocationConnections(db); err != nil {
			return nil, fmt.Errorf("failed to seed location connections: %w", err)
		}

		if err := SeedItemsAndCharacters(db); err != nil {
			return nil, fmt.Errorf("failed to seed items and characters: %w", err)
		}

		if err := SeedFarming(db); err != nil {
			return nil, fmt.Errorf("failed to seed farming: %w", err)
		}

		if err := SeedCrafting(db); err != nil {
			return nil, fmt.Errorf("failed to seed crafting recipes and workstations: %w", err)
		}

		if err := InitializeAllCharacterProgression(db); err != nil {
			return nil, fmt.Errorf("initialize character progression: %w", err)
		}

		if err := SeedShopInventory(db); err != nil {
			return nil, fmt.Errorf("failed to seed shop inventory: %w", err)
		}
	} else {
		var locationCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM locations`).Scan(&locationCount); err != nil {
			return nil, fmt.Errorf("check existing world data: %w", err)
		}
		if locationCount > 0 {
			if err := SeedFarming(db); err != nil {
				return nil, fmt.Errorf("failed to migrate communal farming data: %w", err)
			}
			if err := ensureMillingData(db); err != nil {
				return nil, fmt.Errorf("failed to migrate milling data: %w", err)
			}
			if err := initializeMillingRecipeKnowledge(db); err != nil {
				return nil, fmt.Errorf("initialize milling recipe knowledge: %w", err)
			}
			if err := seedMillingShopItems(db); err != nil {
				return nil, fmt.Errorf("failed to migrate mill shop items: %w", err)
			}
		}
	}

	return db, nil
}

func migrateSchema(db *sql.DB) error {
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS server_state (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)

	columnsToAdd := []struct {
		table  string
		column string
		def    string
	}{
		{"locations", "is_indoor", "BOOLEAN DEFAULT 0"},
		{"locations", "vehicle_access_tier", "TEXT DEFAULT 'ALL'"},
		{"characters", "energy", "INTEGER NOT NULL DEFAULT 100"},
		{"characters", "health", "INTEGER NOT NULL DEFAULT 100"},
		{"characters", "home_location_id", "TEXT NOT NULL DEFAULT ''"},
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
		{"items", "health_restore", "INTEGER DEFAULT 0"},
		{"items", "is_perishable", "BOOLEAN DEFAULT 0"},
		{"items", "shelf_life_ticks", "INTEGER DEFAULT 96"},
		{"items", "spoilage_item_id", "TEXT DEFAULT 'rotting_waste'"},
		{"inventories", "freshness_percent", "INTEGER NOT NULL DEFAULT 100"},
		{"location_shop_items", "buy_price_gold", "INTEGER"},
	}

	for _, col := range columnsToAdd {
		query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", col.table, col.column, col.def)
		_, _ = db.Exec(query) // Ignore error if column already exists
	}

	return nil
}

// RecordEventLog records a verifiable physical occurrence in the objective server event ledger.
func RecordEventLog(q Queryable, tick uint64, eventType, actorID, targetID, locationID string, details any) (*EventLogEntry, error) {
	detailsBytes, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event log details: %w", err)
	}

	res, err := execQueryable(q, `
		INSERT INTO event_logs (tick, event_type, actor_id, target_id, location_id, details_json)
		VALUES (?, ?, ?, ?, ?, ?)
	`, tick, eventType, actorID, nullableString(targetID), locationID, string(detailsBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to insert event log: %w", err)
	}

	id, _ := res.LastInsertId()
	return &EventLogEntry{
		ID:          id,
		Tick:        tick,
		Timestamp:   time.Now(),
		EventType:   eventType,
		ActorID:     actorID,
		TargetID:    targetID,
		LocationID:  locationID,
		DetailsJSON: detailsBytes,
	}, nil
}

// GetRecentEventLogsForCharacter retrieves objective event logs involving a specific character.
func GetRecentEventLogsForCharacter(db *sql.DB, characterID string, limit int) ([]EventLogEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := db.Query(`
		SELECT id, tick, timestamp, event_type, actor_id, COALESCE(target_id, ''), location_id, details_json
		FROM event_logs
		WHERE actor_id = ? OR target_id = ?
		ORDER BY id DESC
		LIMIT ?
	`, characterID, characterID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query event logs: %w", err)
	}
	defer rows.Close()

	var logs []EventLogEntry
	for rows.Next() {
		var entry EventLogEntry
		var detailsStr string
		if err := rows.Scan(&entry.ID, &entry.Tick, &entry.Timestamp, &entry.EventType, &entry.ActorID, &entry.TargetID, &entry.LocationID, &detailsStr); err != nil {
			return nil, fmt.Errorf("failed to scan event log: %w", err)
		}
		entry.DetailsJSON = json.RawMessage(detailsStr)
		logs = append(logs, entry)
	}

	return logs, nil
}

// Helper to handle optional target_id string
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Helper to execute query on Queryable interface (sql.DB or sql.Tx)
func execQueryable(q Queryable, query string, args ...any) (sql.Result, error) {
	if db, ok := q.(*sql.DB); ok {
		return db.Exec(query, args...)
	}
	if tx, ok := q.(*sql.Tx); ok {
		return tx.Exec(query, args...)
	}
	return nil, fmt.Errorf("unsupported Queryable type")
}

// SeedDB seeds all default records (locations, resources, connections, items, characters, farming, crafting, shop inventory).
func SeedDB(db *sql.DB) error {
	if err := SeedLocations(db); err != nil {
		return fmt.Errorf("failed to seed locations: %w", err)
	}
	if err := SeedLocationResources(db); err != nil {
		return fmt.Errorf("failed to seed location resources: %w", err)
	}
	if err := SeedLocationConnections(db); err != nil {
		return fmt.Errorf("failed to seed location connections: %w", err)
	}
	if err := SeedItemsAndCharacters(db); err != nil {
		return fmt.Errorf("failed to seed items and characters: %w", err)
	}
	if err := SeedFarming(db); err != nil {
		return fmt.Errorf("failed to seed farming: %w", err)
	}
	if err := SeedCrafting(db); err != nil {
		return fmt.Errorf("failed to seed crafting recipes and workstations: %w", err)
	}
	if err := SeedShopInventory(db); err != nil {
		return fmt.Errorf("failed to seed shop inventory: %w", err)
	}
	return nil
}

// GetServerTick retrieves the saved server tick from the database, returning 0 if not found.
func GetServerTick(db *sql.DB) (uint64, error) {
	var val string
	err := db.QueryRow("SELECT value FROM server_state WHERE key = 'current_tick'").Scan(&val)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var tick uint64
	_, err = fmt.Sscanf(val, "%d", &tick)
	return tick, err
}

// SaveServerTick stores the current server tick in the database.
func SaveServerTick(db *sql.DB, tick uint64) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO server_state (key, value) VALUES ('current_tick', ?)
	`, fmt.Sprintf("%d", tick))
	return err
}
