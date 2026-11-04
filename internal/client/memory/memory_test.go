package memory

import (
	"path/filepath"
	"testing"
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
