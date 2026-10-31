package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
)

type Server struct {
	mqttServer *mqtt.Server
	db         *sql.DB
	tickCount  uint64
}

type TickPayload struct {
	Tick       uint64    `json:"tick"`
	Timestamp  time.Time `json:"timestamp"`
	InGameTime string    `json:"in_game_time"`
	IsNight    bool      `json:"is_night"`
	Weather    string    `json:"weather"`
}

func NewServer(database *sql.DB) (*Server, error) {
	mqttServer := mqtt.New(nil)

	// Create a TCP listener on port 1883
	tcp := listeners.NewTCP(listeners.Config{
		ID:      "t1",
		Address: ":1883",
	})
	err := mqttServer.AddListener(tcp)
	if err != nil {
		return nil, fmt.Errorf("failed to add mqtt tcp listener: %w", err)
	}

	srv := &Server{
		mqttServer: mqttServer,
		db:         database,
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

func (s *Server) runClock() {
	ticker := time.NewTicker(5 * time.Second) // 1 tick every 5 seconds for simulation
	defer ticker.Stop()

	for range ticker.C {
		s.tickCount++
		now := time.Now()
		
		// Simple in-game time calculation (1 real sec = 12 game minutes or similar)
		inGameHour := (int(s.tickCount) * 15) % 2400
		hour := inGameHour / 100
		minute := inGameHour % 100
		if minute >= 60 {
			minute = 59
		}
		timeStr := fmt.Sprintf("%02d:%02d", hour%24, minute)
		isNight := hour < 6 || hour >= 20

		payload := TickPayload{
			Tick:       s.tickCount,
			Timestamp:  now,
			InGameTime: timeStr,
			IsNight:    isNight,
			Weather:    "clear",
		}

		data, err := json.Marshal(payload)
		if err != nil {
			log.Printf("Failed to marshal tick payload: %v", err)
			continue
		}

		// Publish to world/global/time
		err = s.mqttServer.Publish("world/global/time", data, false, 0)
		if err != nil {
			log.Printf("Failed to publish tick: %v", err)
		} else {
			log.Printf("[Tick %d] Time: %s, Night: %v", s.tickCount, timeStr, isNight)
		}
	}
}
