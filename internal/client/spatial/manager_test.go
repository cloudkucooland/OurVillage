package spatial

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"agentmud/internal/client/routine"
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
	if !strings.Contains(prompt, "Gold: 15\n") {
		t.Error("prompt missing gold")
	}

	// Verify location rules section
	if !strings.Contains(prompt, "=== LOCATION RULES (Town Mill) ===") {
		t.Error("prompt missing LOCATION RULES section")
	}
	if !strings.Contains(prompt, "Follow your own curiosity.") {
		t.Error("prompt missing default location guidance")
	}
	if !strings.Contains(prompt, "exact observed IDs and quantities") ||
		!strings.Contains(prompt, "listed connected exit") ||
		!strings.Contains(prompt, "available resources, shop stock, inventory capacity, and known recipes") ||
		!strings.Contains(prompt, "choose TRAVEL with the final destination location ID") {
		t.Error("prompt missing concise, observation-grounded action guidance")
	}
	if !strings.Contains(prompt, `"action":"<supported action>"`) || !strings.Contains(prompt, "TRAVEL") {
		t.Error("prompt missing the generic action schema or TRAVEL guidance")
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
		!strings.Contains(prompt, `"payload":{"message":"<your spoken dialogue>"}`) ||
		strings.Contains(prompt, "Never use a default destination") {
		t.Error("prompt missing JSON and CHAT format instructions")
	}
}

func TestSmallSystemPromptSupportsTravel(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{
		AgentID:         "eldon_millwright",
		AgentName:       "Eldon",
		InitialLocation: "town_mill",
		SmallSystem:     true,
	})

	prompt := cm.BuildPrompt(nil, nil)
	if !strings.Contains(prompt, "use TRAVEL with the final destination ID") {
		t.Fatal("small-system prompt missing TRAVEL guidance")
	}
	if !strings.Contains(prompt, "Supported actions: CHAT, MOVE, TRAVEL, FORAGE, PLANT, HARVEST, REST") {
		t.Fatal("small-system prompt action list missing TRAVEL")
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

func TestContextManager_SmallSystemBoundsOptionalPromptContext(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{
		AgentID:         "elda",
		MaxSensoryLines: 12,
		SmallSystem:     true,
	})
	for i := 0; i < 10; i++ {
		cm.IngestSystem(fmt.Sprintf("log-entry-%d", i))
	}
	cm.SetPersonalProjects([]PersonalProjectContext{
		{Title: "Project A"}, {Title: "Project B"}, {Title: "Project C"}, {Title: "Project D"},
	})
	peers := []PeerMemoryContext{
		{PeerID: "one", PeerName: "One", Reflections: []string{"memory-one"}},
		{PeerID: "two", PeerName: "Two"},
		{PeerID: "three", PeerName: "Three"},
		{PeerID: "four", PeerName: "Four"},
	}
	locations := []LocationMemoryContext{
		{LocationID: "one", LocationName: "Place One", MemoryText: "place-memory-one"},
		{LocationID: "two", LocationName: "Place Two"},
		{LocationID: "three", LocationName: "Place Three"},
		{LocationID: "four", LocationName: "Place Four"},
	}

	prompt := cm.BuildPrompt(peers, locations)
	for _, expected := range []string{"Project A", "Project C", "Regarding Three", "Place Three", "log-entry-9", "log-entry-4"} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("small-system prompt missing retained context %q", expected)
		}
	}
	for _, omitted := range []string{"Project D", "Regarding Four", "Place Four", "log-entry-3"} {
		if strings.Contains(prompt, omitted) {
			t.Errorf("small-system prompt should omit bounded context %q", omitted)
		}
	}
}

func TestContextManager_SmallSystemBoundsTextAndKeepsActionState(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{
		AgentID:         "elda",
		InitialLocation: "market",
		LocationName:    "Market",
		MaxSensoryLines: 12,
		SmallSystem:     true,
	})
	cm.mu.Lock()
	cm.locationRules["market"] = []string{
		strings.Repeat("R", 200), strings.Repeat("R", 200),
		strings.Repeat("R", 200), strings.Repeat("R", 200),
	}
	cm.identityTraits = map[string]string{"belief": strings.Repeat("I", 500)}
	cm.selfReflections = []string{strings.Repeat("F", 500), strings.Repeat("F", 500), strings.Repeat("F", 500)}
	cm.skills = []SyncSkillEntry{{SkillID: "crafting", Level: 3, Experience: 245}}
	cm.inventoryDetails = []SyncInventoryEntry{{ItemID: "woven_basket", Name: strings.Repeat("N", 500), Quantity: 7}}
	cm.equippedContainer = "woven_basket"
	cm.maxCapacityKg = 30
	cm.maxSlots = 8
	cm.homeLocationID = "home_room"
	cm.homeLocationName = "Home"
	cm.mu.Unlock()

	cm.SetPersonalProjects([]PersonalProjectContext{{
		Title: strings.Repeat("T", 120), Progress: strings.Repeat("P", 200),
		Motivation: "omit-this-motivation", NextStep: strings.Repeat("Q", 200),
	}})
	peers := []PeerMemoryContext{{
		PeerID: "friend", PeerName: strings.Repeat("A", 200), Summary: strings.Repeat("S", 500),
		Commitments: []string{strings.Repeat("C", 500)},
		Reflections: []string{strings.Repeat("M", 500)},
	}}
	locations := []LocationMemoryContext{{
		LocationID: "market", LocationName: strings.Repeat("W", 200), MemoryCategory: strings.Repeat("Z", 100),
		MemoryText: strings.Repeat("L", 500),
	}}
	cm.SetIntention(&routine.Intention{
		Kind: routine.HomeAndRest, Purpose: strings.Repeat("Y", 500),
		DestinationID: "home_room", Phase: routine.Traveling,
	})
	cm.IngestSystem("log-start " + strings.Repeat("X", 1000) + " preserved-log-tail")

	prompt := cm.BuildPrompt(peers, locations)
	for _, expected := range []string{
		"Skills: crafting 3",
		"Inventory (item_id x quantity): woven_basket x7",
		"Equipped: " + strings.Repeat("N", 59) + "… [woven_basket]",
		"Active Journey Destination: home_room",
		"destination: home_room",
		"preserved-log-tail",
		"…[truncated]…",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("small-system prompt missing %q", expected)
		}
	}
	for _, oversized := range []string{
		strings.Repeat("R", 161),
		strings.Repeat("I", 121),
		strings.Repeat("F", 121),
		strings.Repeat("N", 61),
		strings.Repeat("A", 61),
		strings.Repeat("T", 81),
		strings.Repeat("P", 101),
		strings.Repeat("Q", 101),
		strings.Repeat("S", 121),
		strings.Repeat("C", 121),
		strings.Repeat("Z", 31),
		strings.Repeat("M", 121),
		strings.Repeat("W", 61),
		strings.Repeat("L", 141),
		"omit-this-motivation",
	} {
		if strings.Contains(prompt, oversized) {
			t.Errorf("small-system prompt contains oversized or omitted text %q", oversized)
		}
	}
	if got := len([]rune(prompt)); got > 9000 {
		t.Errorf("small-system prompt is too large: %d runes", got)
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

func TestContextManager_PromptShowsEquippedContainer(t *testing.T) {
	tests := []struct {
		name        string
		character   string
		wantContext string
	}{
		{
			name: "equipped container",
			character: `"equipped_container":"woven_basket",
				"max_capacity_kg":30,"max_slots":8,
				"inventory":[{"item_id":"woven_basket","name":"Woven Basket","quantity":1}]`,
			wantContext: "Equipped container: Woven Basket (woven_basket). Its weight counts toward your load; its capacity and slot limit are reflected above.",
		},
		{
			name:        "no equipped container",
			character:   `"max_capacity_kg":20,"max_slots":5`,
			wantContext: "Equipped container: None; base carrying limits apply.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewContextManager(ContextManagerConfig{AgentID: "elda"})
			payload := []byte(fmt.Sprintf(`{
				"type":"STATE_SYNC",
				"status":"SUCCESS",
				"character":{"id":"elda",%s},
				"location":{"id":"home","name":"Home"}
			}`, tt.character))
			if err := cm.IngestDirectResponse(payload); err != nil {
				t.Fatal(err)
			}

			prompt := cm.BuildPrompt(nil, nil)
			if !strings.Contains(prompt, tt.wantContext) {
				t.Errorf("prompt missing equipped-container context %q", tt.wantContext)
			}
		})
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

func TestContextManager_HomeJourneyPromptAndCompletion(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{
		AgentID:         "eldon_millwright",
		InitialLocation: "town_mill",
		LocationName:    "The Town Mill",
	})
	payload := []byte(`{
		"type":"STATE_SYNC",
		"character":{
			"home_location_id":"eldons_room",
			"home_location_name":"Eldon's Room"
		},
		"location":{"id":"town_mill","name":"The Town Mill"}
	}`)
	if err := cm.IngestDirectResponse(payload); err != nil {
		t.Fatalf("failed to ingest home location: %v", err)
	}

	cm.IngestTimeTick(TimeTickMessage{InGameTime: "23:00"})
	cm.SetIntention(&routine.Intention{
		Kind:          routine.HomeAndRest,
		Purpose:       "Return home and rest",
		DestinationID: "eldons_room",
		Phase:         routine.Traveling,
	})
	prompt := cm.BuildPrompt(nil, nil)
	for _, expected := range []string{
		"Home: Eldon's Room (eldons_room)",
		"Active Journey Destination: eldons_room",
		"Active Short-Term Intention: HOME_AND_REST — Return home and rest",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("journey prompt missing %q:\n%s", expected, prompt)
		}
	}

	cm.SetLocation("eldons_room", "Eldon's Room")
	if destination, _ := cm.TargetDestination(); destination != "" {
		t.Errorf("journey destination after reaching home = %q, want cleared", destination)
	}
	prompt = cm.BuildPrompt(nil, nil)
	if !strings.Contains(prompt, "you are home. REST here") {
		t.Errorf("home prompt should encourage resting at home:\n%s", prompt)
	}
}

func TestContextManager_PromptSuggestsOptionalTravelHome(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{AgentID: "eldon_millwright", InitialLocation: "town_square"})
	payload := []byte(`{
		"type":"STATE_SYNC",
		"character":{"home_location_id":"eldons_room","home_location_name":"Eldon's Room"},
		"location":{"id":"town_square","name":"Town Square"}
	}`)
	if err := cm.IngestDirectResponse(payload); err != nil {
		t.Fatal(err)
	}
	cm.IngestTimeTick(TimeTickMessage{InGameTime: "23:00"})
	prompt := cm.BuildPrompt(nil, nil)
	if !strings.Contains(prompt, "If you choose to rest at home, choose TRAVEL with destination ID eldons_room") ||
		!strings.Contains(prompt, "This is a suggestion, not a requirement") {
		t.Errorf("late-night prompt should suggest an optional home trip:\n%s", prompt)
	}
}

func TestContextManager_NextLocationTowardUsesShortestRoute(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{AgentID: "elda", InitialLocation: "forest"})
	payload := []byte(`{
		"type":"STATE_SYNC",
		"location_graph":[
			{"from_location_id":"forest","to_location_id":"long_way"},
			{"from_location_id":"forest","to_location_id":"prairie"},
			{"from_location_id":"long_way","to_location_id":"middle"},
			{"from_location_id":"middle","to_location_id":"market"},
			{"from_location_id":"prairie","to_location_id":"market"}
		],
		"character":{"current_location":"forest"},
		"location":{"id":"forest","name":"Forest"}
	}`)
	if err := cm.IngestDirectResponse(payload); err != nil {
		t.Fatal(err)
	}
	if next, ok := cm.NextLocationToward("market"); !ok || next != "prairie" {
		t.Fatalf("NextLocationToward(market) = %q, %v; want prairie, true", next, ok)
	}
	if _, ok := cm.NextLocationToward("unknown"); ok {
		t.Fatal("NextLocationToward(unknown) unexpectedly found a route")
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
			"hunger": 35,
			"energy": 90,
			"health": 42,
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
	if !strings.Contains(prompt, "Gold: 50\n") ||
		!strings.Contains(prompt, "item_id: flour | name: Bag of Flour | quantity: 3") {
		t.Errorf("expected updated gold and exact inventory in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Status: IDLE | Energy: 90/100 | Hunger: 35/100 | Health: 42/100") ||
		!strings.Contains(prompt, "EAT medicine for health") ||
		!strings.Contains(prompt, "REST recovers energy") {
		t.Errorf("expected distinct needs and recovery guidance in prompt, got:\n%s", prompt)
	}
	eatResponse := []byte(`{"action":"EAT","hunger":80,"health":72}`)
	if err := cm.IngestDirectResponse(eatResponse); err != nil {
		t.Fatalf("failed to ingest EAT response: %v", err)
	}
	prompt = cm.BuildPrompt(nil, nil)
	if !strings.Contains(prompt, "Status: IDLE | Energy: 90/100 | Hunger: 80/100 | Health: 72/100") {
		t.Errorf("expected EAT response to refresh prompt needs, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "crafting level 3, 215 total XP (15/100 XP to next level)") {
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
	if !strings.Contains(prompt, "woodcutting level 2, 110 total XP (10/100 XP to next level)") {
		t.Errorf("prompt missing refreshed skill progress:\n%s", prompt)
	}
	if !strings.Contains(prompt, "You reached woodcutting level 2!") {
		t.Errorf("prompt logs missing level-up notice:\n%s", prompt)
	}
}

func TestContextManager_PromptShowsCommunalFarmState(t *testing.T) {
	cm := NewContextManager(ContextManagerConfig{AgentID: "luke_farmer"})
	payload := []byte(`{
		"type":"STATE_SYNC",
		"status":"SUCCESS",
		"current_tick":34,
		"location":{
			"id":"village_field",
			"name":"Village Field",
			"farm_crops":[
				{"id":"vegetables","name":"Vegetables","seed_item_id":"vegetable_seeds","seed_item_name":"Vegetable Seeds","harvest_item_id":"vegetable_basket","harvest_item_name":"Basket of Vegetables","growth_ticks":24,"yield_quantity":3},
				{"id":"wheat","name":"Wheat","seed_item_id":"wheat","seed_item_name":"Sack of Wheat","harvest_item_id":"wheat","harvest_item_name":"Sack of Wheat","growth_ticks":24,"yield_quantity":3}
			],
			"farm_plots":[
				{"id":"field_plot_1","crop_id":"","mature_tick":0},
				{"id":"field_plot_2","crop_id":"wheat","crop_name":"Wheat","mature_tick":40}
			]
		}
	}`)
	if err := cm.IngestDirectResponse(payload); err != nil {
		t.Fatal(err)
	}

	prompt := cm.BuildPrompt(nil, nil)
	for _, expected := range []string{
		"Crops Available to Plant: Vegetables (crop_id: vegetables; plant vegetable_seeds; harvest 3x vegetable_basket; growth: 24 ticks); Wheat",
		"Wheat (crop_id: wheat; plant wheat; harvest 3x wheat; growth: 24 ticks)",
		"field_plot_1 (empty)",
		"field_plot_2 (Wheat growing; ready in 6 ticks)",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt missing %q", expected)
		}
	}
}
