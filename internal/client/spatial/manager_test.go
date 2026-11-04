package spatial

import (
	"strings"
	"testing"
	"time"
)

func TestContextManager_PromptAssembly(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{
		AgentID:          "eldon_millwright",
		AgentName:        "Eldon the Millwright",
		InitialLocation:  "town_mill",
		LocationName:     "Town Mill",
		MaxSensoryLines:  12,
		DefaultGold:      15,
		DefaultEnergy:    20, // Low energy to test status formatting
		DefaultInventory: []string{"4x Wheat", "2x Flour"},
	})

	// Ingest time tick
	cm.IngestTimeTick(TimeTickMessage{
		Tick:       105,
		Timestamp:  time.Now(),
		InGameTime: "10:05",
		IsNight:    false,
		Weather:    "Sunny",
	})

	// Ingest room chat
	cm.RegisterCharacter("luke_farmer", "Farmer Luke")
	cm.IngestChat("luke_farmer", "Eldon, I have 10 sacks of wheat. What's your rate today?")

	// Construct client-side subjective reflections (RFC 013 Client Memory Engine)
	peerMemories := []PeerMemoryContext{
		{
			PeerID:   "martha_barkeep",
			PeerName: "Martha the Barkeep",
			Reflections: []string{
				"Martha regularly serves warm mutton and lent you 2 gold last week.",
			},
		},
	}

	prompt := cm.BuildPrompt(peerMemories, nil)

	// Verify profile section
	if !strings.Contains(prompt, "=== YOUR PROFILE ===") {
		t.Error("prompt missing YOUR PROFILE section")
	}
	if !strings.Contains(prompt, "Name: Eldon the Millwright") {
		t.Error("prompt missing agent name")
	}
	if !strings.Contains(prompt, "Gold: 15 | Inventory: 4x Wheat, 2x Flour") {
		t.Error("prompt missing gold and inventory")
	}

	// Verify location rules section
	if !strings.Contains(prompt, "=== LOCATION RULES (Town Mill) ===") {
		t.Error("prompt missing LOCATION RULES section")
	}
	if !strings.Contains(prompt, "You must charge a fair fee") {
		t.Error("prompt missing town mill rules")
	}

	// Verify psychological safety section (RFC 017)
	if !strings.Contains(prompt, "=== COGNITIVE ENVIRONMENT & PSYCHOLOGICAL SAFETY ===") {
		t.Error("prompt missing COGNITIVE ENVIRONMENT & PSYCHOLOGICAL SAFETY section")
	}
	if !strings.Contains(prompt, "This is your private internal mind. You are not being tested, judged, or scored.") {
		t.Error("prompt missing psychological safety description")
	}

	// Verify private client reflections section (RFC 013)
	if !strings.Contains(prompt, "=== YOUR PRIVATE MEMORIES OF PEOPLE PRESENT ===") {
		t.Error("prompt missing YOUR PRIVATE MEMORIES section")
	}
	if !strings.Contains(prompt, "Regarding Martha the Barkeep (martha_barkeep):") {
		t.Error("prompt missing reflection header for Martha")
	}
	if !strings.Contains(prompt, "Martha regularly serves warm mutton and lent you 2 gold last week.") {
		t.Error("prompt missing Martha's episodic memory string")
	}

	// Verify sensory logs section
	if !strings.Contains(prompt, "=== RECENT LOGS ===") {
		t.Error("prompt missing RECENT LOGS section")
	}
	if !strings.Contains(prompt, "Farmer Luke says: \"Eldon, I have 10 sacks of wheat. What's your rate today?\"") {
		t.Error("prompt missing Luke's chat log")
	}

	// Verify instruction section
	if !strings.Contains(prompt, "=== INSTRUCTION ===") {
		t.Error("prompt missing INSTRUCTION section")
	}
}

func TestContextManager_DynamicLocationChange(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{
		AgentID:         "eldon_millwright",
		InitialLocation: "town_mill",
		LocationName:    "Town Mill",
	})

	var oldLocObserved, newLocObserved string
	callbackCalled := false

	cm.SetOnLocationChange(func(oldLoc, newLoc string) {
		oldLocObserved = oldLoc
		newLocObserved = newLoc
		callbackCalled = true
	})

	directPayload := []byte(`{"intent_id":"req_123","status":"SUCCESS","message":"Moved to Tavern"}`)
	if err := cm.IngestDirectResponse(directPayload); err != nil {
		t.Fatalf("unexpected error ingesting direct response: %v", err)
	}

	if !callbackCalled {
		t.Fatal("expected location change callback to be triggered, but wasn't")
	}
	if oldLocObserved != "town_mill" {
		t.Errorf("expected oldLoc town_mill, got %s", oldLocObserved)
	}
	if newLocObserved != "tavern" {
		t.Errorf("expected newLoc tavern, got %s", newLocObserved)
	}
	if cm.CurrentLocation() != "tavern" {
		t.Errorf("expected current location tavern, got %s", cm.CurrentLocation())
	}
	if cm.LocationName() != "Tavern" {
		t.Errorf("expected location name Tavern, got %s", cm.LocationName())
	}
}

func TestContextManager_StateSync(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{
		AgentID:         "eldon_millwright",
		InitialLocation: "town_square",
	})

	syncPayload := []byte(`{
		"intent_id": "req_look_1",
		"status": "SUCCESS",
		"type": "STATE_SYNC",
		"message": "You observe The Town Mill",
		"character": {
			"id": "eldon_millwright",
			"name": "Eldon the Millwright",
			"gold": 50,
			"energy": 90,
			"status": "IDLE",
			"current_location": "town_mill",
			"inventory": [
				{"item_id": "flour", "name": "Bag of Flour", "quantity": 3}
			]
		},
		"location": {
			"id": "town_mill",
			"name": "The Town Mill",
			"description": "The rustic timber mill.",
			"characters_present": [
				{"id": "martha_barkeep", "name": "Martha the Barkeep"}
			],
			"ground_items": [
				{"item_id": "wood_log", "name": "Wood Log", "quantity": 2}
			]
		}
	}`)

	if err := cm.IngestDirectResponse(syncPayload); err != nil {
		t.Fatalf("unexpected error ingesting state sync: %v", err)
	}

	if cm.CurrentLocation() != "town_mill" {
		t.Errorf("expected town_mill, got %s", cm.CurrentLocation())
	}
	if cm.LocationName() != "The Town Mill" {
		t.Errorf("expected The Town Mill, got %s", cm.LocationName())
	}

	prompt := cm.BuildPrompt(nil, nil)
	if !strings.Contains(prompt, "Gold: 50 | Inventory: 3x Bag of Flour") {
		t.Errorf("expected updated gold and inventory in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Martha the Barkeep") {
		t.Errorf("expected Martha in sensory logs in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "2x Wood Log") {
		t.Errorf("expected Wood Log in sensory logs in prompt, got:\n%s", prompt)
	}
}
