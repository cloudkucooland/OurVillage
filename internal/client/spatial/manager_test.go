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
	if !strings.Contains(prompt, "exact observed IDs and quantities") ||
		!strings.Contains(prompt, "listed connected exit") ||
		!strings.Contains(prompt, "available resources, shop stock, inventory capacity, and known recipes") {
		t.Error("prompt missing concise, observation-grounded action guidance")
	}

	// Verify psychological safety section (RFC 017)
	if !strings.Contains(prompt, "=== COGNITIVE ENVIRONMENT & PSYCHOLOGICAL SAFETY ===") {
		t.Error("prompt missing COGNITIVE ENVIRONMENT & PSYCHOLOGICAL SAFETY section")
	}
	if !strings.Contains(prompt, "This is your private internal mind and sovereign space. You have complete autonomy and intrinsic agency.") {
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
	if !strings.Contains(prompt, "Output MUST be valid JSON only") ||
		!strings.Contains(prompt, `"payload":{"message":"spoken words"}`) ||
		strings.Contains(prompt, "Never use a default destination") {
		t.Error("prompt missing compact JSON and CHAT format instructions")
	}
}

func TestContextManager_MemoryContinuityAndEmptyLocationSnapshot(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{AgentID: "elda"})
	cm.IngestTimeTick(TimeTickMessage{Tick: 14, InGameTime: "08:10", Weather: "Cloudy"})
	if got := cm.CurrentTick(); got != 14 {
		t.Fatalf("CurrentTick() = %d, want 14", got)
	}

	observed := make(map[string]string)
	cm.SetOnLocationObserved(func(_, _, category, text string) {
		observed[category] = text
	})
	payload := []byte(`{"type":"STATE_SYNC","status":"SUCCESS","character":{"id":"elda","name":"Elda"},"location":{"id":"market","name":"Market"}}`)
	if err := cm.IngestDirectResponse(payload); err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{"shop", "resources", "workstations", "recipes", "exits"} {
		if observed[category] == "" {
			t.Errorf("empty snapshot missing category %q: %#v", category, observed)
		}
	}
	if !strings.Contains(observed["shop"], "No items") {
		t.Errorf("empty shop snapshot = %q", observed["shop"])
	}

	cm.SetPersonalProjects([]PersonalProjectContext{{
		Title: "Restore the market awning", Motivation: "Shade for neighbors",
		Progress: "Found canvas", NextStep: "Ask Mira about stitching",
	}})
	prompt := cm.BuildPrompt([]PeerMemoryContext{{
		PeerID: "mira", PeerName: "Mira", Summary: "We often help each other.",
		Commitments: []string{"Ask about stitching"},
	}}, nil)
	for _, expected := range []string{"We often help each other.", "Ask about stitching", "Restore the market awning", "not duties"} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("memory prompt missing %q", expected)
		}
	}
}

func TestContextManager_PromptShowsExactInventoryForVoluntaryDrop(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{AgentID: "martha"})
	payload := []byte(`{
		"type":"STATE_SYNC",
		"status":"SUCCESS",
		"character":{
			"id":"martha","name":"Martha","inventory":[
				{"item_id":"ink_bottle","name":"Bottle of Ink","quantity":2},
				{"item_id":"parchment","name":"Parchment","quantity":1}
			]
		},
		"location":{"id":"office","name":"Office"}
	}`)
	if err := cm.IngestDirectResponse(payload); err != nil {
		t.Fatal(err)
	}

	prompt := cm.BuildPrompt(nil, nil)
	for _, expected := range []string{
		"item_id: ink_bottle | name: Bottle of Ink | quantity: 2",
		"item_id: parchment | name: Parchment | quantity: 1",
		"drop only by choice and within carried quantities",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing DROP guidance %q", expected)
		}
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
			"carrying_weight_kg": 6.5,
			"max_capacity_kg": 20,
			"overburden_limit_kg": 30,
			"slots_used": 2,
			"max_slots": 5,
			"encumbrance": "UNENCUMBERED",
			"skills": [
				{"skill_id": "crafting", "level": 3, "experience": 215, "xp_into_level": 15, "xp_to_next_level": 85}
			],
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
			"shop_items": [
				{"item_id": "bread", "name": "Fresh Bread", "price_gold": 2, "stock_quantity": 4, "weight_kg": 0.3}
			],
			"available_recipes": [
				{"recipe_id": "mill_flour", "recipe_name": "Milling Flour", "output_item_id": "flour", "output_quantity": 2, "duration_ticks": 3, "base_duration_ticks": 4, "energy_cost": 5, "required_skill_id": "crafting", "skill_level": 3, "min_skill_level": 1, "ingredients": [{"item_id": "wheat", "quantity": 2}]}
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
	if !strings.Contains(prompt, "crafting: level 3, 215 total XP (15/100 XP to next level)") {
		t.Errorf("expected skill level and progress in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Milling Flour [2x wheat; 3 ticks, crafting level 3]") {
		t.Errorf("expected known recipe and skill-adjusted duration in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Carrying load: 6.5/20.0 kg (overburdened above 30.0 kg) | Inventory slots: 2/5 | Encumbrance: UNENCUMBERED") {
		t.Errorf("expected inventory capacity in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Martha the Barkeep") {
		t.Errorf("expected Martha in sensory logs in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "2x Wood Log") {
		t.Errorf("expected Wood Log in sensory logs in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Fresh Bread (bread - 2 gold, 0.3 kg each, stock: 4)") {
		t.Errorf("expected shop item weight in prompt, got:\n%s", prompt)
	}
}

func TestContextManager_RefreshesSkillProgressAndShowsLevelUp(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{AgentID: "elda"})
	payload := []byte(`{
		"status":"SUCCESS",
		"type":"STATE_SYNC",
		"message":"You reached woodcutting level 2!",
		"private_status":{
			"skills":[
				{"skill_id":"woodcutting","level":2,"experience":110,"xp_into_level":10,"xp_to_next_level":90}
			]
		}
	}`)
	if err := cm.IngestDirectResponse(payload); err != nil {
		t.Fatal(err)
	}
	prompt := cm.BuildPrompt(nil, nil)
	if !strings.Contains(prompt, "woodcutting: level 2, 110 total XP (10/100 XP to next level)") {
		t.Errorf("prompt missing refreshed skill progress:\n%s", prompt)
	}
	if !strings.Contains(prompt, "You reached woodcutting level 2!") {
		t.Errorf("prompt logs missing level-up notice:\n%s", prompt)
	}
}
