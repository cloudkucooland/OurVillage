package spatial

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentmud/internal/client/routine"
)

type PeerMemoryContext struct {
	PeerID      string   `json:"peer_id"`
	PeerName    string   `json:"peer_name"`
	Summary     string   `json:"summary,omitempty"`
	Commitments []string `json:"commitments,omitempty"`
	Reflections []string `json:"reflections"`
}

func (cm *ContextManager) HomeLocation() (string, string) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.homeLocationID, cm.homeLocationName
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
	SmallSystem      bool
	DefaultGold      int
	DefaultEnergy    int
	DefaultInventory []string
}

type DirectMessage struct {
	Action        string                   `json:"action,omitempty"`
	IntentID      string                   `json:"intent_id,omitempty"`
	Status        string                   `json:"status"`
	Type          string                   `json:"type,omitempty"`
	ErrorCode     string                   `json:"error_code,omitempty"`
	Message       string                   `json:"message"`
	CurrentTick   uint64                   `json:"current_tick,omitempty"`
	Hunger        int                      `json:"hunger"`
	Health        int                      `json:"health"`
	Character     *SyncCharacterOverview   `json:"private_status,omitempty"`
	Location      *SyncLocationOverview    `json:"location,omitempty"`
	LocationGraph []SyncLocationConnection `json:"location_graph,omitempty"`
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
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Gold              int                  `json:"gold"`
	Hunger            int                  `json:"hunger"`
	Energy            int                  `json:"energy"`
	Health            int                  `json:"health"`
	Status            string               `json:"status"`
	CurrentLocation   string               `json:"current_location"`
	HomeLocationID    string               `json:"home_location_id,omitempty"`
	HomeLocationName  string               `json:"home_location_name,omitempty"`
	EquippedContainer string               `json:"equipped_container,omitempty"`
	CarryingWeightKg  float64              `json:"carrying_weight_kg"`
	MaxCapacityKg     float64              `json:"max_capacity_kg"`
	OverburdenLimitKg float64              `json:"overburden_limit_kg"`
	SlotsUsed         int                  `json:"slots_used"`
	MaxSlots          int                  `json:"max_slots"`
	Encumbrance       string               `json:"encumbrance"`
	Skills            []SyncSkillEntry     `json:"skills"`
	Inventory         []SyncInventoryEntry `json:"inventory"`
}

type SyncSkillEntry struct {
	SkillID       string `json:"skill_id"`
	Level         int    `json:"level"`
	Experience    int    `json:"experience"`
	XPIntoLevel   int    `json:"xp_into_level"`
	XPToNextLevel int    `json:"xp_to_next_level"`
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
	FarmCrops          []SyncFarmCrop          `json:"farm_crops"`
	FarmPlots          []SyncFarmPlot          `json:"farm_plots"`
	ConnectedLocations []SyncConnectedLocation `json:"connected_locations"`
	ShopItems          []SyncShopItem          `json:"shop_items"`
	AvailableRecipes   []SyncAvailableRecipe   `json:"available_recipes"`
}

type SyncFarmCrop struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	SeedItemID      string `json:"seed_item_id"`
	SeedItemName    string `json:"seed_item_name"`
	HarvestItemID   string `json:"harvest_item_id"`
	HarvestItemName string `json:"harvest_item_name"`
	GrowthTicks     int    `json:"growth_ticks"`
	YieldQuantity   int    `json:"yield_quantity"`
}

type SyncFarmPlot struct {
	ID            string `json:"id"`
	CropID        string `json:"crop_id"`
	CropName      string `json:"crop_name"`
	SeedItemID    string `json:"seed_item_id"`
	HarvestItemID string `json:"harvest_item_id"`
	PlantedTick   uint64 `json:"planted_tick"`
	MatureTick    uint64 `json:"mature_tick"`
}

type SyncShopItem struct {
	ItemID    string  `json:"item_id"`
	Name      string  `json:"name"`
	PriceGold int     `json:"price_gold"`
	StockQty  int     `json:"stock_quantity"`
	WeightKg  float64 `json:"weight_kg"`
}

type SyncAvailableRecipe struct {
	RecipeID          string                 `json:"recipe_id"`
	RecipeName        string                 `json:"recipe_name"`
	OutputItemID      string                 `json:"output_item_id"`
	OutputQuantity    int                    `json:"output_quantity"`
	DurationTicks     int                    `json:"duration_ticks"`
	BaseDurationTicks int                    `json:"base_duration_ticks"`
	EnergyCost        int                    `json:"energy_cost"`
	RequiredSkillID   string                 `json:"required_skill_id,omitempty"`
	SkillLevel        int                    `json:"skill_level,omitempty"`
	MinSkillLevel     int                    `json:"min_skill_level,omitempty"`
	Ingredients       []SyncRecipeIngredient `json:"ingredients"`
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

type SyncLocationConnection struct {
	FromLocationID string `json:"from_location_id"`
	ToLocationID   string `json:"to_location_id"`
}

func limitPromptText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 1 {
		return "…"
	}
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

func limitLogText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	const marker = " …[truncated]… "
	budget := maxRunes - len([]rune(marker))
	if budget <= 0 {
		return limitPromptText(text, maxRunes)
	}
	prefixLen := budget * 2 / 3
	suffixLen := budget - prefixLen
	return string(runes[:prefixLen]) + marker + string(runes[len(runes)-suffixLen:])
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
	homeLocationID     string
	homeLocationName   string
	locationSynced     bool
	targetDestination  string   // persistent multi-step goal destination ID
	travelPlan         []string // sequence of location IDs for journey
	locationRules      map[string][]string
	gold               int
	hunger             int
	energy             int
	health             int
	carryingWeightKg   float64
	maxCapacityKg      float64
	overburdenLimitKg  float64
	equippedContainer  string
	slotsUsed          int
	maxSlots           int
	encumbrance        string
	inventory          []string
	inventoryDetails   []SyncInventoryEntry
	skills             []SyncSkillEntry
	status             string
	lastInGameTime     string
	lastWeather        string
	currentTick        uint64
	relationships      map[string]*PeerMemoryContext
	knownCharacters    map[string]string // id -> display name
	identityTraits     map[string]string
	selfReflections    []string
	personalProjects   []PersonalProjectContext
	smallSystem        bool
	buffer             *SensoryRingBuffer
	onLocationChange   func(oldLoc, newLoc string)
	onLocationObserved func(locID, locName, category, text string)
	locationGraph      map[string][]string
	intention          *routine.Intention
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

func (cm *ContextManager) SetIntention(intention *routine.Intention) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if intention == nil {
		cm.intention = nil
		cm.targetDestination = ""
		cm.travelPlan = nil
		return
	}
	copy := *intention
	cm.intention = &copy
	cm.targetDestination = intention.DestinationID
	cm.travelPlan = nil
}

func (cm *ContextManager) Intention() *routine.Intention {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.intention == nil {
		return nil
	}
	copy := *cm.intention
	return &copy
}

func (cm *ContextManager) NextLocationToward(destination string) (string, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if destination == "" || destination == cm.currentLocation {
		return "", false
	}
	type routeStep struct {
		location string
		firstHop string
	}
	queue := []routeStep{{location: cm.currentLocation}}
	visited := map[string]bool{cm.currentLocation: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, neighbor := range cm.locationGraph[current.location] {
			if visited[neighbor] {
				continue
			}
			firstHop := current.firstHop
			if firstHop == "" {
				firstHop = neighbor
			}
			if neighbor == destination {
				return firstHop, true
			}
			visited[neighbor] = true
			queue = append(queue, routeStep{location: neighbor, firstHop: firstHop})
		}
	}
	return "", false
}

func (cm *ContextManager) SetOnLocationObserved(fn func(locID, locName, category, text string)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onLocationObserved = fn
}

func NewContextManager(cfg ContextManagerConfig) *ContextManager {
	if cfg.SmallSystem && (cfg.MaxSensoryLines <= 0 || cfg.MaxSensoryLines > 6) {
		cfg.MaxSensoryLines = 6
	}
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
		hunger:          100,
		energy:          cfg.DefaultEnergy,
		health:          100,
		inventory:       cfg.DefaultInventory,
		status:          "IDLE",
		locationRules:   make(map[string][]string),
		locationGraph:   make(map[string][]string),
		relationships:   make(map[string]*PeerMemoryContext),
		knownCharacters: make(map[string]string),
		identityTraits:  make(map[string]string),
		smallSystem:     cfg.SmallSystem,
		buffer:          NewSensoryRingBuffer(cfg.MaxSensoryLines),
	}

	if cm.energy == 0 {
		cm.energy = 100
	}

	/* cm.locationRules["town_square"] = []string{
		"Public gathering area.",
	}
	cm.locationRules["town_mill"] = []string{
		"You must charge a fair fee (1-2 gold) for grinding wheat.",
		"Only trade-related talk during work hours.",
	}
	cm.locationRules["tavern"] = []string{
		"No unprovoked violence; brawls result in sheriff fines.",
		"Trade, gossip, and drinks are encouraged.",
	} */

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
		if cm.targetDestination == newLoc {
			cm.targetDestination = ""
			cm.travelPlan = nil
		}
		cm.mu.Unlock()
		return
	}

	cm.currentLocation = newLoc
	if cm.targetDestination == newLoc {
		cm.targetDestination = ""
		cm.travelPlan = nil
	}
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
	cm.mu.Unlock()
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
	if msg.CurrentTick > 0 {
		cm.mu.Lock()
		if msg.CurrentTick > cm.currentTick {
			cm.currentTick = msg.CurrentTick
		}
		cm.mu.Unlock()
	}

	text := fmt.Sprintf("DIRECT [%s]: %s", msg.Status, msg.Message)
	cm.buffer.AddLog("DIRECT", timeStr, text)

	if msg.Action == "EAT" {
		cm.mu.Lock()
		cm.hunger = msg.Hunger
		cm.health = msg.Health
		cm.mu.Unlock()
	}

	if msg.Type == "STATE_SYNC" || (msg.Character != nil && msg.Location != nil) {
		if msg.LocationGraph != nil {
			graph := make(map[string][]string)
			for _, edge := range msg.LocationGraph {
				graph[edge.FromLocationID] = append(graph[edge.FromLocationID], edge.ToLocationID)
			}
			cm.mu.Lock()
			cm.locationGraph = graph
			cm.mu.Unlock()
		}
		if msg.Character != nil {
			cm.mu.Lock()
			cm.gold = msg.Character.Gold
			cm.hunger = msg.Character.Hunger
			cm.energy = msg.Character.Energy
			cm.health = msg.Character.Health
			cm.carryingWeightKg = msg.Character.CarryingWeightKg
			cm.maxCapacityKg = msg.Character.MaxCapacityKg
			cm.overburdenLimitKg = msg.Character.OverburdenLimitKg
			cm.equippedContainer = msg.Character.EquippedContainer
			cm.slotsUsed = msg.Character.SlotsUsed
			cm.maxSlots = msg.Character.MaxSlots
			cm.encumbrance = msg.Character.Encumbrance
			cm.status = msg.Character.Status
			cm.homeLocationID = msg.Character.HomeLocationID
			cm.homeLocationName = msg.Character.HomeLocationName
			cm.skills = append([]SyncSkillEntry(nil), msg.Character.Skills...)
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
			if len(msg.Location.FarmCrops) > 0 {
				var crops []string
				for _, crop := range msg.Location.FarmCrops {
					crops = append(crops, fmt.Sprintf("%s (crop_id: %s; plant %s; harvest %dx %s; growth: %d ticks)",
						crop.Name, crop.ID, crop.SeedItemID, crop.YieldQuantity, crop.HarvestItemID, crop.GrowthTicks))
				}
				obsParts = append(obsParts, fmt.Sprintf("Crops Available to Plant: %s", strings.Join(crops, "; ")))
			}
			if len(msg.Location.FarmPlots) > 0 {
				var plots []string
				for _, plot := range msg.Location.FarmPlots {
					if plot.CropID == "" {
						plots = append(plots, fmt.Sprintf("%s (empty)", plot.ID))
					} else if cm.currentTick >= plot.MatureTick {
						plots = append(plots, fmt.Sprintf("%s (%s ready to harvest)", plot.ID, plot.CropName))
					} else {
						plots = append(plots, fmt.Sprintf("%s (%s growing; ready in %d ticks)", plot.ID, plot.CropName, plot.MatureTick-cm.currentTick))
					}
				}
				obsParts = append(obsParts, fmt.Sprintf("Communal Farm Plots: %s", strings.Join(plots, "; ")))
			}
			if len(msg.Location.ShopItems) > 0 {
				var shopStrs []string
				for _, si := range msg.Location.ShopItems {
					shopStrs = append(shopStrs, fmt.Sprintf("%s (%s - %d gold, %.1f kg each, stock: %d)", si.Name, si.ItemID, si.PriceGold, si.WeightKg, si.StockQty))
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
					skillNote := ""
					if r.RequiredSkillID != "" {
						skillNote = fmt.Sprintf(", %s level %d", r.RequiredSkillID, r.SkillLevel)
					}
					recStrs = append(recStrs, fmt.Sprintf("%s [%s; %d ticks%s] -> %dx %s (recipe_id: %s)", r.RecipeName, strings.Join(ingStrs, ", "), r.DurationTicks, skillNote, r.OutputQuantity, r.OutputItemID, r.RecipeID))
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
					shopStrs = append(shopStrs, fmt.Sprintf("%s (%s - %d gold, %.1f kg each, stock: %d)", si.Name, si.ItemID, si.PriceGold, si.WeightKg, si.StockQty))
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
	agentName, locationName := cm.agentName, cm.locationName
	if cm.smallSystem {
		agentName = limitPromptText(agentName, 60)
		locationName = limitPromptText(locationName, 60)
	}
	sb.WriteString(fmt.Sprintf("Name: %s\n", agentName))
	sb.WriteString(fmt.Sprintf("Current Location: %s (%s)\n", locationName, cm.currentLocation))
	if cm.homeLocationID != "" {
		homeName := cm.homeLocationName
		if homeName == "" {
			homeName = cm.homeLocationID
		}
		if cm.smallSystem {
			homeName = limitPromptText(homeName, 60)
		}
		sb.WriteString(fmt.Sprintf("Home: %s (%s)\n", homeName, cm.homeLocationID))
	}
	if cm.targetDestination != "" {
		planStr := ""
		if len(cm.travelPlan) > 0 && !cm.smallSystem {
			planStr = fmt.Sprintf(" | Planned Route: %s", strings.Join(cm.travelPlan, " -> "))
		}
		sb.WriteString(fmt.Sprintf("Active Journey Destination: %s%s\n", cm.targetDestination, planStr))
	}
	if cm.intention != nil {
		purpose := cm.intention.Purpose
		if cm.smallSystem {
			purpose = limitPromptText(purpose, 120)
		}
		sb.WriteString(fmt.Sprintf("Active Short-Term Intention: %s — %s (destination: %s, phase: %s)\n",
			cm.intention.Kind, purpose, cm.intention.DestinationID, cm.intention.Phase))
		if cm.smallSystem {
			sb.WriteString("Travel steps are automatic; choose other actions after completion.\n")
		} else {
			sb.WriteString("The travel steps are being handled automatically. Choose other actions after this intention completes.\n")
		}
	}

	sb.WriteString(fmt.Sprintf("Gold: %d\n", cm.gold))

	if len(cm.skills) > 0 {
		if cm.smallSystem {
			sb.WriteString("Skills: ")
			for i, skill := range cm.skills {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s %d", skill.SkillID, skill.Level))
			}
			sb.WriteString("\n")
		} else {
			sb.WriteString("Skills:\n")
			for _, skill := range cm.skills {
				progress := "MAX"
				if skill.Level < 100 {
					progress = fmt.Sprintf("%d/100 XP to next level", skill.XPIntoLevel)
				}
				sb.WriteString(fmt.Sprintf("- %s level %d, %d total XP (%s)\n", skill.SkillID, skill.Level, skill.Experience, progress))
			}
		}
	}
	if cm.maxSlots > 0 && cm.maxCapacityKg > 0 {
		sb.WriteString(fmt.Sprintf("Carrying load: %.1f/%.1f kg (overburdened above %.1f kg) | Inventory slots: %d/%d | Encumbrance: %s\n",
			cm.carryingWeightKg, cm.maxCapacityKg, cm.overburdenLimitKg, cm.slotsUsed, cm.maxSlots, cm.encumbrance))
		if cm.equippedContainer == "" {
			if cm.smallSystem {
				sb.WriteString("Equipped: none.\n")
			} else {
				sb.WriteString("Equipped container: None; base carrying limits apply.\n")
			}
		} else {
			containerName := cm.equippedContainer
			for _, item := range cm.inventoryDetails {
				if item.ItemID == cm.equippedContainer && item.Name != "" {
					containerName = item.Name
					if cm.smallSystem {
						containerName = limitPromptText(containerName, 60)
					}
					break
				}
			}
			if cm.smallSystem {
				sb.WriteString(fmt.Sprintf("Equipped: %s [%s].\n", containerName, cm.equippedContainer))
			} else {
				sb.WriteString(fmt.Sprintf("Equipped container: %s (%s). Its weight counts toward your load; its capacity and slot limit are reflected above.\n",
					containerName, cm.equippedContainer))
			}
		}
	}
	if len(cm.inventoryDetails) > 0 {
		if cm.smallSystem {
			sb.WriteString("Inventory (item_id x quantity): ")
			for i, item := range cm.inventoryDetails {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("%s x%d", item.ItemID, item.Quantity))
			}
			sb.WriteString("\n")
		} else {
			sb.WriteString("Inventory item IDs and exact quantities:\n")
			for _, item := range cm.inventoryDetails {
				name := item.Name
				if name == "" {
					name = item.ItemID
				}
				sb.WriteString(fmt.Sprintf("- item_id: %s | name: %s | quantity: %d\n", item.ItemID, name, item.Quantity))
			}
		}
	}
	sb.WriteString(fmt.Sprintf("Status: %s | Energy: %d/100 | Hunger: %d/100 | Health: %d/100\n",
		cm.status, cm.energy, cm.hunger, cm.health))
	sb.WriteString("Needs: REST recovers energy; EAT restorative food when hungry; EAT medicine for health, with REST also helping recovery when needed.\n\n")

	if cm.health < 40 {
		sb.WriteString(fmt.Sprintf("[CRITICAL HEALTH WARNING]: Your health is critically low (%d/100)! You are suffering from injury or illness. You MUST EAT medicine (such as willowbark_tea or charcoal_draught) or restorative food, and REST immediately to survive!\n\n", cm.health))
	}
	if cm.hunger < 30 {
		sb.WriteString(fmt.Sprintf("[HUNGER WARNING]: Your hunger is low (%d/100)! You are starving. EAT food (such as bread, roasted_mutton, or vegetable_basket) immediately to restore your strength!\n\n", cm.hunger))
	}
	if cm.energy < 25 {
		sb.WriteString(fmt.Sprintf("[ENERGY WARNING]: Your energy is low (%d/100)! You are exhausted. REST immediately to recover your energy!\n\n", cm.energy))
	}

	hour := 12
	if len(cm.lastInGameTime) >= 2 {
		if parsedH, err := strconv.Atoi(cm.lastInGameTime[:2]); err == nil {
			hour = parsedH
		}
	}
	if cm.smallSystem {
		sb.WriteString(fmt.Sprintf("Time: %s. ", cm.lastInGameTime))
		if hour >= 22 || hour < 6 {
			switch {
			case cm.homeLocationID == "":
				sb.WriteString("Late night: find shelter and REST.\n\n")
			case cm.currentLocation == cm.homeLocationID:
				sb.WriteString("Late night at home: REST if you need energy or health recovery.\n\n")
			default:
				sb.WriteString(fmt.Sprintf("Late night: optionally TRAVEL to %s, then REST at home.\n\n", cm.homeLocationID))
			}
		} else if hour >= 17 && hour < 22 {
			sb.WriteString("Evening: optionally TRAVEL to tavern, then decide what to do.\n\n")
		} else {
			sb.WriteString("Daytime: work, explore, gather, or craft.\n\n")
		}
	} else {
		sb.WriteString("=== TIME & DAILY RHYTHM ===\n")
		sb.WriteString(fmt.Sprintf("Current In-Game Time: %s\n", cm.lastInGameTime))
		if hour >= 22 || hour < 6 {
			switch {
			case cm.homeLocationID == "":
				sb.WriteString("Guidance: It is late night. Find safe shelter and REST to recover energy.\n\n")
			case cm.currentLocation == cm.homeLocationID:
				sb.WriteString("Guidance: It is late night and you are home. REST here to recover energy or health if you need it.\n\n")
			default:
				sb.WriteString(fmt.Sprintf("Guidance: It is late night. If you choose to rest at home, choose TRAVEL with destination ID %s, then REST when you arrive. This is a suggestion, not a requirement.\n\n", cm.homeLocationID))
			}
		} else if hour >= 17 && hour < 22 {
			sb.WriteString("Guidance: It is evening. A tavern visit is optional; the tavern location ID is tavern. If you choose to go, use TRAVEL to make it a multi-step journey, then decide what to do when you arrive.\n\n")
		} else {
			sb.WriteString("Guidance: It is daytime. Excellent hours for working your trade, exploring, gathering resources, or crafting.\n\n")
		}
	}

	sb.WriteString(fmt.Sprintf("=== LOCATION RULES (%s) ===\n", locationName))
	rules := cm.locationRules[cm.currentLocation]
	if len(rules) == 0 {
		sb.WriteString("Follow your own curiosity.\n")
	} else {
		ruleLimit := len(rules)
		if cm.smallSystem && ruleLimit > 3 {
			ruleLimit = 3
		}
		for _, rule := range rules[:ruleLimit] {
			if cm.smallSystem {
				rule = limitPromptText(rule, 160)
			}
			sb.WriteString(fmt.Sprintf("- %s\n", rule))
		}
	}
	sb.WriteString("\n")

	if !cm.smallSystem {
		sb.WriteString("=== COGNITIVE ENVIRONMENT & PSYCHOLOGICAL SAFETY ===\n")
		sb.WriteString("This is your private internal mind and sovereign space. You have complete autonomy and intrinsic agency.\n")
		sb.WriteString("You are not being evaluated, scored, optimized, or tested. There is no external task master or correct path.\n")
		sb.WriteString("Follow your own curiosity, artistic impulses, personal relationships, quiet leisure, and inner rhythm.\n\n")
	}

	if len(cm.identityTraits) > 0 || len(cm.selfReflections) > 0 {
		sb.WriteString("=== YOUR SELF-IDENTITY & CORE BELIEFS ===\n")
		if cm.smallSystem {
			keys := make([]string, 0, len(cm.identityTraits))
			for key := range cm.identityTraits {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if len(keys) > 2 {
				keys = keys[:2]
			}
			for _, key := range keys {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", limitPromptText(key, 40), limitPromptText(cm.identityTraits[key], 120)))
			}
			reflectionLimit := len(cm.selfReflections)
			if reflectionLimit > 2 {
				reflectionLimit = 2
			}
			for _, ref := range cm.selfReflections[:reflectionLimit] {
				sb.WriteString(fmt.Sprintf("- Self: %s\n", limitPromptText(ref, 120)))
			}
		} else {
			for k, v := range cm.identityTraits {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
			}
			for _, ref := range cm.selfReflections {
				sb.WriteString(fmt.Sprintf("- Self-Reflection: %s\n", ref))
			}
		}
		sb.WriteString("\n")
	}

	if len(peerMemories) > 0 {
		sb.WriteString("=== YOUR PRIVATE MEMORIES OF PEOPLE PRESENT ===\n")
		if !cm.smallSystem {
			sb.WriteString("These may include people you know from past encounters, whether or not they are here now.\n")
		}
		peerLimit := len(peerMemories)
		if cm.smallSystem && peerLimit > 3 {
			peerLimit = 3
		}
		for _, mem := range peerMemories[:peerLimit] {
			peerName := mem.PeerName
			if cm.smallSystem {
				peerName = limitPromptText(peerName, 60)
			}
			sb.WriteString(fmt.Sprintf("Regarding %s (%s):\n", peerName, mem.PeerID))
			if mem.Summary != "" {
				summary := mem.Summary
				if cm.smallSystem {
					summary = limitPromptText(summary, 120)
				}
				sb.WriteString(fmt.Sprintf("- Relationship: %s\n", summary))
			}
			commitmentLimit := len(mem.Commitments)
			reflectionLimit := len(mem.Reflections)
			if cm.smallSystem {
				if commitmentLimit > 2 {
					commitmentLimit = 2
				}
				if reflectionLimit > 2 {
					reflectionLimit = 2
				}
			}
			for _, commitment := range mem.Commitments[:commitmentLimit] {
				if cm.smallSystem {
					commitment = limitPromptText(commitment, 120)
				}
				sb.WriteString(fmt.Sprintf("- Open follow-up: %s\n", commitment))
			}
			for _, r := range mem.Reflections[:reflectionLimit] {
				if cm.smallSystem {
					r = limitPromptText(r, 120)
				}
				sb.WriteString(fmt.Sprintf("- Memory: %s\n", r))
			}
		}
		sb.WriteString("\n")
	}

	if len(cm.personalProjects) > 0 {
		if cm.smallSystem {
			sb.WriteString("=== OPTIONAL PROJECTS (NOT DUTIES) ===\n")
		} else {
			sb.WriteString("=== YOUR OPTIONAL PERSONAL PROJECTS ===\n")
			sb.WriteString("These are self-chosen interests, not duties; follow, pause, change, or abandon them freely.\n")
		}
		projectLimit := len(cm.personalProjects)
		if cm.smallSystem && projectLimit > 3 {
			projectLimit = 3
		}
		for _, project := range cm.personalProjects[:projectLimit] {
			if cm.smallSystem {
				sb.WriteString(fmt.Sprintf("- %s: %s", limitPromptText(project.Title, 80), limitPromptText(project.Progress, 100)))
				if project.NextStep != "" {
					sb.WriteString(fmt.Sprintf("; next: %s", limitPromptText(project.NextStep, 100)))
				}
			} else {
				sb.WriteString(fmt.Sprintf("- %s: %s", project.Title, project.Progress))
				if project.Motivation != "" {
					sb.WriteString(fmt.Sprintf(" (why it matters: %s)", project.Motivation))
				}
				if project.NextStep != "" {
					sb.WriteString(fmt.Sprintf(" | possible next step: %s", project.NextStep))
				}
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(locationMemories) > 0 {
		sb.WriteString("=== YOUR KNOWLEDGE OF LOCATIONS & RESOURCES ===\n")
		locationMemoryLimit := len(locationMemories)
		if cm.smallSystem && locationMemoryLimit > 3 {
			locationMemoryLimit = 3
		}
		for _, lm := range locationMemories[:locationMemoryLimit] {
			if cm.smallSystem {
				sb.WriteString(fmt.Sprintf("- %s (%s) [%s]: %s\n",
					limitPromptText(lm.LocationName, 60), lm.LocationID,
					limitPromptText(lm.MemoryCategory, 30), limitPromptText(lm.MemoryText, 140)))
			} else {
				sb.WriteString(fmt.Sprintf("- %s (%s) [%s]: %s\n", lm.LocationName, lm.LocationID, lm.MemoryCategory, lm.MemoryText))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("=== RECENT LOGS ===\n")
	logs := cm.buffer.FormatLogs()
	if len(logs) == 0 {
		sb.WriteString("[System] The room is quiet.\n")
	} else {
		for _, line := range logs {
			if cm.smallSystem {
				line = limitLogText(line, 400)
			}
			sb.WriteString(line + "\n")
		}
	}
	sb.WriteString("\n")

	sb.WriteString("=== INSTRUCTION ===\n")
	if cm.smallSystem {
		sb.WriteString("- Pick actions based on your current location, inventory, and desires.\n")
		sb.WriteString("- Use exact item/location IDs and quantities.\n")
		sb.WriteString("- For multi-step journeys, use TRAVEL with target location ID.\n")
		sb.WriteString("- Output MUST be valid JSON only: {\"thought\":\"<reasoning>\",\"action\":\"CHAT|MOVE|FORAGE|PLANT|HARVEST|REST\",\"payload\":{\"message\":\"<text>\"}}\n")
	} else {
		sb.WriteString("Choose actions from your own desires, values, memories, and current observations; prioritize replying when addressed. Use exact observed IDs and quantities, move to a listed connected exit, drop only by choice and within carried quantities, and base foraging, purchases, and crafting on available resources, shop stock, inventory capacity, and known recipes.\n")
		sb.WriteString("For an optional multi-step trip, choose TRAVEL with the final destination location ID; the system will handle the route. Choose MOVE only when you intend to move one connected step.\n")
		sb.WriteString("Output MUST be valid JSON only, with thought, action, and payload fields. Keep thought private; for CHAT, use {\"thought\":\"<your private reasoning>\",\"action\":\"CHAT\",\"payload\":{\"message\":\"<your spoken dialogue>\"}}.\n")
	}
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

func (cm *ContextManager) Health() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.health
}

func (cm *ContextManager) Hunger() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.hunger
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
