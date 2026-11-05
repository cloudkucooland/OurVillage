package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"agentmud/internal/db"
	"agentmud/internal/server/admin"
	"agentmud/internal/server/tui"

	tea "github.com/charmbracelet/bubbletea"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
)

type Server struct {
	mqttServer *mqtt.Server
	db         *sql.DB
	authHook   *DBAuthHook
	mu         sync.RWMutex
	tickCount  uint64
	weather    string
}

type TickPayload struct {
	Tick       uint64    `json:"tick"`
	Timestamp  time.Time `json:"timestamp"`
	InGameTime string    `json:"in_game_time"`
	IsNight    bool      `json:"is_night"`
	Weather    string    `json:"weather"`
}

func NewServer(database *sql.DB, listenAddr ...string) (*Server, error) {
	mqttServer := mqtt.New(&mqtt.Options{
		InlineClient: true,
	})

	authHook := NewDBAuthHook(database)
	// Add database authentication and ACL hook
	err := mqttServer.AddHook(authHook, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to add db auth hook: %w", err)
	}

	address := ":1883"
	if envAddr := os.Getenv("AGENTMUD_MQTT_ADDR"); envAddr != "" {
		address = envAddr
	}
	if len(listenAddr) > 0 {
		address = listenAddr[0]
	}

	if address != "" {
		// Create a TCP listener on specified port
		tcp := listeners.NewTCP(listeners.Config{
			ID:      "t1",
			Address: address,
		})
		err = mqttServer.AddListener(tcp)
		if err != nil {
			return nil, fmt.Errorf("failed to add mqtt tcp listener: %w", err)
		}
	}

	srv := &Server{
		mqttServer: mqttServer,
		db:         database,
		authHook:   authHook,
		weather:    "clear",
	}

	// Reset any stuck character statuses and online flags on server startup
	_, _ = database.Exec("UPDATE characters SET status = 'IDLE', action_target = '', action_start_tick = 0, action_end_tick = 0, action_intent_id = '', is_online = 0")

	if err := srv.SetupIntentHandlers(); err != nil {
		return nil, fmt.Errorf("failed to setup intent handlers: %w", err)
	}

	return srv, nil
}

func (s *Server) Start() error {
	// Start MQTT server in background
	go func() {
		log.Println("Starting Mochi-MQTT broker on :1883...")
		err := s.mqttServer.Serve()
		if err != nil {
			log.Printf("MQTT server stopped: %v", err)
		}
	}()

	// Start Master Clock & Tick Engine (RFC 001)
	go s.runClock()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down AgentMUD server...")
	return s.mqttServer.Close()
}

func (s *Server) StartTUI() error {
	logPath := os.Getenv("AGENTMUD_LOG")
	if logPath == "" {
		logPath = "agentmud-server.log"
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open server log file %q: %w", logPath, err)
	}
	defer logFile.Close()
	previousLogOutput := log.Writer()
	defer log.SetOutput(previousLogOutput)

	logChannel := tui.NewLogChannel()
	log.SetOutput(io.MultiWriter(logFile, tui.NewLogWriter(logChannel)))

	tuiModel := tui.NewModel(s.db, admin.NewAdminCommandExecutor(s.db, s.SetWeather), s)
	tuiModel.SetLogChannel(logChannel)

	// Start MQTT server in background
	go func() {
		err := s.mqttServer.Serve()
		if err != nil {
			log.Printf("MQTT server stopped: %v", err)
		}
	}()

	// Start Master Clock & Tick Engine
	go s.runClock()

	p := tea.NewProgram(tuiModel, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Printf("[TUI Error] %v", err)
	}

	log.Println("Shutting down AgentMUD server...")
	return s.mqttServer.Close()
}

func (s *Server) GetTickCount() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tickCount
}

func (s *Server) SetTickCount(t uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tickCount = t
}

func (s *Server) GetWeather() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.weather == "" {
		return "clear"
	}
	return s.weather
}

func (s *Server) SetWeather(w string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.weather = w
}

func (s *Server) runClock() {
	interval := 90 * time.Second // default: 90 seconds per tick to give LLM agents time to process
	if envInterval := os.Getenv("AGENTMUD_TICK_INTERVAL"); envInterval != "" {
		if parsed, err := time.ParseDuration(envInterval); err == nil && parsed > 0 {
			interval = parsed
		}
	}
	log.Printf("[Clock] Starting server master clock with tick interval: %v", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		s.ProcessTick()
	}
}

// ProcessTick executes a single server clock cycle, resolving actions and broadcasting time.
func (s *Server) ProcessTick() {
	s.mu.Lock()
	s.tickCount++
	currentTick := s.tickCount
	weather := s.weather
	s.mu.Unlock()

	now := time.Now()

	// 1 tick = 15 minutes of in-game time (96 ticks = 24 hours / 1 full in-game day)
	totalMinutes := currentTick * 15
	hour := (totalMinutes / 60) % 24
	minute := totalMinutes % 60

	timeStr := fmt.Sprintf("%02d:%02d", hour, minute)
	isNight := hour < 6 || hour >= 20

	// 1. Replenish dynamic ecological resource pools hourly (every 4 ticks)
	if currentTick%4 == 0 {
		_ = db.ReplenishLocationResources(s.db, weather)
	}

	// 2. Resolve temporal action locks (RFC 007 Section 6: Stateful Temporal Foraging Engine)
	s.resolveCompletedActions(currentTick, weather)

	// 3. Publish Master Time broadcast
	payload := TickPayload{
		Tick:       currentTick,
		Timestamp:  now,
		InGameTime: timeStr,
		IsNight:    isNight,
		Weather:    weather,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal tick payload: %v", err)
		return
	}

	// Publish to world/global/time
	err = s.mqttServer.Publish("world/global/time", data, false, 0)
	if err != nil {
		log.Printf("Failed to publish tick: %v", err)
	} else {
		log.Printf("[Tick %d] Time: %s, Night: %v, Weather: %s", currentTick, timeStr, isNight, weather)
	}
}

func (s *Server) resolveCompletedActions(currentTick uint64, weather string) {
	rows, err := s.db.Query(`
		SELECT id, status, current_location, action_target, action_start_tick, action_end_tick, action_intent_id
		FROM characters
		WHERE status != 'IDLE' AND action_end_tick <= ?
	`, currentTick)
	if err != nil {
		log.Printf("Failed to query characters with completed actions: %v", err)
		return
	}
	defer rows.Close()

	type completedAction struct {
		charID    string
		status    string
		locID     string
		target    string
		startTick uint64
		endTick   uint64
		intentID  string
	}

	var completed []completedAction
	for rows.Next() {
		var a completedAction
		if err := rows.Scan(&a.charID, &a.status, &a.locID, &a.target, &a.startTick, &a.endTick, &a.intentID); err != nil {
			log.Printf("Failed to scan completed action: %v", err)
			continue
		}
		completed = append(completed, a)
	}

	for _, a := range completed {
		switch a.status {
		case "FORAGING":
			ticksSpent := int(a.endTick - a.startTick)
			if ticksSpent <= 0 {
				ticksSpent = 1
			}

			res, err := db.ResolveForaging(s.db, a.charID, a.locID, a.target, ticksSpent, weather, false)
			if err != nil {
				log.Printf("Failed to resolve foraging for %s: %v", a.charID, err)
				continue
			}

			// Broadcast room system event
			sysTopic := fmt.Sprintf("world/locations/%s/system", a.locID)
			sysMsg := fmt.Sprintf("%s has finished foraging for %s.", a.charID, a.target)
			_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

			// Broadcast direct agent response on completion (RFC 007 Section 10.5)
			respMap := map[string]interface{}{
				"intent_id":       a.intentID,
				"status":          "SUCCESS",
				"ticks_spent":     res.TicksSpent,
				"energy_deducted": res.EnergyDeducted,
				"skill_xp_gained": res.SkillXPGained,
				"yield":           res.Yield,
				"total_weight_kg": res.TotalWeightKg,
				"max_capacity_kg": res.MaxCapacityKg,
				"narrative_log":   res.NarrativeLog,
			}

			if respBytes, err := json.Marshal(respMap); err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}

		case "CRAFTING":
			res, err := db.ResolveCrafting(s.db, a.charID, a.target)
			if err != nil {
				log.Printf("Failed to resolve crafting for %s: %v", a.charID, err)
				continue
			}

			// Broadcast room system event
			sysTopic := fmt.Sprintf("world/locations/%s/system", a.locID)
			sysMsg := fmt.Sprintf("%s has finished crafting %s.", a.charID, res.RecipeName)
			_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

			// Broadcast direct agent response on completion (RFC 008)
			respMap := map[string]interface{}{
				"intent_id":       a.intentID,
				"status":          "SUCCESS",
				"action":          "CRAFT",
				"recipe_id":       res.RecipeID,
				"ticks_spent":     res.TicksSpent,
				"energy_deducted": res.EnergyDeducted,
				"skill_xp_gained": res.SkillXPGained,
				"yield":           res.Yield,
				"total_weight_kg": res.TotalWeightKg,
				"max_capacity_kg": res.MaxCapacityKg,
				"narrative_log":   res.NarrativeLog,
			}

			if respBytes, err := json.Marshal(respMap); err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}

		case "RESTING":
			_, err = s.db.Exec("UPDATE characters SET energy = MIN(100, energy + 50) WHERE id = ?", a.charID)
			if err != nil {
				log.Printf("Failed to resolve resting energy for %s: %v", a.charID, err)
				continue
			}

			var newEnergy int
			_ = s.db.QueryRow("SELECT energy FROM characters WHERE id = ?", a.charID).Scan(&newEnergy)

			sysTopic := fmt.Sprintf("world/locations/%s/system", a.locID)
			sysMsg := fmt.Sprintf("%s has finished resting and feels refreshed.", a.charID)
			_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

			respMap := map[string]interface{}{
				"intent_id":     a.intentID,
				"status":        "SUCCESS",
				"action":        "REST",
				"energy":        newEnergy,
				"narrative_log": fmt.Sprintf("You finished resting and recovered energy (Energy: %d/100).", newEnergy),
			}

			if respBytes, err := json.Marshal(respMap); err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}
		}

		// Reset character state back to IDLE in SQLite so actions aren't re-executed
		_, err = s.db.Exec(`
			UPDATE characters 
			SET status = 'IDLE', action_target = '', action_start_tick = 0, action_end_tick = 0, action_intent_id = '' 
			WHERE id = ?`,
			a.charID,
		)
		if err != nil {
			log.Printf("Failed to clear action state for character %s: %v", a.charID, err)
		}
	}
}
