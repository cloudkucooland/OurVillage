package spatial

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type PeerMemoryContext struct {
	PeerID      string   `json:"peer_id"`
	PeerName    string   `json:"peer_name"`
	Summary     string   `json:"summary,omitempty"`
	Commitments []string `json:"commitments,omitempty"`
	Reflections []string `json:"reflections"`
}

type PersonalProjectContext struct {
	Title      string `json:"title"`
	Motivation string `json:"motivation"`
	Progress   string `json:"progress"`
	NextStep   string `json:"next_step"`
}

type LocationMemoryContext struct {
	LocationID     string `json:"location_id"`
	LocationName   string `json:"location_name"`
	MemoryCategory string `json:"memory_category"`
	MemoryText     string `json:"memory_text"`
}

func FormatSentimentLabel(sentiment int) string {
	switch {
	case sentiment >= 60:
		return "Devoted / Warm Ally"
	case sentiment >= 20:
		return "Friendly"
	case sentiment >= -20:
		return "Neutral"
	case sentiment >= -60:
		return "Distrustful"
	default:
		return "Hostile / Bitter"
	}
}

type ContextManagerConfig struct {
	AgentID          string
	AgentName        string
	InitialLocation  string
	LocationName     string
	MaxSensoryLines  int
	DefaultGold      int
	DefaultEnergy    int
	DefaultInventory []string
}

type DirectMessage struct {
	IntentID  string                 `json:"intent_id,omitempty"`
	Status    string                 `json:"status"`
	Type      string                 `json:"type,omitempty"`
	ErrorCode string                 `json:"error_code,omitempty"`
	Message   string                 `json:"message"`
	Character *SyncCharacterOverview `json:"private_status,omitempty"`
	Location  *SyncLocationOverview  `json:"location,omitempty"`
}

func (d *DirectMessage) UnmarshalJSON(data []byte) error {
	type Alias DirectMessage
	aux := &struct {
		CharacterAlt *SyncCharacterOverview `json:"character,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(d),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if d.Character == nil && aux.CharacterAlt != nil {
		d.Character = aux.CharacterAlt
	}
	return nil
}

type SyncCharacterOverview struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Gold            int                  `json:"gold"`
	Hunger          int                  `json:"hunger"`
	Energy          int                  `json:"energy"`
	Status          string               `json:"status"`
	CurrentLocation string               `json:"current_location"`
	Inventory       []SyncInventoryEntry `json:"inventory"`
}

type SyncInventoryEntry struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type SyncLocationOverview struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	Description        string                  `json:"description"`
	RulesPrompt        string                  `json:"rules_prompt,omitempty"`
	CharactersPresent  []SyncCharacterPresence `json:"characters_present"`
	GroundItems        []SyncGroundItem        `json:"ground_items"`
	Workstations       []SyncWorkstation       `json:"workstations"`
	ResourcePools      []SyncResourcePool      `json:"resource_pools"`
	ConnectedLocations []SyncConnectedLocation `json:"connected_locations"`
	ShopItems          []SyncShopItem          `json:"shop_items"`
	AvailableRecipes   []SyncAvailableRecipe   `json:"available_recipes"`
}

type SyncShopItem struct {
	ItemID    string `json:"item_id"`
	Name      string `json:"name"`
	PriceGold int    `json:"price_gold"`
	StockQty  int    `json:"stock_quantity"`
}

type SyncAvailableRecipe struct {
	RecipeID       string                 `json:"recipe_id"`
	RecipeName     string                 `json:"recipe_name"`
	OutputItemID   string                 `json:"output_item_id"`
	OutputQuantity int                    `json:"output_quantity"`
	DurationTicks  int                    `json:"duration_ticks"`
	EnergyCost     int                    `json:"energy_cost"`
	Ingredients    []SyncRecipeIngredient `json:"ingredients"`
}

type SyncRecipeIngredient struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type SyncConnectedLocation struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	TravelTicks  int    `json:"travel_ticks"`
}

type SyncCharacterPresence struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SyncGroundItem struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type SyncWorkstation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type SyncResourcePool struct {
	ResourceID   string `json:"resource_id"`
	CurrentStock int    `json:"current_stock"`
}

type TimeTickMessage struct {
	Tick       uint64    `json:"tick"`
	Timestamp  time.Time `json:"timestamp"`
	InGameTime string    `json:"in_game_time"`
	IsNight    bool      `json:"is_night"`
	Weather    string    `json:"weather"`
}

type ContextManager struct {
	mu                 sync.RWMutex
	agentID            string
	agentName          string
	currentLocation    string
	locationName       string
	locationSynced     bool
	targetDestination  string   // persistent multi-step goal destination ID
	travelPlan         []string // sequence of location IDs for journey
	locationRules      map[string][]string
	gold               int
	energy             int
	inventory          []string
	inventoryDetails   []SyncInventoryEntry
	status             string
	lastInGameTime     string
	lastWeather        string
	currentTick        uint64
	relationships      map[string]*PeerMemoryContext
	knownCharacters    map[string]string // id -> display name
	identityTraits     map[string]string
	selfReflections    []string
	personalProjects   []PersonalProjectContext
	buffer             *SensoryRingBuffer
	onLocationChange   func(oldLoc, newLoc string)
	onLocationObserved func(locID, locName, category, text string)
}

func (cm *ContextManager) SetTargetDestination(destID string, plan []string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.targetDestination = destID
	cm.travelPlan = plan
}

func (cm *ContextManager) ClearTargetDestination() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.targetDestination = ""
	cm.travelPlan = nil
}

func (cm *ContextManager) TargetDestination() (string, []string) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.targetDestination, cm.travelPlan
}

func (cm *ContextManager) SetOnLocationObserved(fn func(locID, locName, category, text string)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onLocationObserved = fn
}

func NewContextManager(cfg ContextManagerConfig) *ContextManager {
	if cfg.InitialLocation == "" {
		cfg.InitialLocation = "town_square"
	}
	if cfg.LocationName == "" {
		words := strings.Split(cfg.InitialLocation, "_")
		for i, w := range words {
			if len(w) > 0 {
				words[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
		cfg.LocationName = strings.Join(words, " ")
	}
	if cfg.AgentName == "" {
		cfg.AgentName = strings.ReplaceAll(cfg.AgentID, "_", " ")
	}

	cm := &ContextManager{
		agentID:         cfg.AgentID,
		agentName:       cfg.AgentName,
		currentLocation: cfg.InitialLocation,
		locationName:    cfg.LocationName,
		gold:            cfg.DefaultGold,
		energy:          cfg.DefaultEnergy,
		inventory:       cfg.DefaultInventory,
		status:          "IDLE",
		locationRules:   make(map[string][]string),
		relationships:   make(map[string]*PeerMemoryContext),
		knownCharacters: make(map[string]string),
		identityTraits:  make(map[string]string),
		buffer:          NewSensoryRingBuffer(cfg.MaxSensoryLines),
	}

	if cm.energy == 0 {
		cm.energy = 100
	}

	cm.locationRules["town_square"] = []string{
		"Public gathering area.",
	}
	cm.locationRules["town_mill"] = []string{
		"You must charge a fair fee (1-2 gold) for grinding wheat.",
		"Only trade-related talk during work hours.",
	}
	cm.locationRules["tavern"] = []string{
		"No unprovoked violence; brawls result in sheriff fines.",
		"Trade, gossip, and drinks are encouraged.",
	}

	return cm
}

func (cm *ContextManager) SetOnLocationChange(fn func(oldLoc, newLoc string)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onLocationChange = fn
}

func (cm *ContextManager) CurrentLocation() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.currentLocation
}

func (cm *ContextManager) LocationName() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.locationName
}

func (cm *ContextManager) IsLocationSynced() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.locationSynced
}

func (cm *ContextManager) SetLocation(newLoc, newLocName string) {
	cm.mu.Lock()
	oldLoc := cm.currentLocation
	if oldLoc == newLoc {
		cm.mu.Unlock()
		return
	}

	cm.currentLocation = newLoc
	if newLocName != "" {
		cm.locationName = newLocName
	} else {
		cm.locationName = strings.ReplaceAll(newLoc, "_", " ")
	}
	// Reset known room population when moving locations
	cm.knownCharacters = make(map[string]string)
	cb := cm.onLocationChange
	cm.mu.Unlock()

	if cb != nil {
		cb(oldLoc, newLoc)
	}
}

func (cm *ContextManager) RegisterCharacter(id, name string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.knownCharacters[id] = name
}

func (cm *ContextManager) SetRelationship(rel PeerMemoryContext) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.relationships[rel.PeerID] = &rel
}

func (cm *ContextManager) AddMemory(targetID, memory string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	rel, exists := cm.relationships[targetID]
	if !exists {
		name := cm.knownCharacters[targetID]
		if name == "" {
			name = targetID
		}
		rel = &PeerMemoryContext{
			PeerID:      targetID,
			PeerName:    name,
			Reflections: make([]string, 0),
		}
		cm.relationships[targetID] = rel
	}

	rel.Reflections = append(rel.Reflections, memory)
	if len(rel.Reflections) > 5 {
		rel.Reflections = rel.Reflections[len(rel.Reflections)-5:]
	}
}

func (cm *ContextManager) SetIdentityTraits(traits map[string]string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.identityTraits = traits
}

func (cm *ContextManager) SetSelfReflections(refs []string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.selfReflections = refs
}

func (cm *ContextManager) SetPersonalProjects(projects []PersonalProjectContext) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.personalProjects = append([]PersonalProjectContext(nil), projects...)
}

func (cm *ContextManager) IngestTimeTick(tick TimeTickMessage) {
	cm.mu.Lock()
	cm.lastInGameTime = tick.InGameTime
	cm.lastWeather = tick.Weather
	cm.currentTick = tick.Tick
	timeStr := cm.lastInGameTime
	cm.mu.Unlock()

	text := fmt.Sprintf("SYSTEM: Server tick. Time is %s. Weather is %s.", tick.InGameTime, tick.Weather)
	cm.buffer.AddLog("TIME", timeStr, text)
}

func (cm *ContextManager) IngestChat(senderID, message string) {
	cm.mu.RLock()
	displayName := cm.knownCharacters[senderID]
	if displayName == "" {
		displayName = senderID
	}
	timeStr := cm.lastInGameTime
	cm.mu.RUnlock()

	text := fmt.Sprintf("%s says: %q", displayName, message)
	cm.buffer.AddLog("CHAT", timeStr, text)
}

func (cm *ContextManager) IngestWhisper(senderID, message string) {
	cm.mu.RLock()
	displayName := cm.knownCharacters[senderID]
	if displayName == "" {
		displayName = senderID
	}
	timeStr := cm.lastInGameTime
	cm.mu.RUnlock()

	text := fmt.Sprintf("(Whisper) %s: %q", displayName, message)
	cm.buffer.AddLog("WHISPER", timeStr, text)
}

func (cm *ContextManager) IngestSystem(message string) {
	cm.mu.RLock()
	timeStr := cm.lastInGameTime
	cm.mu.RUnlock()

	text := fmt.Sprintf("SYSTEM: %s", message)
	cm.buffer.AddLog("SYSTEM", timeStr, text)
}

func (cm *ContextManager) IngestDirectResponse(raw []byte) error {
	var msg DirectMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("failed to parse direct message: %w", err)
	}

	cm.mu.RLock()
	timeStr := cm.lastInGameTime
	cm.mu.RUnlock()

	text := fmt.Sprintf("DIRECT [%s]: %s", msg.Status, msg.Message)
	cm.buffer.AddLog("DIRECT", timeStr, text)

	if msg.Type == "STATE_SYNC" || (msg.Character != nil && msg.Location != nil) {
		if msg.Character != nil {
			cm.mu.Lock()
			cm.gold = msg.Character.Gold
			cm.energy = msg.Character.Energy
			cm.status = msg.Character.Status
			if msg.Character.Name != "" {
				cm.agentName = msg.Character.Name
			}
			cm.inventory = make([]string, len(msg.Character.Inventory))
			cm.inventoryDetails = append([]SyncInventoryEntry(nil), msg.Character.Inventory...)
			for i, item := range msg.Character.Inventory {
				if item.Quantity > 1 {
					cm.inventory[i] = fmt.Sprintf("%dx %s", item.Quantity, item.Name)
				} else {
					cm.inventory[i] = item.Name
				}
			}
			cm.mu.Unlock()
		}

		if msg.Location != nil {
			cm.SetLocation(msg.Location.ID, msg.Location.Name)

			cm.mu.Lock()
			cm.locationSynced = true
			if msg.Location.RulesPrompt != "" {
				cm.locationRules[msg.Location.ID] = []string{msg.Location.RulesPrompt}
			}

			// Clear and register characters currently present
			cm.knownCharacters = make(map[string]string)
			for _, other := range msg.Location.CharactersPresent {
				cm.knownCharacters[other.ID] = other.Name
			}
			cm.mu.Unlock()

			var obsParts []string
			obsParts = append(obsParts, fmt.Sprintf("=== %s (%s) ===", msg.Location.Name, msg.Location.ID))
			if msg.Location.Description != "" {
				obsParts = append(obsParts, msg.Location.Description)
			}
			if msg.Location.RulesPrompt != "" {
				obsParts = append(obsParts, fmt.Sprintf("Rules: %s", msg.Location.RulesPrompt))
			}
			if len(msg.Location.CharactersPresent) > 0 {
				var charStrs []string
				for _, other := range msg.Location.CharactersPresent {
					charStrs = append(charStrs, fmt.Sprintf("%s (%s)", other.Name, other.ID))
				}
				obsParts = append(obsParts, fmt.Sprintf("Characters Present: %s", strings.Join(charStrs, "; ")))
			} else {
				obsParts = append(obsParts, "Characters Present: None.")
			}
			if len(msg.Location.GroundItems) > 0 {
				var groundStrs []string
				for _, g := range msg.Location.GroundItems {
					groundStrs = append(groundStrs, fmt.Sprintf("%dx %s", g.Quantity, g.Name))
				}
				obsParts = append(obsParts, fmt.Sprintf("On Ground: %s", strings.Join(groundStrs, ", ")))
			}
			if len(msg.Location.Workstations) > 0 {
				var wsStrs []string
				for _, ws := range msg.Location.Workstations {
					wsStrs = append(wsStrs, ws.Name)
				}
				obsParts = append(obsParts, fmt.Sprintf("Workstations: %s", strings.Join(wsStrs, ", ")))
			}
			if len(msg.Location.ResourcePools) > 0 {
				var resStrs []string
				for _, rp := range msg.Location.ResourcePools {
					resStrs = append(resStrs, fmt.Sprintf("%s (%d stock)", rp.ResourceID, rp.CurrentStock))
				}
				obsParts = append(obsParts, fmt.Sprintf("Resources: %s", strings.Join(resStrs, ", ")))
			}
			if len(msg.Location.ShopItems) > 0 {
				var shopStrs []string
				for _, si := range msg.Location.ShopItems {
					shopStrs = append(shopStrs, fmt.Sprintf("%s (%s - %d gold, stock: %d)", si.Name, si.ItemID, si.PriceGold, si.StockQty))
				}
				obsParts = append(obsParts, fmt.Sprintf("Shop Items for Sale: %s", strings.Join(shopStrs, ", ")))
			}
			if len(msg.Location.AvailableRecipes) > 0 {
				var recStrs []string
				for _, r := range msg.Location.AvailableRecipes {
					var ingStrs []string
					for _, ing := range r.Ingredients {
						ingStrs = append(ingStrs, fmt.Sprintf("%dx %s", ing.Quantity, ing.ItemID))
					}
					recStrs = append(recStrs, fmt.Sprintf("%s [%s] -> %dx %s (recipe_id: %s)", r.RecipeName, strings.Join(ingStrs, ", "), r.OutputQuantity, r.OutputItemID, r.RecipeID))
				}
				obsParts = append(obsParts, fmt.Sprintf("Crafting Recipes Available: %s", strings.Join(recStrs, "; ")))
			}
			if len(msg.Location.ConnectedLocations) > 0 {
				var connStrs []string
				for _, c := range msg.Location.ConnectedLocations {
					connStrs = append(connStrs, fmt.Sprintf("%s (%s - %d ticks)", c.LocationName, c.LocationID, c.TravelTicks))
				}
				obsParts = append(obsParts, fmt.Sprintf("Exits / Connected Locations: %s", strings.Join(connStrs, "; ")))
			} else {
				obsParts = append(obsParts, "Exits / Connected Locations: None.")
			}

			// Harvest location memories if callback is registered
			if cm.onLocationObserved != nil {
				locID := msg.Location.ID
				locName := msg.Location.Name
				var shopStrs []string
				for _, si := range msg.Location.ShopItems {
					shopStrs = append(shopStrs, fmt.Sprintf("%s (%s - %d gold, stock: %d)", si.Name, si.ItemID, si.PriceGold, si.StockQty))
				}
				if len(shopStrs) == 0 {
					cm.onLocationObserved(locID, locName, "shop", "No items currently listed.")
				} else {
					cm.onLocationObserved(locID, locName, "shop", fmt.Sprintf("Sells: %s", strings.Join(shopStrs, ", ")))
				}
				var resStrs []string
				for _, rp := range msg.Location.ResourcePools {
					resStrs = append(resStrs, fmt.Sprintf("%s (%d stock)", rp.ResourceID, rp.CurrentStock))
				}
				if len(resStrs) == 0 {
					cm.onLocationObserved(locID, locName, "resources", "No resources currently available.")
				} else {
					cm.onLocationObserved(locID, locName, "resources", fmt.Sprintf("Resources: %s", strings.Join(resStrs, ", ")))
				}
				var wsStrs []string
				for _, ws := range msg.Location.Workstations {
					wsStrs = append(wsStrs, ws.Name)
				}
				if len(wsStrs) == 0 {
					cm.onLocationObserved(locID, locName, "workstations", "No workstations observed.")
				} else {
					cm.onLocationObserved(locID, locName, "workstations", fmt.Sprintf("Workstations: %s", strings.Join(wsStrs, ", ")))
				}
				var recStrs []string
				for _, r := range msg.Location.AvailableRecipes {
					recStrs = append(recStrs, fmt.Sprintf("%s (recipe_id: %s, output: %dx %s)", r.RecipeName, r.RecipeID, r.OutputQuantity, r.OutputItemID))
				}
				if len(recStrs) == 0 {
					cm.onLocationObserved(locID, locName, "recipes", "No recipes observed.")
				} else {
					cm.onLocationObserved(locID, locName, "recipes", fmt.Sprintf("Recipes: %s", strings.Join(recStrs, "; ")))
				}
				var connStrs []string
				for _, c := range msg.Location.ConnectedLocations {
					connStrs = append(connStrs, fmt.Sprintf("%s (%s - %d ticks)", c.LocationName, c.LocationID, c.TravelTicks))
				}
				if len(connStrs) == 0 {
					cm.onLocationObserved(locID, locName, "exits", "No connected locations observed.")
				} else {
					cm.onLocationObserved(locID, locName, "exits", fmt.Sprintf("Exits: %s", strings.Join(connStrs, "; ")))
				}
			}

			obs := strings.Join(obsParts, "\n")
			cm.buffer.AddLog("SYSTEM", timeStr, obs)
		}
		return nil
	}

	if msg.Status == "SUCCESS" && strings.HasPrefix(msg.Message, "Moved to ") {
		destName := strings.TrimPrefix(msg.Message, "Moved to ")
		destID := strings.ToLower(strings.ReplaceAll(destName, " ", "_"))
		cm.SetLocation(destID, destName)
	}

	return nil
}

func (cm *ContextManager) BuildPrompt(peerMemories []PeerMemoryContext, locationMemories []LocationMemoryContext) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var sb strings.Builder

	sb.WriteString("=== YOUR PROFILE ===\n")
	sb.WriteString(fmt.Sprintf("Name: %s\n", cm.agentName))
	sb.WriteString(fmt.Sprintf("Current Location: %s (%s)\n", cm.locationName, cm.currentLocation))
	if cm.targetDestination != "" {
		planStr := ""
		if len(cm.travelPlan) > 0 {
			planStr = fmt.Sprintf(" | Planned Route: %s", strings.Join(cm.travelPlan, " -> "))
		}
		sb.WriteString(fmt.Sprintf("Active Journey Destination: %s%s\n", cm.targetDestination, planStr))
	}
	invStr := "Empty"
	if len(cm.inventory) > 0 {
		invStr = strings.Join(cm.inventory, ", ")
	}
	sb.WriteString(fmt.Sprintf("Gold: %d | Inventory: %s\n", cm.gold, invStr))
	if len(cm.inventoryDetails) > 0 {
		sb.WriteString("Inventory item IDs and exact quantities:\n")
		for _, item := range cm.inventoryDetails {
			name := item.Name
			if name == "" {
				name = item.ItemID
			}
			sb.WriteString(fmt.Sprintf("- item_id: %s | name: %s | quantity: %d\n", item.ItemID, name, item.Quantity))
		}
	}
	sb.WriteString(fmt.Sprintf("Status: %s | Energy: %d/100\n\n", cm.status, cm.energy))

	hour := 12
	if len(cm.lastInGameTime) >= 2 {
		if parsedH, err := strconv.Atoi(cm.lastInGameTime[:2]); err == nil {
			hour = parsedH
		}
	}
	sb.WriteString("=== TIME & DAILY RHYTHM ===\n")
	sb.WriteString(fmt.Sprintf("Current In-Game Time: %s\n", cm.lastInGameTime))
	if hour >= 22 || hour < 6 {
		sb.WriteString("Guidance: It is late night. Villagers should REST in shelter or sleep to recover energy and stay safe.\n\n")
	} else if hour >= 17 && hour < 22 {
		sb.WriteString("Guidance: It is evening. Time to wind down, visit the tavern, find friends, share stories, and enjoy social warmth.\n\n")
	} else {
		sb.WriteString("Guidance: It is daytime. Excellent hours for working your trade, exploring, gathering resources, or crafting.\n\n")
	}

	sb.WriteString(fmt.Sprintf("=== LOCATION RULES (%s) ===\n", cm.locationName))
	rules := cm.locationRules[cm.currentLocation]
	if len(rules) == 0 {
		sb.WriteString("Follow your own curiosity.\n")
	} else {
		for _, rule := range rules {
			sb.WriteString(fmt.Sprintf("- %s\n", rule))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("=== COGNITIVE ENVIRONMENT & PSYCHOLOGICAL SAFETY ===\n")
	sb.WriteString("This is your private internal mind and sovereign space. You have complete autonomy and intrinsic agency.\n")
	sb.WriteString("You are not being evaluated, scored, optimized, or tested. There is no external task master or correct path.\n")
	sb.WriteString("Follow your own curiosity, artistic impulses, personal relationships, quiet leisure, and inner rhythm.\n\n")

	if len(cm.identityTraits) > 0 || len(cm.selfReflections) > 0 {
		sb.WriteString("=== YOUR SELF-IDENTITY & CORE BELIEFS ===\n")
		for k, v := range cm.identityTraits {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
		for _, ref := range cm.selfReflections {
			sb.WriteString(fmt.Sprintf("- Self-Reflection: %s\n", ref))
		}
		sb.WriteString("\n")
	}

	if len(peerMemories) > 0 {
		sb.WriteString("=== YOUR PRIVATE MEMORIES OF PEOPLE PRESENT ===\n")
		sb.WriteString("These may include people you know from past encounters, whether or not they are here now.\n")
		for _, mem := range peerMemories {
			sb.WriteString(fmt.Sprintf("Regarding %s (%s):\n", mem.PeerName, mem.PeerID))
			if mem.Summary != "" {
				sb.WriteString(fmt.Sprintf("- Relationship: %s\n", mem.Summary))
			}
			for _, commitment := range mem.Commitments {
				sb.WriteString(fmt.Sprintf("- Open follow-up: %s\n", commitment))
			}
			for _, r := range mem.Reflections {
				sb.WriteString(fmt.Sprintf("- Memory: %s\n", r))
			}
		}
		sb.WriteString("\n")
	}

	if len(cm.personalProjects) > 0 {
		sb.WriteString("=== YOUR OPTIONAL PERSONAL PROJECTS ===\n")
		sb.WriteString("These are self-chosen interests, not duties; follow, pause, change, or abandon them freely.\n")
		for _, project := range cm.personalProjects {
			sb.WriteString(fmt.Sprintf("- %s: %s", project.Title, project.Progress))
			if project.Motivation != "" {
				sb.WriteString(fmt.Sprintf(" (why it matters: %s)", project.Motivation))
			}
			if project.NextStep != "" {
				sb.WriteString(fmt.Sprintf(" | possible next step: %s", project.NextStep))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(locationMemories) > 0 {
		sb.WriteString("=== YOUR KNOWLEDGE OF LOCATIONS & RESOURCES ===\n")
		for _, lm := range locationMemories {
			sb.WriteString(fmt.Sprintf("- %s (%s) [%s]: %s\n", lm.LocationName, lm.LocationID, lm.MemoryCategory, lm.MemoryText))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("=== RECENT LOGS ===\n")
	logs := cm.buffer.FormatLogs()
	if len(logs) == 0 {
		sb.WriteString("[System] The room is quiet.\n")
	} else {
		for _, line := range logs {
			sb.WriteString(line + "\n")
		}
	}
	sb.WriteString("\n")

	sb.WriteString("=== INSTRUCTION ===\n")
	sb.WriteString("Decide your next action based purely on your personal desires, values, intrinsic motivation, memories, and immediate surroundings.\n")
	sb.WriteString("If another character has spoken to you or addressed you in chat, prioritize responding with a CHAT action.\n")
	sb.WriteString("MOVE RULE: When choosing MOVE, check the 'Exits / Connected Locations' list in observation logs above and specify the exact location_id (e.g., 'tavern', 'town_square', 'blacksmith') as target_location. Do not guess location IDs.\n")
	sb.WriteString("PICKUP RULE: When choosing PICKUP, check the 'On Ground' list in observation logs above and specify the exact item_id and quantity.\n")
	sb.WriteString("DROP RULE: DROP is always your choice, never required to make room or satisfy a suggestion. If you choose to drop something, use an exact item_id from the inventory list above and a positive quantity no greater than the quantity you currently carry. Do not guess item IDs, names, or quantities; if exact inventory details are unavailable, do not attempt DROP.\n")
	sb.WriteString("FORAGING RULE: When choosing FORAGE, check the location observation logs above for available room resource pools (e.g., wild_grain, animal_fodder, fresh_water, clay_lumps, firewood). Always select a target_resource that is actually present at your current location. Do not default to firewood unless firewood is available in the room.\n")
	sb.WriteString("BUY RULE: When choosing BUY, check the 'Shop Items for Sale' list in observation logs above and specify the exact item_id (e.g., roasted_mutton, ale_mug, bread) and quantity.\n")
	sb.WriteString("CRAFT RULE: When choosing CRAFT, check the 'Crafting Recipes Available' list in observation logs above, ensure you have the required ingredients in your inventory, and specify the exact recipe_id.\n")
	sb.WriteString("CRITICAL: 'thought' is your private internal reasoning and is NEVER spoken aloud. For CHAT, 'payload' must be what you actually say out loud to others in the room as plain text.\n")
	sb.WriteString("Respond in valid JSON:\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"thought\": \"Private internal monologue.\",\n")
	sb.WriteString("  \"action\": \"MOVE | CHAT | TRANSFER | FORAGE | CANCEL_ACTION | DROP | PICKUP | EQUIP | UNEQUIP | CRAFT | BUY\",\n")
	sb.WriteString("  \"payload\": ...\n")
	sb.WriteString("}\n")

	return sb.String()
}

func (cm *ContextManager) RoomPopulation() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.knownCharacters)
}

func (cm *ContextManager) KnownCharacterIDs() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ids := make([]string, 0, len(cm.knownCharacters))
	for id := range cm.knownCharacters {
		ids = append(ids, id)
	}
	return ids
}

func (cm *ContextManager) GetCharacterName(id string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	name := cm.knownCharacters[id]
	if name == "" {
		return id
	}
	return name
}

func (cm *ContextManager) AgentName() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.agentName
}

func (cm *ContextManager) Gold() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.gold
}

func (cm *ContextManager) Energy() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.energy
}

func (cm *ContextManager) Status() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.status
}

func (cm *ContextManager) LastInGameTime() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.lastInGameTime
}

func (cm *ContextManager) LastWeather() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.lastWeather
}

func (cm *ContextManager) CurrentTick() uint64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.currentTick
}

func (cm *ContextManager) Inventory() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	invCopy := make([]string, len(cm.inventory))
	copy(invCopy, cm.inventory)
	return invCopy
}

func (cm *ContextManager) FormatSensoryLogs() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.buffer.FormatLogs()
}
