package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/memory"
	"agentmud/internal/client/model"
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
	case "CHAT":
		payload, err = json.Marshal(actionCfg.ChatMsg)
	case "TRANSFER":
		payload, err = json.Marshal(map[string]interface{}{
			"recipient_id": actionCfg.Recipient,
			"amount":       actionCfg.Amount,
			"item_id":      actionCfg.Item,
			"quantity":     actionCfg.Qty,
		})
	case "FORAGE":
		payload, err = json.Marshal(map[string]interface{}{
			"target_resource": actionCfg.Resource,
			"duration_ticks":  actionCfg.Duration,
		})
	case "CANCEL_ACTION":
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
	case "UNEQUIP":
		payload, err = json.Marshal(map[string]interface{}{})
	case "CRAFT":
		payload, err = json.Marshal(map[string]interface{}{
			"recipe_id": actionCfg.Recipe,
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

// RunClient initializes the MQTT client and event loops according to the provided configuration.
func RunClient(ctx context.Context, cfg *Config) error {
	clientCtx, cancelClient := context.WithCancel(ctx)
	defer cancelClient()

	logPath := cfg.Client.LogFile
	if logPath == "" && cfg.Client.TUI {
		logPath = "agentmud-client.log"
	}
	if logPath != "" {
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
	defer engine.Close()

	initLoc := cfg.Action.TargetLoc
	if initLoc == "" {
		initLoc = "town_square"
	}
	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{
		AgentID:          cfg.Client.AgentID,
		InitialLocation:  initLoc,
		MaxSensoryLines:  cfg.Cognition.MaxSensoryLogLines,
		DefaultGold:      15,
		DefaultEnergy:    100,
		DefaultInventory: []string{"bread", "water_flask"},
	})

	memoryStore, err := memory.NewClientMemoryStore(cfg.Memory.DBPath)
	if err != nil {
		return fmt.Errorf("failed to initialize client memory store: %w", err)
	}
	defer memoryStore.Close()

	synthesizer := memory.NewReflectionSynthesizer(
		memoryStore,
		engine,
		cfg.Client.AgentID,
		time.Duration(cfg.Cognition.ReflectionIntervalTicks)*time.Second,
	)
	synthesizer.Start(clientCtx)

	fetchPeerMemories := func() []spatial.PeerMemoryContext {
		traits, err := memoryStore.GetIdentityTraits()
		if err == nil && len(traits) > 0 {
			tMap := make(map[string]string)
			for _, t := range traits {
				tMap[t.Key] = t.Value
			}
			ctxMgr.SetIdentityTraits(tMap)
		}
		selfRefs, err := memoryStore.GetRecentSelfReflections(3)
		if err == nil && len(selfRefs) > 0 {
			var refTexts []string
			for _, r := range selfRefs {
				refTexts = append(refTexts, r.ReflectionText)
			}
			ctxMgr.SetSelfReflections(refTexts)
		}

		var peerMemories []spatial.PeerMemoryContext
		for _, peerID := range ctxMgr.KnownCharacterIDs() {
			reflections, err := memoryStore.GetReflectionsForPeer(peerID, cfg.Memory.MaxReflectionsPerPeer)
			if err == nil && len(reflections) > 0 {
				var texts []string
				for _, r := range reflections {
					texts = append(texts, r.ReflectionText)
				}
				peerMemories = append(peerMemories, spatial.PeerMemoryContext{
					PeerID:      peerID,
					PeerName:    ctxMgr.GetCharacterName(peerID),
					Reflections: texts,
				})
			}
		}
		return peerMemories
	}

	ctxMgr.SetOnLocationObserved(func(locID, locName, cat, text string) {
		_ = memoryStore.UpsertLocationMemory(locID, locName, cat, text, 0)
	})

	fetchLocationMemories := func() []spatial.LocationMemoryContext {
		mems, err := memoryStore.GetLocationMemories()
		if err != nil || len(mems) == 0 {
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
				_ = memoryStore.AddEvent(0, chatPayload.AgentID, "CHAT", string(msg.Payload()))

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
				_ = memoryStore.AddEvent(0, whisperPayload.AgentID, "WHISPER", string(msg.Payload()))
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
		log.Printf("[AgentMUD] Connected to MQTT broker %s as %s (client: %s, engine: %s)",
			cfg.Client.BrokerURL, cfg.Client.AgentID, clientID, cfg.Model.Provider)

		// Re-subscribe to global time
		c.Subscribe("world/global/time", 0, func(_ mqtt.Client, msg mqtt.Message) {
			log.Printf("[TIME] %s", string(msg.Payload()))
			var tick spatial.TimeTickMessage
			if err := json.Unmarshal(msg.Payload(), &tick); err == nil {
				ctxMgr.IngestTimeTick(tick)
			}

			if cfg.Cognition.IdleThresholdTicks > 0 {
				idleTicks++
				if idleTicks >= cfg.Cognition.IdleThresholdTicks {
					idleTicks = 0
					log.Printf("[Intrinsic Motivation] Idle threshold reached (%d ticks). Triggering autonomous motivation turn...", cfg.Cognition.IdleThresholdTicks)

					go func() {
						ctxMgr.IngestSystem("You feel a quiet moment of intrinsic curiosity and urge to act. What shall you do? Perhaps you should explore the village, check your inventory, forage for resources, or visit the tavern.")

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
		})

		// Re-subscribe to agent direct messages
		directTopic := fmt.Sprintf("world/agents/%s/direct", cfg.Client.AgentID)
		c.Subscribe(directTopic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			idleTicks = 0
			log.Printf("[DIRECT RESPONSE] %s", string(msg.Payload()))
			res, matched := tracker.Resolve(msg.Payload())
			if matched {
				log.Printf("[Correlation] Intent %s acknowledged by server: %s in %v", res.IntentID, res.Status, res.Duration)
				_ = memoryStore.AddEvent(0, "SERVER", "DIRECT_RESPONSE", string(msg.Payload()))
			}
			_ = ctxMgr.IngestDirectResponse(msg.Payload())
		})

		// Re-subscribe to current location topics on reconnect
		currentLoc := ctxMgr.CurrentLocation()
		if currentLoc != "" {
			subscribeLocation(c, currentLoc)
		}

		// Dispatch initial LOOK intent to sync true character location from server
		go func() {
			select {
			case <-clientCtx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}

			log.Printf("[Startup] Dispatching initial LOOK to query true character location...")
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
		p := tea.NewProgram(tuiModel, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Printf("[TUI Error] %v", err)
		}
	} else {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		select {
		case <-clientCtx.Done():
			log.Println("[AgentMUD] Context cancelled, shutting down...")
		case sig := <-sigChan:
			log.Printf("[AgentMUD] Received signal %v, shutting down...", sig)
		}
	}

	log.Println("[AgentMUD] Disconnecting client...")
	client.Disconnect(250)
	return nil
}
