package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/spatial"
	"agentmud/internal/client/tui"

	tea "github.com/charmbracelet/bubbletea"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// RunPlayer connects a human-controlled character to the world and starts its TUI.
func RunPlayer(ctx context.Context, cfg *Config) error {
	if err := playerConfigError(cfg); err != nil {
		return fmt.Errorf("invalid player configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{
		AgentID:         cfg.Client.AgentID,
		InitialLocation: "town_square",
		MaxSensoryLines: cfg.Cognition.MaxSensoryLogLines,
	})
	tracker := dispatcher.NewCorrelationTracker(50)
	defer tracker.Close()

	clientID := cfg.Client.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("player_%s_%d", cfg.Client.AgentID, time.Now().UnixNano())
	}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Client.BrokerURL).
		SetClientID(clientID).
		SetUsername(cfg.Client.AgentID).
		SetPassword(cfg.Client.Password)
	if cfg.Client.KeepAliveSeconds > 0 {
		opts.SetKeepAlive(time.Duration(cfg.Client.KeepAliveSeconds) * time.Second)
	}

	var activeClient mqtt.Client
	var activeClientMu sync.RWMutex
	subscribeLocation := func(c mqtt.Client, locationID string) {
		c.Subscribe(fmt.Sprintf("world/locations/%s/chat", locationID), 0, func(_ mqtt.Client, msg mqtt.Message) {
			var chat struct {
				AgentID string `json:"agent_id"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(msg.Payload(), &chat); err != nil {
				ctxMgr.IngestChat("Unknown", string(msg.Payload()))
				return
			}
			ctxMgr.IngestChat(chat.AgentID, chat.Message)
		})
		c.Subscribe(fmt.Sprintf("world/locations/%s/system", locationID), 0, func(_ mqtt.Client, msg mqtt.Message) {
			ctxMgr.IngestSystem(string(msg.Payload()))
		})
		c.Subscribe(fmt.Sprintf("world/locations/%s/whisper/%s", locationID, cfg.Client.AgentID), 0, func(_ mqtt.Client, msg mqtt.Message) {
			var whisper struct {
				AgentID string `json:"agent_id"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(msg.Payload(), &whisper); err != nil {
				ctxMgr.IngestWhisper("Unknown", string(msg.Payload()))
				return
			}
			ctxMgr.IngestWhisper(whisper.AgentID, whisper.Message)
		})
	}
	unsubscribeLocation := func(c mqtt.Client, locationID string) {
		c.Unsubscribe(
			fmt.Sprintf("world/locations/%s/chat", locationID),
			fmt.Sprintf("world/locations/%s/system", locationID),
			fmt.Sprintf("world/locations/%s/whisper/%s", locationID, cfg.Client.AgentID),
		)
	}
	ctxMgr.SetOnLocationChange(func(oldLocation, newLocation string) {
		activeClientMu.RLock()
		c := activeClient
		activeClientMu.RUnlock()
		if c != nil && c.IsConnected() {
			unsubscribeLocation(c, oldLocation)
			subscribeLocation(c, newLocation)
		}
	})

	opts.OnConnect = func(c mqtt.Client) {
		activeClientMu.Lock()
		activeClient = c
		activeClientMu.Unlock()
		c.Subscribe("world/global/time", 0, func(_ mqtt.Client, msg mqtt.Message) {
			var tick spatial.TimeTickMessage
			if err := json.Unmarshal(msg.Payload(), &tick); err == nil {
				ctxMgr.IngestTimeTick(tick)
			}
		})
		c.Subscribe(fmt.Sprintf("world/agents/%s/direct", cfg.Client.AgentID), 0, func(_ mqtt.Client, msg mqtt.Message) {
			if err := ctxMgr.IngestDirectResponse(msg.Payload()); err != nil {
				return
			}
			tracker.Resolve(msg.Payload())
		})
		locationID := ctxMgr.CurrentLocation()
		if locationID != "" {
			subscribeLocation(c, locationID)
		}

		go func() {
			disp := dispatcher.NewIntentDispatcher(cfg.Client.AgentID, c, tracker)
			_, err := disp.Dispatch(ctx, dispatcher.Intent{
				IntentID: dispatcher.GenerateIntentID(),
				Action:   "LOOK",
				Payload:  json.RawMessage(`{}`),
			}, 5*time.Second)
			if err != nil && ctx.Err() == nil {
				ctxMgr.IngestSystem("Initial look failed: " + err.Error())
			}
		}()
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker at %s: %w", cfg.Client.BrokerURL, token.Error())
	}
	defer client.Disconnect(250)

	model := tui.NewPlayerModel(ctx, ctxMgr,
		dispatcher.NewIntentDispatcher(cfg.Client.AgentID, client, tracker))
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("player TUI failed: %w", err)
	}
	return nil
}

func playerConfigError(cfg *Config) error {
	if strings.TrimSpace(cfg.Client.AgentID) == "" {
		return fmt.Errorf("client.agent_id cannot be empty")
	}
	if strings.TrimSpace(cfg.Client.Password) == "" {
		return fmt.Errorf("client.password cannot be empty")
	}
	if strings.TrimSpace(cfg.Client.BrokerURL) == "" {
		return fmt.Errorf("client.broker_url cannot be empty")
	}
	return nil
}
