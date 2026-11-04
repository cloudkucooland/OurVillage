package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Reflection struct {
	ID             int64     `json:"id"`
	PeerID         string    `json:"peer_id"`
	CreatedAtTick  uint64    `json:"created_at_tick"`
	SalienceScore  int       `json:"salience_score"` // 1 (minor) to 10 (critical memory)
	ReflectionText string    `json:"reflection_text"`
	Timestamp      time.Time `json:"timestamp"`
}

type IdentityTrait struct {
	Key           string    `json:"key"`
	Value         string    `json:"value"`
	UpdatedAtTick uint64    `json:"updated_at_tick"`
	SalienceScore int       `json:"salience_score"`
	Timestamp     time.Time `json:"timestamp"`
}

type SelfReflection struct {
	ID             int64     `json:"id"`
	Tick           uint64    `json:"tick"`
	ReflectionText string    `json:"reflection_text"`
	Timestamp      time.Time `json:"timestamp"`
}

type LocationMemory struct {
	ID             int64     `json:"id"`
	LocationID     string    `json:"location_id"`
	LocationName   string    `json:"location_name"`
	MemoryCategory string    `json:"memory_category"`
	MemoryText     string    `json:"memory_text"`
	UpdatedAtTick  uint64    `json:"updated_at_tick"`
	Timestamp      time.Time `json:"timestamp"`
}

type ClientMemoryStore struct {
	db *sql.DB
}

func NewClientMemoryStore(dbPath string) (*ClientMemoryStore, error) {
	if strings.HasPrefix(dbPath, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			dbPath = filepath.Join(home, dbPath[2:])
		}
	}

	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	// Busy timeout parameter ensures SQLite waits up to 5s during lock contention
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open local client memory database: %w", err)
	}

	// Enable Write-Ahead Logging (WAL) for higher concurrency read/write access
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS client_event_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tick INTEGER NOT NULL,
		peer_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		raw_payload TEXT NOT NULL,
		reflection_processed BOOLEAN DEFAULT 0,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS client_reflections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		peer_id TEXT NOT NULL,
		created_at_tick INTEGER NOT NULL,
		salience_score INTEGER DEFAULT 5,
		reflection_text TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_client_reflections_peer ON client_reflections(peer_id);

	CREATE TABLE IF NOT EXISTS client_identity (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at_tick INTEGER NOT NULL,
		salience_score INTEGER DEFAULT 5,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS client_self_reflections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tick INTEGER NOT NULL,
		reflection_text TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS client_location_memories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		location_id TEXT NOT NULL,
		location_name TEXT NOT NULL,
		memory_category TEXT NOT NULL,
		memory_text TEXT NOT NULL,
		updated_at_tick INTEGER NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_client_loc_mem ON client_location_memories(location_id);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize client memory schema: %w", err)
	}

	// Safe migration check
	_, _ = db.Exec("ALTER TABLE client_event_history ADD COLUMN reflection_processed BOOLEAN DEFAULT 0;")

	return &ClientMemoryStore{db: db}, nil
}

func (m *ClientMemoryStore) Close() error {
	return m.db.Close()
}

func (m *ClientMemoryStore) AddEvent(tick uint64, peerID, eventType, rawPayload string) error {
	_, err := m.db.Exec(`
		INSERT INTO client_event_history (tick, peer_id, event_type, raw_payload)
		VALUES (?, ?, ?, ?)
	`, tick, peerID, eventType, rawPayload)
	return err
}

func (m *ClientMemoryStore) AddReflection(peerID string, createdAtTick uint64, salienceScore int, text string) error {
	_, err := m.db.Exec(`
		INSERT INTO client_reflections (peer_id, created_at_tick, salience_score, reflection_text)
		VALUES (?, ?, ?, ?)
	`, peerID, createdAtTick, salienceScore, text)
	return err
}

func (m *ClientMemoryStore) GetReflectionsForPeer(peerID string, limit int) ([]Reflection, error) {
	if limit <= 0 {
		limit = 5
	}

	rows, err := m.db.Query(`
		SELECT id, peer_id, created_at_tick, salience_score, reflection_text, timestamp
		FROM client_reflections
		WHERE peer_id = ?
		ORDER BY id DESC
		LIMIT ?
	`, peerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reflections []Reflection
	for rows.Next() {
		var r Reflection
		if err := rows.Scan(&r.ID, &r.PeerID, &r.CreatedAtTick, &r.SalienceScore, &r.ReflectionText, &r.Timestamp); err != nil {
			return nil, err
		}
		reflections = append(reflections, r)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return reflections, nil
}

func (m *ClientMemoryStore) GetUnreflectedEvents(limit int) ([]EventRecord, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := m.db.Query(`
		SELECT id, tick, peer_id, event_type, raw_payload
		FROM client_event_history
		WHERE reflection_processed = 0
		ORDER BY id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventRecord
	for rows.Next() {
		var ev EventRecord
		if err := rows.Scan(&ev.ID, &ev.Tick, &ev.PeerID, &ev.EventType, &ev.RawPayload); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

func (m *ClientMemoryStore) MarkEventsReflectedUpTo(maxID int64) error {
	_, err := m.db.Exec(`
		UPDATE client_event_history
		SET reflection_processed = 1
		WHERE id <= ?
	`, maxID)
	return err
}

func (m *ClientMemoryStore) UpsertIdentityTrait(key, value string, tick uint64, salience int) error {
	_, err := m.db.Exec(`
		INSERT INTO client_identity (key, value, updated_at_tick, salience_score, timestamp)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at_tick = excluded.updated_at_tick,
			salience_score = excluded.salience_score,
			timestamp = CURRENT_TIMESTAMP
	`, key, value, tick, salience)
	return err
}

func (m *ClientMemoryStore) GetIdentityTraits() ([]IdentityTrait, error) {
	rows, err := m.db.Query(`
		SELECT key, value, updated_at_tick, salience_score, timestamp
		FROM client_identity
		ORDER BY salience_score DESC, updated_at_tick DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traits []IdentityTrait
	for rows.Next() {
		var t IdentityTrait
		if err := rows.Scan(&t.Key, &t.Value, &t.UpdatedAtTick, &t.SalienceScore, &t.Timestamp); err != nil {
			return nil, err
		}
		traits = append(traits, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return traits, nil
}

func (m *ClientMemoryStore) AddSelfReflection(tick uint64, text string) error {
	_, err := m.db.Exec(`
		INSERT INTO client_self_reflections (tick, reflection_text)
		VALUES (?, ?)
	`, tick, text)
	return err
}

func (m *ClientMemoryStore) GetRecentSelfReflections(limit int) ([]SelfReflection, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := m.db.Query(`
		SELECT id, tick, reflection_text, timestamp
		FROM client_self_reflections
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reflections []SelfReflection
	for rows.Next() {
		var r SelfReflection
		if err := rows.Scan(&r.ID, &r.Tick, &r.ReflectionText, &r.Timestamp); err != nil {
			return nil, err
		}
		reflections = append(reflections, r)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return reflections, nil
}

func (m *ClientMemoryStore) UpsertLocationMemory(locationID, locationName, category, memoryText string, tick uint64) error {
	_, err := m.db.Exec(`
		INSERT INTO client_location_memories (location_id, location_name, memory_category, memory_text, updated_at_tick, timestamp)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(location_id, memory_category) DO UPDATE SET
			location_name = excluded.location_name,
			memory_text = excluded.memory_text,
			updated_at_tick = excluded.updated_at_tick,
			timestamp = CURRENT_TIMESTAMP
	`, locationID, locationName, category, memoryText, tick)
	if err != nil {
		// If UNIQUE constraint on (location_id, memory_category) doesn't exist yet, try plain insert or update.
		// To be robust, let's delete existing for this category and insert, or use upsert if constraint exists.
		_, err2 := m.db.Exec(`
			DELETE FROM client_location_memories WHERE location_id = ? AND memory_category = ?
		`, locationID, category)
		if err2 != nil {
			return err
		}
		_, err = m.db.Exec(`
			INSERT INTO client_location_memories (location_id, location_name, memory_category, memory_text, updated_at_tick, timestamp)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		`, locationID, locationName, category, memoryText, tick)
	}
	return err
}

func (m *ClientMemoryStore) GetLocationMemories() ([]LocationMemory, error) {
	rows, err := m.db.Query(`
		SELECT id, location_id, location_name, memory_category, memory_text, updated_at_tick, timestamp
		FROM client_location_memories
		ORDER BY updated_at_tick DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mems []LocationMemory
	for rows.Next() {
		var lm LocationMemory
		if err := rows.Scan(&lm.ID, &lm.LocationID, &lm.LocationName, &lm.MemoryCategory, &lm.MemoryText, &lm.UpdatedAtTick, &lm.Timestamp); err != nil {
			return nil, err
		}
		mems = append(mems, lm)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return mems, nil
}
