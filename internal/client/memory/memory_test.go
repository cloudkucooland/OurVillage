package memory

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"agentmud/internal/client/routine"

	_ "modernc.org/sqlite"
)

func TestClientMemoryStore_InitDB(t *testing.T) {
	// 1. In-Memory SQLite Test
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize in-memory ClientMemoryStore: %v", err)
	}
	defer store.Close()

	if store.db == nil {
		t.Fatal("Expected db instance to be initialized, got nil")
	}

	// Verify schema tables exist
	var tableName string
	err = store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='client_event_history'").Scan(&tableName)
	if err != nil || tableName != "client_event_history" {
		t.Errorf("Expected table client_event_history to exist, got table: %s (err: %v)", tableName, err)
	}

	err = store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='client_reflections'").Scan(&tableName)
	if err != nil || tableName != "client_reflections" {
		t.Errorf("Expected table client_reflections to exist, got table: %s (err: %v)", tableName, err)
	}
}

func TestClientMemoryStore_AddAndRetrieveEvents(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}
	defer store.Close()

	// Add event
	err = store.AddEvent(100, "gideon_forge", "TRADE", `{"gold": 5, "items": [{"id": "iron_ingot", "qty": 1}]}`)
	if err != nil {
		t.Fatalf("Failed to add event: %v", err)
	}

	// Verify database record
	var tick uint64
	var peerID, eventType, rawPayload string
	err = store.db.QueryRow("SELECT tick, peer_id, event_type, raw_payload FROM client_event_history WHERE peer_id = ?", "gideon_forge").Scan(&tick, &peerID, &eventType, &rawPayload)
	if err != nil {
		t.Fatalf("Failed to query inserted event: %v", err)
	}

	if tick != 100 {
		t.Errorf("Expected tick 100, got %d", tick)
	}
	if peerID != "gideon_forge" {
		t.Errorf("Expected peer_id 'gideon_forge', got %s", peerID)
	}
	if eventType != "TRADE" {
		t.Errorf("Expected event_type 'TRADE', got %s", eventType)
	}
}

func TestClientMemoryStore_PersistsActiveIntention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewClientMemoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	want := routine.Intention{
		Kind:          routine.HomeAndRest,
		Purpose:       "Return home and rest",
		DestinationID: "eldons_room",
		Phase:         routine.Traveling,
	}
	if err := store.SaveActiveIntention("eldon", want); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewClientMemoryStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetActiveIntention("eldon")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != want {
		t.Fatalf("GetActiveIntention() = %#v, want %#v", got, want)
	}
	other := routine.Intention{Kind: routine.Visit, DestinationID: "tavern", Phase: routine.Traveling}
	if err := store.SaveActiveIntention("martha", other); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearActiveIntention("eldon"); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetActiveIntention("eldon")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("GetActiveIntention() after clear = %#v, want nil", got)
	}
	if got, err := store.GetActiveIntention("martha"); err != nil || got == nil || *got != other {
		t.Fatalf("other agent's intention = %#v, %v; want %#v, nil", got, err, other)
	}
}

func TestClientMemoryStore_AddAndRetrieveReflections(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}
	defer store.Close()

	// Insert reflections for Martha
	err = store.AddReflection("martha_barkeep", 50, 7, "Martha served warm mutton stew when energy was critical.")
	if err != nil {
		t.Fatalf("Failed to add first reflection: %v", err)
	}

	err = store.AddReflection("martha_barkeep", 120, 8, "Lent 2 gold to Martha for town tavern supplies.")
	if err != nil {
		t.Fatalf("Failed to add second reflection: %v", err)
	}

	// Insert reflection for a different peer
	_ = store.AddReflection("gideon_forge", 80, 4, "Gideon charges full price for hammer repairs.")

	// Query reflections for Martha (limit 5)
	reflections, err := store.GetReflectionsForPeer("martha_barkeep", 5)
	if err != nil {
		t.Fatalf("Failed to get reflections: %v", err)
	}

	if len(reflections) != 2 {
		t.Fatalf("Expected 2 reflections for Martha, got %d", len(reflections))
	}

	// Verify reverse-chronological order (newest ID first)
	if reflections[0].CreatedAtTick != 120 {
		t.Errorf("Expected most recent reflection created_at_tick to be 120, got %d", reflections[0].CreatedAtTick)
	}
	if reflections[0].SalienceScore != 8 {
		t.Errorf("Expected salience score 8, got %d", reflections[0].SalienceScore)
	}
	if reflections[0].ReflectionText != "Lent 2 gold to Martha for town tavern supplies." {
		t.Errorf("Unexpected reflection text: %s", reflections[0].ReflectionText)
	}

	// Query limit restriction
	reflectionsLimited, err := store.GetReflectionsForPeer("martha_barkeep", 1)
	if err != nil {
		t.Fatalf("Failed to query limited reflections: %v", err)
	}
	if len(reflectionsLimited) != 1 {
		t.Errorf("Expected 1 reflection with limit=1, got %d", len(reflectionsLimited))
	}
}

func TestClientMemoryStore_PathExpansion(t *testing.T) {
	tempDir := t.TempDir()
	dbFile := filepath.Join(tempDir, "sub_dir", "test_memory.db")

	store, err := NewClientMemoryStore(dbFile)
	if err != nil {
		t.Fatalf("Failed to initialize file-backed store with nested directory creation: %v", err)
	}
	defer store.Close()

	_ = store.AddReflection("eldon_millwright", 1, 5, "Self-reflection test.")
	refs, err := store.GetReflectionsForPeer("eldon_millwright", 5)
	if err != nil || len(refs) != 1 {
		t.Errorf("Expected file-backed store to save and return 1 reflection, got len %d (err: %v)", len(refs), err)
	}
}

func TestClientMemoryStore_IdentityAndSelfReflections(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to init store: %v", err)
	}

	defer store.Close()

	// Test identity traits upsert & retrieve
	err = store.UpsertIdentityTrait("craft_philosophy", "Believes in fair pricing and durable work.", 100, 8)
	if err != nil {
		t.Fatalf("Failed to upsert identity trait: %v", err)
	}

	// Upsert update
	err = store.UpsertIdentityTrait("craft_philosophy", "Believes in community-first milling and fair pricing.", 150, 9)
	if err != nil {
		t.Fatalf("Failed to update identity trait: %v", err)
	}

	traits, err := store.GetIdentityTraits()
	if err != nil {
		t.Fatalf("Failed to get identity traits: %v", err)
	}
	if len(traits) != 1 {
		t.Fatalf("Expected 1 identity trait, got %d", len(traits))
	}
	if traits[0].Value != "Believes in community-first milling and fair pricing." {
		t.Errorf("Unexpected trait value: %s", traits[0].Value)
	}
	if traits[0].SalienceScore != 9 {
		t.Errorf("Expected salience 9, got %d", traits[0].SalienceScore)
	}

	// Test self-reflections
	err = store.AddSelfReflection(200, "Today I ground extra wheat for the village bakery.")
	if err != nil {
		t.Fatalf("Failed to add self reflection: %v", err)
	}
	err = store.AddSelfReflection(210, "Feeling peaceful by the river mill.")
	if err != nil {
		t.Fatalf("Failed to add second self reflection: %v", err)
	}

	selfRefs, err := store.GetRecentSelfReflections(5)
	if err != nil {
		t.Fatalf("Failed to get self reflections: %v", err)
	}
	if len(selfRefs) != 2 {
		t.Fatalf("Expected 2 self reflections, got %d", len(selfRefs))
	}
	if selfRefs[0].Tick != 210 {
		t.Errorf("Expected most recent tick 210, got %d", selfRefs[0].Tick)
	}
}

func TestClientMemoryStore_EventRetryAndExactCompletion(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 1; i <= 3; i++ {
		if err := store.AddEvent(uint64(i), "peer", "CHAT", "event"); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.GetUnreflectedEvents(10)
	if err != nil || len(events) != 3 {
		t.Fatalf("initial events = %d, err = %v; want 3", len(events), err)
	}
	if err := store.DeferEvents([]int64{events[0].ID}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkEventsReflected([]int64{events[1].ID}); err != nil {
		t.Fatal(err)
	}
	events, err = store.GetUnreflectedEvents(10)
	if err != nil || len(events) != 1 || events[0].Tick != 3 {
		t.Fatalf("eligible events = %#v, err = %v; want only third event", events, err)
	}
}

func TestClientMemoryStore_ProjectAndRelationshipPersistence(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	project := PersonalProject{
		ID: "community-mill", Title: "Repair the mill", Motivation: "Keep flour available",
		Status: "active", Progress: "Found timber", NextStep: "Ask Mira about planks",
		People: []string{"mira"}, Locations: []string{"town_mill"}, UpdatedTick: 42,
	}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	projects, err := store.GetProjects(true, 10)
	if err != nil || len(projects) != 1 || projects[0].NextStep != project.NextStep ||
		projects[0].Locations[0] != "town_mill" {
		t.Fatalf("projects = %#v, err = %v", projects, err)
	}

	if err := store.AddEvent(43, "mira", "CHAT", "Mira offered advice."); err != nil {
		t.Fatal(err)
	}
	events, err := store.GetUnreflectedEvents(5)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddRelationshipReflection("mira", 43, 7, "Mira offered advice.", "We share an interest in restoring the mill.", []string{"Ask Mira about planks"}, []int64{events[0].ID}); err != nil {
		t.Fatal(err)
	}
	relationship, err := store.GetRelationship("mira")
	if err != nil || relationship.Summary == "" || len(relationship.Commitments) != 1 {
		t.Fatalf("relationship = %#v, err = %v", relationship, err)
	}
	reflections, err := store.GetReflectionsForPeer("mira", 5)
	if err != nil || len(reflections) != 1 {
		t.Fatalf("reflections = %#v, err = %v", reflections, err)
	}
}

func TestClientMemoryStore_LocationSnapshotsReplaceAndFilter(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertLocationMemory("market", "Market", "shop", "Sells bread.", 1); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertLocationMemory("mill", "Mill", "resources", "Wheat.", 2); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertLocationMemory("market", "Market", "shop", "No items currently listed.", 3); err != nil {
		t.Fatal(err)
	}
	memories, err := store.GetLocationMemoriesForLocations([]string{"market"}, 20)
	if err != nil || len(memories) != 1 || memories[0].MemoryText != "No items currently listed." {
		t.Fatalf("market memories = %#v, err = %v", memories, err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM client_location_memories WHERE location_id = 'market' AND memory_category = 'shop'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("snapshot count = %d, err = %v; want 1", count, err)
	}
}

func TestClientMemoryStore_LegacyEventSchemaMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE client_event_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tick INTEGER NOT NULL,
			peer_id TEXT NOT NULL, event_type TEXT NOT NULL, raw_payload TEXT NOT NULL,
			reflection_processed BOOLEAN DEFAULT 0, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewClientMemoryStore(path)
	if err != nil {
		t.Fatalf("opening legacy schema: %v", err)
	}
	defer store.Close()
	if err := store.AddEvent(9, "peer", "CHAT", "legacy-compatible"); err != nil {
		t.Fatal(err)
	}
	events, err := store.GetUnreflectedEvents(10)
	if err != nil || len(events) != 1 || events[0].Tick != 9 {
		t.Fatalf("migrated events = %#v, err = %v", events, err)
	}
}
