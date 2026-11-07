package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/memory"
	"agentmud/internal/client/model"
	"agentmud/internal/client/routine"
	"agentmud/internal/client/spatial"
	"agentmud/internal/client/tui"

	tea "github.com/charmbracelet/bubbletea"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// BuildIntentMessage generates the RFC 002 compliant intent JSON payload based on ActionConfig.
func BuildIntentMessage(actionCfg ActionConfig) ([]byte, error) {
	intentID := dispatcher.GenerateIntentID()
	var payload json.RawMessage
	var err error

	switch actionCfg.Action {
	case "MOVE":
		payload, err = json.Marshal(map[string]string{
			"target_location": actionCfg.TargetLoc,
		})
	case "TRAVEL":
		payload, err = json.Marshal(map[string]string{
			"target_location": actionCfg.TargetLoc,
		})
	case "CHAT":
		payload, err = json.Marshal(actionCfg.ChatMsg)
	case "TRANSFER":
		payload, err = json.Marshal(map[string]interface{}{
			"recipient_id": actionCfg.Recipient,
			"amount":       actionCfg.Amount,
			"item_id":      actionCfg.Item,
			"quantity":     actionCfg.Qty,
		})
	case "FORAGE", "COLLECT":
		payload, err = json.Marshal(map[string]interface{}{
			"target_resource": actionCfg.Resource,
			"duration_ticks":  actionCfg.Duration,
		})
	case "CANCEL_ACTION", "LOOK", "STATUS", "UNEQUIP":
		payload, err = json.Marshal(map[string]interface{}{})
	case "DROP":
		payload, err = json.Marshal(map[string]interface{}{
			"item_id":  actionCfg.Item,
			"quantity": actionCfg.Qty,
		})
	case "PICKUP":
		payload, err = json.Marshal(map[string]interface{}{
			"item_id":  actionCfg.Item,
			"quantity": actionCfg.Qty,
		})
	case "EQUIP":
		payload, err = json.Marshal(map[string]interface{}{
			"item_id": actionCfg.Item,
		})
	case "CRAFT":
		payload, err = json.Marshal(map[string]interface{}{
			"recipe_id": actionCfg.Recipe,
		})
	case "PLANT":
		payload, err = json.Marshal(map[string]string{
			"plot_id": actionCfg.PlotID,
			"crop_id": actionCfg.CropID,
		})
	case "HARVEST":
		payload, err = json.Marshal(map[string]string{
			"plot_id": actionCfg.PlotID,
		})
	case "BUY":
		payload, err = json.Marshal(map[string]interface{}{
			"item_id":  actionCfg.Item,
			"quantity": actionCfg.Qty,
		})
	case "RESTOCK":
		payload, err = json.Marshal(map[string]interface{}{
			"item_id":    actionCfg.Item,
			"quantity":   actionCfg.Qty,
			"price_gold": actionCfg.Amount,
		})
	case "REST":
		duration := actionCfg.Duration
		if duration <= 0 {
			duration = 30
		}
		payload, err = json.Marshal(map[string]interface{}{
			"duration_ticks": duration,
		})
	default:
		return nil, fmt.Errorf("unknown action: %s", actionCfg.Action)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to marshal action payload: %w", err)
	}

	intent := dispatcher.Intent{
		IntentID: intentID,
		Action:   actionCfg.Action,
		Payload:  payload,
	}

	validated, err := dispatcher.ValidateIntent(intent)
	if err != nil {
		return nil, err
	}

	return json.Marshal(validated)
}

func processActiveIntention(ctx context.Context, agentID string, ctxMgr *spatial.ContextManager, store *memory.ClientMemoryStore, disp *dispatcher.IntentDispatcher) (bool, error) {
	intention := ctxMgr.Intention()
	if intention == nil {
		return false, nil
	}
	if !strings.EqualFold(ctxMgr.Status(), "IDLE") {
		if intention.Kind == routine.HomeAndRest && intention.Phase == routine.StartingRest && strings.EqualFold(ctxMgr.Status(), "RESTING") {
			intention.Phase = routine.Resting
			if err := store.SaveActiveIntention(agentID, *intention); err != nil {
				return true, err
			}
			ctxMgr.SetIntention(intention)
		}
		return true, nil
	}

	if ctxMgr.CurrentLocation() != intention.DestinationID {
		next, ok := ctxMgr.NextLocationToward(intention.DestinationID)
		if !ok {
			if err := store.ClearActiveIntention(agentID); err != nil {
				return true, err
			}
			ctxMgr.SetIntention(nil)
			return true, fmt.Errorf("no known route to intention destination %q", intention.DestinationID)
		}
		payload, err := json.Marshal(map[string]string{"target_location": next})
		if err != nil {
			return true, fmt.Errorf("encode intention movement: %w", err)
		}
		result, err := disp.Dispatch(ctx, dispatcher.Intent{
			IntentID: dispatcher.GenerateIntentID(),
			Action:   "MOVE",
			Payload:  payload,
		}, 10*time.Second)
		if err != nil {
			return true, fmt.Errorf("move toward intention destination %q: %w", intention.DestinationID, err)
		}
		if !strings.EqualFold(result.Status, "SUCCESS") {
			if clearErr := store.ClearActiveIntention(agentID); clearErr != nil {
				return true, fmt.Errorf("movement failed (%s): %v; clear intention: %w", result.Message, result.Err, clearErr)
			}
			ctxMgr.SetIntention(nil)
			return true, fmt.Errorf("movement toward intention destination %q failed: %s", intention.DestinationID, result.Message)
		}
		return true, nil
	}

	if intention.Kind == routine.Visit {
		if err := store.ClearActiveIntention(agentID); err != nil {
			return true, err
		}
		ctxMgr.SetIntention(nil)
		ctxMgr.IngestSystem(fmt.Sprintf("You arrived at %s.", intention.DestinationID))
		return true, nil
	}
	if intention.Kind != routine.HomeAndRest {
		if err := store.ClearActiveIntention(agentID); err != nil {
			return true, err
		}
		ctxMgr.SetIntention(nil)
		return true, fmt.Errorf("unsupported short-term intention kind %q", intention.Kind)
	}
	if intention.Phase == routine.Resting {
		if err := store.ClearActiveIntention(agentID); err != nil {
			return true, err
		}
		ctxMgr.SetIntention(nil)
		return true, nil
	}
	if ctxMgr.Energy() >= 100 {
		if err := store.ClearActiveIntention(agentID); err != nil {
			return true, err
		}
		ctxMgr.SetIntention(nil)
		return true, nil
	}

	intention.Phase = routine.StartingRest
	if err := store.SaveActiveIntention(agentID, *intention); err != nil {
		return true, err
	}
	ctxMgr.SetIntention(intention)
	payload, err := json.Marshal(map[string]int{"duration_ticks": 30})
	if err != nil {
		return true, fmt.Errorf("encode intention rest: %w", err)
	}
	result, err := disp.Dispatch(ctx, dispatcher.Intent{
		IntentID: dispatcher.GenerateIntentID(),
		Action:   "REST",
		Payload:  payload,
	}, 10*time.Second)
	if err != nil {
		return true, fmt.Errorf("rest at home: %w", err)
	}
	if !strings.EqualFold(result.Status, "SUCCESS") {
		if clearErr := store.ClearActiveIntention(agentID); clearErr != nil {
			return true, fmt.Errorf("rest failed (%s): %v; clear intention: %w", result.Message, result.Err, clearErr)
		}
		ctxMgr.SetIntention(nil)
		return true, fmt.Errorf("rest at home failed: %s", result.Message)
	}
	intention.Phase = routine.Resting
	if err := store.SaveActiveIntention(agentID, *intention); err != nil {
		return true, err
	}
	ctxMgr.SetIntention(intention)
	return true, nil
}

func beginTravelIntention(intent dispatcher.Intent, agentID string, ctxMgr *spatial.ContextManager, store *memory.ClientMemoryStore) (bool, error) {
	if !strings.EqualFold(intent.Action, "TRAVEL") {
		return false, nil
	}
	var payload struct {
		TargetLocation string `json:"target_location"`
	}
	if err := json.Unmarshal(intent.Payload, &payload); err != nil {
		return true, fmt.Errorf("decode TRAVEL destination: %w", err)
	}
	if payload.TargetLocation != ctxMgr.CurrentLocation() {
		if _, ok := ctxMgr.NextLocationToward(payload.TargetLocation); !ok {
			return true, fmt.Errorf("no known route to TRAVEL destination %q", payload.TargetLocation)
		}
	}
	kind := routine.Visit
	purpose := "Visit " + payload.TargetLocation
	homeLocationID, _ := ctxMgr.HomeLocation()
	if payload.TargetLocation == homeLocationID {
		kind = routine.HomeAndRest
		purpose = "Return home and rest"
	}
	intention := routine.Intention{
		Kind:          kind,
		Purpose:       purpose,
		DestinationID: payload.TargetLocation,
		Phase:         routine.Traveling,
	}
	if err := store.SaveActiveIntention(agentID, intention); err != nil {
		return true, err
	}
	ctxMgr.SetIntention(&intention)
	return true, nil
}

// RunClient initializes the MQTT client and event loops according to the provided configuration.
func RunClient(ctx context.Context, cfg *Config) error {
	clientCtx, cancelClient := context.WithCancel(ctx)
	defer cancelClient()

	logPath := cfg.Client.LogFile
	if logPath == "" && cfg.Client.TUI {
		logPath = "agentmud-client.log"
	}
	if logPath != "" {
		if strings.HasPrefix(logPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				logPath = filepath.Join(home, logPath[2:])
			}
		}
		if dir := filepath.Dir(logPath); dir != "." && dir != "" {
			_ = os.MkdirAll(dir, 0755)
		}
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			log.SetOutput(logFile)
			defer logFile.Close()
		} else if cfg.Client.TUI {
			log.SetOutput(io.Discard)
		}
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize model engine: %w", err)
	}
	engine = model.NewLoggingEngine(engine, log.Default())
	defer engine.Close()

	initLoc := cfg.Action.TargetLoc
	if initLoc == "" {
		initLoc = "town_square"
	}
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{
		AgentID:          cfg.Client.AgentID,
		InitialLocation:  initLoc,
		MaxSensoryLines:  cfg.Cognition.MaxSensoryLogLines,
		SmallSystem:      cfg.Cognition.SmallSystem,
		DefaultGold:      15,
		DefaultEnergy:    100,
		DefaultInventory: []string{"bread", "water_flask"},
	})

	memoryStore, err := memory.NewClientMemoryStore(cfg.Memory.DBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize client memory store: %w", err)
	}
	defer memoryStore.Close()
	activeIntention, err := memoryStore.GetActiveIntention(cfg.Client.AgentID)
	if err != nil {
		return fmt.Errorf("failed to restore active intention: %w", err)
	}
	ctxMgr.SetIntention(activeIntention)

	synthesizer := memory.NewReflectionSynthesizer(
		memoryStore,
		engine,
		cfg.Client.AgentID,
		time.Duration(cfg.Cognition.ReflectionIntervalTicks)*time.Second,
		time.Duration(cfg.Memory.SynthesisTimeoutSeconds)*time.Second,
	)
	synthesizer.SetSocialPresenceChecker(func() bool {
		isResting := strings.EqualFold(ctxMgr.Status(), "RESTING")
		return ctxMgr.RoomPopulation() > 0 && !isResting
	})
	var synthesisStatuses chan string
	if cfg.Client.TUI {
		synthesisStatuses = make(chan string, 8)
		synthesizer.SetStatusCallback(func(status string) {
			select {
			case synthesisStatuses <- status:
			default:
				log.Printf("[Synthesizer Status] %s", status)
			}
		})
	} else {
		synthesizer.SetStatusCallback(func(status string) {
			log.Printf("[Synthesizer Status] %s", status)
		})
	}
	synthesizer.Start(clientCtx)

	fetchPeerMemories := func() []spatial.PeerMemoryContext {
		traits, err := memoryStore.GetIdentityTraits()
		if err != nil {
			log.Printf("[Memory] Failed to load identity traits: %v", err)
		} else if len(traits) > 0 {
			tMap := make(map[string]string)
			for _, t := range traits {
				tMap[t.Key] = t.Value
			}
			ctxMgr.SetIdentityTraits(tMap)
		}
		selfRefs, err := memoryStore.GetRecentSelfReflections(3)
		if err != nil {
			log.Printf("[Memory] Failed to load self-reflections: %v", err)
		} else if len(selfRefs) > 0 {
			var refTexts []string
			for _, r := range selfRefs {
				refTexts = append(refTexts, r.ReflectionText)
			}
			ctxMgr.SetSelfReflections(refTexts)
		}

		projects, err := memoryStore.GetProjects(true, 10)
		if err != nil {
			log.Printf("[Memory] Failed to load personal projects: %v", err)
			ctxMgr.SetPersonalProjects(nil)
		} else {
			projectContexts := make([]spatial.PersonalProjectContext, 0, len(projects))
			for _, project := range projects {
				projectContexts = append(projectContexts, spatial.PersonalProjectContext{
					Title: project.Title, Motivation: project.Motivation,
					Progress: project.Progress, NextStep: project.NextStep,
				})
			}
			ctxMgr.SetPersonalProjects(projectContexts)
		}

		peerByID := make(map[string]*spatial.PeerMemoryContext)
		var peerIDs []string
		addPeer := func(peerID string) *spatial.PeerMemoryContext {
			if peer := peerByID[peerID]; peer != nil {
				return peer
			}
			peer := &spatial.PeerMemoryContext{PeerID: peerID, PeerName: ctxMgr.GetCharacterName(peerID)}
			peerByID[peerID] = peer
			peerIDs = append(peerIDs, peerID)
			return peer
		}
		relationships, err := memoryStore.GetRecentRelationships(10)
		if err != nil {
			log.Printf("[Memory] Failed to load relationship summaries: %v", err)
		} else {
			for _, relationship := range relationships {
				peer := addPeer(relationship.PeerID)
				peer.Summary = relationship.Summary
				peer.Commitments = relationship.Commitments
			}
		}
		knownIDs := ctxMgr.KnownCharacterIDs()
		sort.Strings(knownIDs)
		for _, peerID := range knownIDs {
			addPeer(peerID)
		}
		for _, peerID := range peerIDs {
			peer := peerByID[peerID]
			reflections, err := memoryStore.GetReflectionsForPeer(peerID, cfg.Memory.MaxReflectionsPerPeer)
			if err != nil {
				log.Printf("[Memory] Failed to load reflections for %s: %v", peerID, err)
			} else if len(reflections) > 0 {
				var texts []string
				for _, r := range reflections {
					texts = append(texts, r.ReflectionText)
				}
				peer.Reflections = texts
			}
		}
		peerMemories := make([]spatial.PeerMemoryContext, 0, len(peerIDs))
		for _, peerID := range peerIDs {
			peerMemories = append(peerMemories, *peerByID[peerID])
		}
		return peerMemories
	}

	ctxMgr.SetOnLocationObserved(func(locID, locName, cat, text string) {
		if err := memoryStore.UpsertLocationMemory(locID, locName, cat, text, ctxMgr.CurrentTick()); err != nil {
			log.Printf("[Memory] Failed to save %s observation for %s: %v", cat, locID, err)
		}
	})

	fetchLocationMemories := func() []spatial.LocationMemoryContext {
		locationSet := map[string]struct{}{ctxMgr.CurrentLocation(): {}}
		projects, err := memoryStore.GetProjects(true, 10)
		if err != nil {
			log.Printf("[Memory] Failed to load project locations: %v", err)
		} else {
			for _, project := range projects {
				for _, locationID := range project.Locations {
					locationSet[locationID] = struct{}{}
				}
			}
		}
		locationIDs := make([]string, 0, len(locationSet))
		for locationID := range locationSet {
			if locationID != "" {
				locationIDs = append(locationIDs, locationID)
			}
		}
		sort.Strings(locationIDs)
		mems, err := memoryStore.GetLocationMemoriesForLocations(locationIDs, 20)
		if err != nil {
			log.Printf("[Memory] Failed to load location observations: %v", err)
			return nil
		}
		if len(mems) == 0 {
			return nil
		}
		var res []spatial.LocationMemoryContext
		for _, lm := range mems {
			res = append(res, spatial.LocationMemoryContext{
				LocationID:     lm.LocationID,
				LocationName:   lm.LocationName,
				MemoryCategory: lm.MemoryCategory,
				MemoryText:     lm.MemoryText,
			})
		}
		return res
	}

	tracker := dispatcher.NewCorrelationTracker(50)
	defer tracker.Close()

	clientID := cfg.Client.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("client_%s_%d", cfg.Client.AgentID, time.Now().UnixNano())
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Client.BrokerURL)
	opts.SetClientID(clientID)
	opts.SetUsername(cfg.Client.AgentID)
	opts.SetPassword(cfg.Client.Password)
	if cfg.Client.KeepAliveSeconds > 0 {
		opts.SetKeepAlive(time.Duration(cfg.Client.KeepAliveSeconds) * time.Second)
	}

	idleTicks := 0
	var intentionRunning atomic.Bool
	var activeMQTTClient mqtt.Client

	subscribeLocation := func(c mqtt.Client, loc string) {
		chatTopic := fmt.Sprintf("world/locations/%s/chat", loc)
		sysTopic := fmt.Sprintf("world/locations/%s/system", loc)
		whisperTopic := fmt.Sprintf("world/locations/%s/whisper/%s", loc, cfg.Client.AgentID)

		c.Subscribe(chatTopic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			idleTicks = 0
			log.Printf("[LOCATION CHAT (%s)] %s", msg.Topic(), string(msg.Payload()))
			var chatPayload struct {
				AgentID string `json:"agent_id"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(msg.Payload(), &chatPayload); err == nil && chatPayload.AgentID != "" {
				ctxMgr.IngestChat(chatPayload.AgentID, chatPayload.Message)
				if err := memoryStore.AddEvent(ctxMgr.CurrentTick(), chatPayload.AgentID, "CHAT", string(msg.Payload())); err != nil {
					log.Printf("[Memory] Failed to record chat event: %v", err)
				}

				if chatPayload.AgentID != cfg.Client.AgentID && engine != nil {
					go func() {
						textLower := strings.ToLower(chatPayload.Message)
						agentIDLower := strings.ToLower(cfg.Client.AgentID)
						agentNameLower := strings.ToLower(ctxMgr.AgentName())

						isDirectMention := strings.Contains(textLower, agentIDLower) || strings.Contains(textLower, agentNameLower)

						shouldRespond := false
						if isDirectMention {
							log.Printf("[Autonomous Chat] Direct mention detected for %s. Forcing response turn.", cfg.Client.AgentID)
							shouldRespond = true
						} else {
							pop := ctxMgr.RoomPopulation()
							if pop <= 0 {
								pop = 1
							}
							scaledProb := cfg.Cognition.ChatResponseProbability / float64(pop)
							roll := float64(time.Now().UnixNano()%1000) / 1000.0
							if roll < scaledProb {
								log.Printf("[Autonomous Chat] Chat response probability triggered (roll %.3f < scaled prob %.3f, base %.2f, pop %d)", roll, scaledProb, cfg.Cognition.ChatResponseProbability, pop)
								shouldRespond = true
							}
						}

						if shouldRespond {
							memories := fetchPeerMemories()
							prompt := ctxMgr.BuildPrompt(memories, fetchLocationMemories())

							temp := cfg.Model.LlamaPure.Temperature
							if temp <= 0 {
								temp = 0.7
							}
							genOpts := model.GenerationOptions{
								Temperature: float32(temp),
							}
							intent, err := dispatcher.GenerateIntentWithRetry(clientCtx, engine, prompt, genOpts, 2)
							if err != nil {
								log.Printf("[Autonomous Chat] Cognition generation failed: %v", err)
								return
							}
							if handled, err := beginTravelIntention(intent, cfg.Client.AgentID, ctxMgr, memoryStore); handled {
								if err != nil {
									log.Printf("[Autonomous Chat] Failed to start travel intention: %v", err)
								}
								return
							}

							disp := dispatcher.NewIntentDispatcher(cfg.Client.AgentID, c, tracker)
							res, err := disp.Dispatch(clientCtx, intent, 10*time.Second)
							if err != nil {
								log.Printf("[Autonomous Chat] Intent dispatch failed: %v", err)
								return
							}
							idleTicks = 0
							log.Printf("[Autonomous Chat] Triggered response processed -> Action: %s (Thought: %s) [%s]", intent.Action, intent.Thought, res.Message)
						}
					}()
				}
			} else {
				ctxMgr.IngestChat("Unknown", string(msg.Payload()))
			}
		})

		c.Subscribe(sysTopic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			log.Printf("[LOCATION SYS (%s)] %s", msg.Topic(), string(msg.Payload()))
			ctxMgr.IngestSystem(string(msg.Payload()))
		})

		c.Subscribe(whisperTopic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			idleTicks = 0
			log.Printf("[WHISPER (%s)] %s", msg.Topic(), string(msg.Payload()))
			var whisperPayload struct {
				AgentID string `json:"agent_id"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(msg.Payload(), &whisperPayload); err == nil && whisperPayload.AgentID != "" {
				ctxMgr.IngestWhisper(whisperPayload.AgentID, whisperPayload.Message)
				if err := memoryStore.AddEvent(ctxMgr.CurrentTick(), whisperPayload.AgentID, "WHISPER", string(msg.Payload())); err != nil {
					log.Printf("[Memory] Failed to record whisper event: %v", err)
				}
			} else {
				ctxMgr.IngestWhisper("Unknown", string(msg.Payload()))
			}
		})

		log.Printf("[Spatial] Subscribed to location topics for %s", loc)
	}

	unsubscribeLocation := func(c mqtt.Client, loc string) {
		chatTopic := fmt.Sprintf("world/locations/%s/chat", loc)
		sysTopic := fmt.Sprintf("world/locations/%s/system", loc)
		whisperTopic := fmt.Sprintf("world/locations/%s/whisper/%s", loc, cfg.Client.AgentID)

		c.Unsubscribe(chatTopic, sysTopic, whisperTopic)
		log.Printf("[Spatial] Unsubscribed from location topics for %s", loc)
	}

	ctxMgr.SetOnLocationChange(func(oldLoc, newLoc string) {
		if activeMQTTClient != nil && activeMQTTClient.IsConnected() {
			unsubscribeLocation(activeMQTTClient, oldLoc)
			subscribeLocation(activeMQTTClient, newLoc)
		}
	})

	opts.OnConnect = func(c mqtt.Client) {
		activeMQTTClient = c
		log.Printf("[OurVillage] Connected to MQTT broker %s as %s (client: %s, engine: %s)",
			cfg.Client.BrokerURL, cfg.Client.AgentID, clientID, cfg.Model.Provider)

		// Re-subscribe to global time
		c.Subscribe("world/global/time", 0, func(_ mqtt.Client, msg mqtt.Message) {
			log.Printf("[TIME] %s", string(msg.Payload()))
			var tick spatial.TimeTickMessage
			if err := json.Unmarshal(msg.Payload(), &tick); err == nil {
				ctxMgr.IngestTimeTick(tick)
			}

			if !ctxMgr.IsLocationSynced() {
				return
			}

			if ctxMgr.Intention() != nil {
				idleTicks = 0
				intention := ctxMgr.Intention()
				status := ctxMgr.Status()
				canAdvance := strings.EqualFold(status, "IDLE") ||
					(intention != nil && intention.Kind == routine.HomeAndRest &&
						intention.Phase == routine.StartingRest && strings.EqualFold(status, "RESTING"))
				if canAdvance && intentionRunning.CompareAndSwap(false, true) {
					go func() {
						defer intentionRunning.Store(false)
						disp := dispatcher.NewIntentDispatcher(cfg.Client.AgentID, c, tracker)
						if _, err := processActiveIntention(clientCtx, cfg.Client.AgentID, ctxMgr, memoryStore, disp); err != nil {
							log.Printf("[Intention] %v", err)
						}
					}()
				}
				return
			}

			if cfg.Cognition.IdleThresholdTicks > 0 {
				status := ctxMgr.Status()
				if !strings.EqualFold(status, "IDLE") {
					idleTicks = 0
				} else {
					idleTicks++
					if idleTicks >= cfg.Cognition.IdleThresholdTicks {
						idleTicks = 0
						log.Printf("[Intrinsic Motivation] Idle threshold reached (%d ticks). Triggering autonomous motivation turn...", cfg.Cognition.IdleThresholdTicks)

						go func() {
							if !strings.EqualFold(ctxMgr.Status(), "IDLE") {
								return
							}

							memories := fetchPeerMemories()
							prompt := ctxMgr.BuildPrompt(memories, fetchLocationMemories())

							temp := cfg.Model.LlamaPure.Temperature
							if temp <= 0 {
								temp = 0.7
							}
							genOpts := model.GenerationOptions{
								Temperature: float32(temp),
							}
							intent, err := dispatcher.GenerateIntentWithRetry(clientCtx, engine, prompt, genOpts, 2)
							if err != nil {
								log.Printf("[Intrinsic Motivation] Cognition generation failed: %v", err)
								return
							}

							if !strings.EqualFold(ctxMgr.Status(), "IDLE") {
								return
							}
							if handled, err := beginTravelIntention(intent, cfg.Client.AgentID, ctxMgr, memoryStore); handled {
								if err != nil {
									log.Printf("[Intrinsic Motivation] Failed to start travel intention: %v", err)
								}
								return
							}

							disp := dispatcher.NewIntentDispatcher(cfg.Client.AgentID, c, tracker)
							res, err := disp.Dispatch(clientCtx, intent, 10*time.Second)
							if err != nil {
								log.Printf("[Intrinsic Motivation] Intent dispatch failed: %v", err)
								return
							}
							idleTicks = 0
							log.Printf("[Intrinsic Motivation] Action: %s (Thought: %s) [%s]", intent.Action, intent.Thought, res.Message)
						}()
					}
				}
			}
		})

		// Re-subscribe to agent direct messages
		directTopic := fmt.Sprintf("world/agents/%s/direct", cfg.Client.AgentID)
		c.Subscribe(directTopic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			idleTicks = 0
			log.Printf("[DIRECT RESPONSE] %s", string(msg.Payload()))
			_ = ctxMgr.IngestDirectResponse(msg.Payload())
			res, matched := tracker.Resolve(msg.Payload())
			if matched {
				log.Printf("[Correlation] Intent %s acknowledged by server: %s in %v", res.IntentID, res.Status, res.Duration)
				if err := memoryStore.AddEvent(ctxMgr.CurrentTick(), cfg.Client.AgentID, "ACTION_RESULT", string(msg.Payload())); err != nil {
					log.Printf("[Memory] Failed to record action outcome: %v", err)
				}
			}
		})

		// Re-subscribe to current location topics on reconnect
		currentLoc := ctxMgr.CurrentLocation()
		if currentLoc != "" {
			subscribeLocation(c, currentLoc)
		}

		// Dispatch initial LOOK intent immediately to sync true character location from database
		go func() {
			log.Printf("[Startup] Dispatching initial LOOK to query true character location from database...")
			disp := dispatcher.NewIntentDispatcher(cfg.Client.AgentID, c, tracker)
			lookIntent := dispatcher.Intent{
				IntentID: fmt.Sprintf("req_startup_look_%d", time.Now().UnixNano()),
				Action:   "LOOK",
				Payload:  json.RawMessage(`{}`),
			}
			_, _ = disp.Dispatch(clientCtx, lookIntent, 5*time.Second)
		}()

		if cfg.Action.Action != "" {
			go func() {
				select {
				case <-clientCtx.Done():
					return
				case <-time.After(1000 * time.Millisecond):
				}

				prompt := ctxMgr.BuildPrompt(fetchPeerMemories(), fetchLocationMemories())
				log.Printf("[Cognition Prompt Ready] Context window assembled (%d bytes)", len(prompt))

				disp := dispatcher.NewIntentDispatcher(cfg.Client.AgentID, c, tracker)
				intentBytes, err := BuildIntentMessage(cfg.Action)
				if err != nil {
					log.Printf("[ERROR] Failed to build intent: %v", err)
					return
				}

				var rawIntent dispatcher.Intent
				if err := json.Unmarshal(intentBytes, &rawIntent); err != nil {
					log.Printf("[ERROR] Failed to unmarshal built intent: %v", err)
					return
				}

				log.Printf("[INTENT] Dispatching %s intent (%s)...", rawIntent.Action, rawIntent.IntentID)
				res, err := disp.Dispatch(clientCtx, rawIntent, 10*time.Second)
				if err != nil {
					log.Printf("[INTENT RESULT ERROR] %s failed: %v", rawIntent.Action, err)
				} else {
					log.Printf("[INTENT RESULT SUCCESS] %s (%s): %s (%v)", res.IntentID, res.Status, res.Message, res.Duration)
				}
			}()
		}
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker at %s: %w", cfg.Client.BrokerURL, token.Error())
	}

	if cfg.Client.TUI {
		chatProb := &cfg.Cognition.ChatResponseProbability
		refFreq := &cfg.Cognition.ReflectionIntervalTicks
		temp := &cfg.Model.LlamaPure.Temperature
		debounce := &cfg.Cognition.TickIntervalDebounceMS

		tuiModel := tui.NewTUIModel(clientCtx, cfg.Client.AgentID, client, tracker, ctxMgr, engine, fetchPeerMemories, chatProb, refFreq, temp, debounce)
		tuiModel.SetTravelIntentionSaver(func(intention routine.Intention) error {
			if err := memoryStore.SaveActiveIntention(cfg.Client.AgentID, intention); err != nil {
				return err
			}
			ctxMgr.SetIntention(&intention)
			return nil
		})
		tuiModel.SetSynthesisStatusChannel(synthesisStatuses)
		tuiModel.SetSelfIdentityProvider(func() ([]memory.IdentityTrait, []memory.SelfReflection, error) {
			traits, err := memoryStore.GetIdentityTraits()
			if err != nil {
				return nil, nil, fmt.Errorf("load identity traits: %w", err)
			}
			reflections, err := memoryStore.GetRecentSelfReflections(10)
			if err != nil {
				return nil, nil, fmt.Errorf("load self-reflections: %w", err)
			}
			return traits, reflections, nil
		})
		tuiModel.SetProjectProvider(func() ([]memory.PersonalProject, error) {
			return memoryStore.GetProjects(false, 20)
		})
		tuiModel.SetLocationMemoryProvider(func() ([]memory.LocationMemory, error) {
			return memoryStore.GetLocationMemories()
		})
		p := tea.NewProgram(tuiModel, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Printf("[TUI Error] %v", err)
		}
	} else {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		select {
		case <-clientCtx.Done():
			log.Println("[OurVillage] Context cancelled, shutting down...")
		case sig := <-sigChan:
			log.Printf("[OurVillage] Received signal %v, shutting down...", sig)
		}
	}

	log.Println("[OurVillage] Disconnecting client...")
	client.Disconnect(250)
	return nil
}
