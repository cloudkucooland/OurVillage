package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"agentmud/internal/db"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
)

type Server struct {
	mqttServer *mqtt.Server
	db         *sql.DB
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

	// Add database authentication and ACL hook
	err := mqttServer.AddHook(NewDBAuthHook(database), nil)
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
		weather:    "clear",
	}

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
			log.Fatalf("MQTT server error: %v", err)
		}
	}()

	// Start Master Clock & Tick Engine (RFC 001)
	go s.runClock()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down AgentMUD server...")
	s.mqttServer.Close()
	return nil
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
	ticker := time.NewTicker(5 * time.Second) // 1 tick every 5 seconds for simulation
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

	// Simple in-game time calculation (1 real sec = 12 game minutes or 1 tick = 15 game minutes)
	inGameHour := (int(currentTick) * 15) % 2400
	hour := inGameHour / 100
	minute := inGameHour % 100
	if minute >= 60 {
		minute = 59
	}
	timeStr := fmt.Sprintf("%02d:%02d", hour%24, minute)
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
		if a.status == "FORAGING" {
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

			respBytes, err := json.Marshal(respMap)
			if err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}
		} else if a.status == "CRAFTING" {
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

			respBytes, err := json.Marshal(respMap)
			if err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}
		}
	}
}
