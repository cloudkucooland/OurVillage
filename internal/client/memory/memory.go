package memory

import (
	"database/sql"
	"encoding/json"
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

type RelationshipMemory struct {
	PeerID      string   `json:"peer_id"`
	Summary     string   `json:"summary"`
	Commitments []string `json:"commitments"`
	UpdatedTick uint64   `json:"updated_tick"`
}

type PersonalProject struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Motivation  string   `json:"motivation"`
	Status      string   `json:"status"`
	Progress    string   `json:"progress"`
	NextStep    string   `json:"next_step"`
	People      []string `json:"people"`
	Locations   []string `json:"locations"`
	UpdatedTick uint64   `json:"updated_tick"`
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

	CREATE TABLE IF NOT EXISTS client_relationships (
		peer_id TEXT PRIMARY KEY,
		summary TEXT NOT NULL,
		commitments_json TEXT NOT NULL DEFAULT '[]',
		updated_tick INTEGER NOT NULL DEFAULT 0,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS client_projects (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		motivation TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',
		progress TEXT NOT NULL DEFAULT '',
		next_step TEXT NOT NULL DEFAULT '',
		people_json TEXT NOT NULL DEFAULT '[]',
		locations_json TEXT NOT NULL DEFAULT '[]',
		updated_tick INTEGER NOT NULL DEFAULT 0,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_client_projects_status ON client_projects(status, updated_tick DESC);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize client memory schema: %w", err)
	}

	if err := ensureColumn(db, "client_event_history", "reflection_processed", "BOOLEAN DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate client event history: %w", err)
	}
	if err := ensureColumn(db, "client_event_history", "attempt_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate client event attempts: %w", err)
	}
	if err := ensureColumn(db, "client_event_history", "retry_after", "DATETIME"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate client event retries: %w", err)
	}
	if err := ensureColumn(db, "client_event_history", "project_processed", "BOOLEAN DEFAULT 0"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate client project events: %w", err)
	}
	if err := ensureColumn(db, "client_event_history", "project_retry_after", "DATETIME"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate client project retries: %w", err)
	}
	if _, err := db.Exec(`
		DELETE FROM client_location_memories
		WHERE id NOT IN (
			SELECT MAX(id) FROM client_location_memories GROUP BY location_id, memory_category
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_client_loc_mem_category
			ON client_location_memories(location_id, memory_category);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate unique location memories: %w", err)
	}

	return &ClientMemoryStore{db: db}, nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
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

func (m *ClientMemoryStore) AddRelationshipReflection(peerID string, tick uint64, salience int, reflection string, summary string, commitments []string, eventIDs []int64) error {
	commitmentsJSON, err := json.Marshal(commitments)
	if err != nil {
		return fmt.Errorf("encode relationship commitments: %w", err)
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO client_reflections (peer_id, created_at_tick, salience_score, reflection_text)
		VALUES (?, ?, ?, ?)
	`, peerID, tick, salience, reflection); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO client_relationships (peer_id, summary, commitments_json, updated_tick, timestamp)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(peer_id) DO UPDATE SET
			summary = excluded.summary,
			commitments_json = excluded.commitments_json,
			updated_tick = excluded.updated_tick,
			timestamp = CURRENT_TIMESTAMP
	`, peerID, summary, string(commitmentsJSON), tick); err != nil {
		return err
	}
	if len(eventIDs) > 0 {
		marks := make([]string, len(eventIDs))
		args := make([]any, len(eventIDs))
		for i, id := range eventIDs {
			marks[i] = "?"
			args[i] = id
		}
		if _, err := tx.Exec(`
			UPDATE client_event_history SET reflection_processed = 1, retry_after = NULL
			WHERE id IN (`+strings.Join(marks, ",")+`)
		`, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
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
		SELECT id, tick, peer_id, event_type, raw_payload, attempt_count
		FROM client_event_history
		WHERE reflection_processed = 0 AND (retry_after IS NULL OR retry_after <= CURRENT_TIMESTAMP)
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
		if err := rows.Scan(&ev.ID, &ev.Tick, &ev.PeerID, &ev.EventType, &ev.RawPayload, &ev.Attempts); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

func (m *ClientMemoryStore) MarkEventsReflected(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	marks := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		marks[i] = "?"
	}
	_, err := m.db.Exec(`
		UPDATE client_event_history SET reflection_processed = 1, retry_after = NULL
		WHERE id IN (`+strings.Join(marks, ",")+`)`, args...)
	return err
}

func (m *ClientMemoryStore) DeferEvents(ids []int64, retryAfter time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}
	args := make([]any, len(ids))
	marks := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		marks[i] = "?"
	}
	_, err := m.db.Exec(`
		UPDATE client_event_history
		SET attempt_count = attempt_count + 1,
			retry_after = datetime('now', '+' || ? || ' seconds')
		WHERE reflection_processed = 0 AND id IN (`+strings.Join(marks, ",")+`)`,
		append([]any{int(retryAfter.Seconds())}, args...)...)
	return err
}

func (m *ClientMemoryStore) GetUnprocessedProjectEvents(limit int) ([]EventRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := m.db.Query(`
		SELECT id, tick, peer_id, event_type, raw_payload, attempt_count
		FROM client_event_history
		WHERE project_processed = 0 AND event_type = 'ACTION_RESULT'
			AND (project_retry_after IS NULL OR project_retry_after <= CURRENT_TIMESTAMP)
		ORDER BY id ASC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []EventRecord
	for rows.Next() {
		var event EventRecord
		if err := rows.Scan(&event.ID, &event.Tick, &event.PeerID, &event.EventType, &event.RawPayload, &event.Attempts); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (m *ClientMemoryStore) MarkProjectsProcessed(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	marks := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		marks[i] = "?"
	}
	_, err := m.db.Exec(`
		UPDATE client_event_history SET project_processed = 1, project_retry_after = NULL
		WHERE id IN (`+strings.Join(marks, ",")+`)`, args...)
	return err
}

func (m *ClientMemoryStore) DeferProjectEvents(ids []int64, retryAfter time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}
	args := make([]any, len(ids))
	marks := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		marks[i] = "?"
	}
	_, err := m.db.Exec(`
		UPDATE client_event_history
		SET attempt_count = attempt_count + 1,
			project_retry_after = datetime('now', '+' || ? || ' seconds')
		WHERE project_processed = 0 AND id IN (`+strings.Join(marks, ",")+`)`,
		append([]any{int(retryAfter.Seconds())}, args...)...)
	return err
}

func (m *ClientMemoryStore) UpsertRelationship(peerID, summary string, commitments []string, tick uint64) error {
	commitmentsJSON, err := json.Marshal(commitments)
	if err != nil {
		return fmt.Errorf("encode relationship commitments: %w", err)
	}
	_, err = m.db.Exec(`
		INSERT INTO client_relationships (peer_id, summary, commitments_json, updated_tick, timestamp)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(peer_id) DO UPDATE SET
			summary = excluded.summary,
			commitments_json = excluded.commitments_json,
			updated_tick = excluded.updated_tick,
			timestamp = CURRENT_TIMESTAMP
	`, peerID, summary, string(commitmentsJSON), tick)
	return err
}

func (m *ClientMemoryStore) GetRelationship(peerID string) (RelationshipMemory, error) {
	var rel RelationshipMemory
	var commitmentsJSON string
	err := m.db.QueryRow(`
		SELECT peer_id, summary, commitments_json, updated_tick
		FROM client_relationships WHERE peer_id = ?
	`, peerID).Scan(&rel.PeerID, &rel.Summary, &commitmentsJSON, &rel.UpdatedTick)
	if err != nil {
		return rel, err
	}
	if err := json.Unmarshal([]byte(commitmentsJSON), &rel.Commitments); err != nil {
		return rel, fmt.Errorf("decode relationship commitments: %w", err)
	}
	return rel, nil
}

func (m *ClientMemoryStore) GetRecentRelationships(limit int) ([]RelationshipMemory, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := m.db.Query(`
		SELECT peer_id, summary, commitments_json, updated_tick
		FROM client_relationships ORDER BY updated_tick DESC, timestamp DESC, peer_id LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var relationships []RelationshipMemory
	for rows.Next() {
		var rel RelationshipMemory
		var commitmentsJSON string
		if err := rows.Scan(&rel.PeerID, &rel.Summary, &commitmentsJSON, &rel.UpdatedTick); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(commitmentsJSON), &rel.Commitments); err != nil {
			return nil, fmt.Errorf("decode relationship commitments: %w", err)
		}
		relationships = append(relationships, rel)
	}
	return relationships, rows.Err()
}

func (m *ClientMemoryStore) UpsertProject(project PersonalProject) error {
	if project.ID == "" || strings.TrimSpace(project.Title) == "" {
		return fmt.Errorf("project id and title are required")
	}
	if project.Status != "active" && project.Status != "paused" && project.Status != "completed" {
		return fmt.Errorf("invalid project status %q", project.Status)
	}
	peopleJSON, err := json.Marshal(project.People)
	if err != nil {
		return fmt.Errorf("encode project people: %w", err)
	}
	locationsJSON, err := json.Marshal(project.Locations)
	if err != nil {
		return fmt.Errorf("encode project locations: %w", err)
	}
	_, err = m.db.Exec(`
		INSERT INTO client_projects
			(id, title, motivation, status, progress, next_step, people_json, locations_json, updated_tick, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			motivation = excluded.motivation,
			status = excluded.status,
			progress = excluded.progress,
			next_step = excluded.next_step,
			people_json = excluded.people_json,
			locations_json = excluded.locations_json,
			updated_tick = excluded.updated_tick,
			timestamp = CURRENT_TIMESTAMP
	`, project.ID, project.Title, project.Motivation, project.Status, project.Progress, project.NextStep,
		string(peopleJSON), string(locationsJSON), project.UpdatedTick)
	return err
}

func (m *ClientMemoryStore) GetProjects(activeOnly bool, limit int) ([]PersonalProject, error) {
	if limit <= 0 {
		limit = 10
	}
	query := `
		SELECT id, title, motivation, status, progress, next_step, people_json, locations_json, updated_tick
		FROM client_projects
	`
	if activeOnly {
		query += " WHERE status = 'active'"
	}
	query += " ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END, updated_tick DESC LIMIT ?"
	rows, err := m.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []PersonalProject
	for rows.Next() {
		var project PersonalProject
		var peopleJSON, locationsJSON string
		if err := rows.Scan(&project.ID, &project.Title, &project.Motivation, &project.Status,
			&project.Progress, &project.NextStep, &peopleJSON, &locationsJSON, &project.UpdatedTick); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(peopleJSON), &project.People); err != nil {
			return nil, fmt.Errorf("decode project people: %w", err)
		}
		if err := json.Unmarshal([]byte(locationsJSON), &project.Locations); err != nil {
			return nil, fmt.Errorf("decode project locations: %w", err)
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
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
	return err
}

func (m *ClientMemoryStore) GetLocationMemories() ([]LocationMemory, error) {
	return m.GetLocationMemoriesForLocations(nil, 20)
}

func (m *ClientMemoryStore) GetLocationMemoriesForLocations(locationIDs []string, limit int) ([]LocationMemory, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `
		SELECT id, location_id, location_name, memory_category, memory_text, updated_at_tick, timestamp
		FROM client_location_memories
	`
	var args []any
	if len(locationIDs) > 0 {
		marks := make([]string, len(locationIDs))
		for i, id := range locationIDs {
			marks[i] = "?"
			args = append(args, id)
		}
		query += " WHERE location_id IN (" + strings.Join(marks, ",") + ")"
	}
	query += " ORDER BY updated_at_tick DESC, id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := m.db.Query(query, args...)
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
