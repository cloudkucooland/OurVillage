package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
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
	mqttServer  *mqtt.Server
	db          *sql.DB
	authHook    *DBAuthHook
	config      *ServerConfig
	mu          sync.RWMutex
	tickCount   uint64
	weather     string
	temperature float64
}

type TickPayload struct {
	Tick        uint64    `json:"tick"`
	Timestamp   time.Time `json:"timestamp"`
	InGameTime  string    `json:"in_game_time"`
	IsNight     bool      `json:"is_night"`
	Weather     string    `json:"weather"`
	Temperature float64   `json:"temperature"`
}

const collapseRecoveryRestDurationTicks uint64 = 60

func EnsureDefaultCertificate(certPath, keyPath string) error {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil
		}
	}

	log.Printf("[Server] Generating default self-signed TLS certificate (%s, %s)...", certPath, keyPath)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "OurVillage MQTT Server",
			Organization: []string{"OurVillage"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost", "ourvillage"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open cert path for writing: %w", err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("failed to encode certificate PEM: %w", err)
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open key path for writing: %w", err)
	}
	defer keyOut.Close()
	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to encode private key PEM: %w", err)
	}

	log.Printf("[Server] Successfully generated self-signed TLS certificate.")
	return nil
}

func NewServer(database *sql.DB, listenAddr ...string) (*Server, error) {
	cfg, err := LoadConfig("")
	if err != nil {
		cfg = DefaultConfig()
	}
	return NewServerWithConfig(database, cfg, listenAddr...)
}

func NewServerWithConfig(database *sql.DB, cfg *ServerConfig, listenAddr ...string) (*Server, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	mqttServer := mqtt.New(&mqtt.Options{
		InlineClient: true,
	})

	authHook := NewDBAuthHook(database)
	// Add database authentication and ACL hook
	err := mqttServer.AddHook(authHook, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to add db auth hook: %w", err)
	}

	certPath := cfg.Server.TLSCert
	if certPath == "" {
		certPath = "server.crt"
	}
	keyPath := cfg.Server.TLSKey
	if keyPath == "" {
		keyPath = "server.key"
	}

	if err := EnsureDefaultCertificate(certPath, keyPath); err != nil {
		log.Printf("[Server] Warning: failed to ensure default TLS certificate: %v", err)
	}

	address := cfg.Server.MQTTAddr
	if envAddr := os.Getenv("AGENTMUD_MQTT_ADDR"); envAddr != "" {
		address = envAddr
	}

	tlsAddress := cfg.Server.MQTTTLSAddr
	if envTLSAddr := os.Getenv("AGENTMUD_MQTT_TLS_ADDR"); envTLSAddr != "" {
		tlsAddress = envTLSAddr
	}

	if len(listenAddr) > 0 {
		address = listenAddr[0]
		if len(listenAddr[0]) == 0 && os.Getenv("AGENTMUD_MQTT_TLS_ADDR") == "" {
			tlsAddress = ""
		}
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

	if tlsAddress != "" {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			log.Printf("[Server] Warning: failed to load TLS key pair from %s and %s: %v", certPath, keyPath, err)
		} else {
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{cert},
			}
			tlsListener := listeners.NewTCP(listeners.Config{
				ID:        "t2",
				Address:   tlsAddress,
				TLSConfig: tlsConfig,
			})
			err = mqttServer.AddListener(tlsListener)
			if err != nil {
				return nil, fmt.Errorf("failed to add mqtt tls listener: %w", err)
			}
			log.Printf("[Server] MQTT TLS listener started on %s", tlsAddress)
		}
	}

	loadedTick, err := db.GetServerTick(database)
	if err != nil {
		log.Printf("[Server] Failed to load persisted tick count: %v", err)
	}

	defWeather := cfg.Weather.DefaultWeather
	if defWeather == "" {
		defWeather = "clear"
	}
	defTemp := cfg.Weather.DefaultTemp
	if defTemp == 0 {
		defTemp = 20.0
	}

	srv := &Server{
		mqttServer:  mqttServer,
		db:          database,
		authHook:    authHook,
		config:      cfg,
		tickCount:   loadedTick,
		weather:     defWeather,
		temperature: defTemp,
	}
	if loadedTick > 0 {
		log.Printf("[Server] Resuming from persisted tick count: %d", loadedTick)
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

	return s.Shutdown()
}

func (s *Server) Shutdown() error {
	log.Println("Shutting down AgentMUD server...")
	s.mu.RLock()
	currentTick := s.tickCount
	s.mu.RUnlock()
	if err := db.SaveServerTick(s.db, currentTick); err != nil {
		log.Printf("Failed to save tick %d on shutdown: %v", currentTick, err)
	}
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

	tuiModel := tui.NewModel(s.db, admin.NewAdminCommandExecutor(s.db, s.SetWeather, s.SetTemperature), s)
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

	return s.Shutdown()
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

func (s *Server) GetTemperature() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.temperature
}

func (s *Server) SetTemperature(t float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.temperature = t
}

func (s *Server) runClock() {
	interval := 90 * time.Second
	if s.config != nil && s.config.Clock.TickInterval > 0 {
		interval = s.config.Clock.TickInterval
	}
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
	startTime := time.Now()
	s.mu.Lock()
	s.tickCount++
	currentTick := s.tickCount
	weather := s.weather
	temperature := s.temperature
	s.mu.Unlock()

	// Persist current tick to database
	if err := db.SaveServerTick(s.db, currentTick); err != nil {
		log.Printf("Failed to persist tick %d: %v", currentTick, err)
	}

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

	// 1.4. Process Spoilage & Perishability Ticks (RFC 009)
	if err := db.ProcessSpoilageTicks(s.db, currentTick); err != nil {
		log.Printf("Failed to process spoilage ticks: %v", err)
	}

	hungerRate := 1
	energyRate := 1
	starvationRate := 1
	weatherRate := 1
	minTemp := 10.0
	maxTemp := 30.0

	if s.config != nil {
		if s.config.Decay.HungerRate > 0 {
			hungerRate = s.config.Decay.HungerRate
		}
		if s.config.Decay.EnergyRate > 0 {
			energyRate = s.config.Decay.EnergyRate
		}
		if s.config.Decay.StarvationHealthRate > 0 {
			starvationRate = s.config.Decay.StarvationHealthRate
		}
		if s.config.Decay.WeatherHealthRate > 0 {
			weatherRate = s.config.Decay.WeatherHealthRate
		}
		if s.config.Weather.MinSafeTemp != 0 || s.config.Weather.MaxSafeTemp != 0 {
			minTemp = s.config.Weather.MinSafeTemp
			maxTemp = s.config.Weather.MaxSafeTemp
		}
	}

	// 1.5. Apply passive hunger and energy decay per tick for idle online characters
	_, _ = s.db.Exec(fmt.Sprintf(`
		UPDATE characters 
		SET hunger = MAX(0, hunger - %d)
		WHERE status = 'IDLE' AND is_online = 1
	`, hungerRate))

	_, _ = s.db.Exec(fmt.Sprintf(`
		UPDATE characters 
		SET energy = MAX(0, energy - %d)
		WHERE status = 'IDLE' AND is_online = 1 AND current_location != home_location_id
	`, energyRate))

	// Online characters at zero hunger lose health each tick, regardless of action status.
	_, _ = s.db.Exec(fmt.Sprintf(`
		UPDATE characters
		SET health = MAX(0, health - %d)
		WHERE hunger <= 0 AND is_online = 1
	`, starvationRate))

	// 1.6. Apply environmental temperature health decay if too cold (< minTemp) or too hot (> maxTemp) for online characters outdoors
	if temperature < minTemp || temperature > maxTemp {
		_, _ = s.db.Exec(fmt.Sprintf(`
			UPDATE characters 
			SET health = MAX(0, health - %d)
			WHERE current_location IN (SELECT id FROM locations WHERE is_indoor = 0) AND is_online = 1
		`, weatherRate))
	}

	s.forceRestForDepletedCharacters(currentTick)

	// 2. Resolve temporal action locks (RFC 007 Section 6: Stateful Temporal Foraging Engine)
	s.resolveCompletedActions(currentTick, weather)
	s.forceRestForDepletedCharacters(currentTick)

	// 3. Publish Master Time broadcast
	payload := TickPayload{
		Tick:        currentTick,
		Timestamp:   now,
		InGameTime:  timeStr,
		IsNight:     isNight,
		Weather:     weather,
		Temperature: temperature,
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
		log.Printf("[Tick %d] Time: %s, Night: %v, Weather: %s, Temp: %.1f°C (Processed in %v)", currentTick, timeStr, isNight, weather, temperature, time.Since(startTime))
	}
}

func (s *Server) forceRestForDepletedCharacters(currentTick uint64) {
	rows, err := s.db.Query(`
		SELECT id, current_location, energy, health, hunger, action_intent_id
		FROM characters
		WHERE (health <= 0 AND hunger <= 0)
			OR (status != 'RESTING' AND (energy <= 0 OR health <= 0))
	`)
	if err != nil {
		log.Printf("Failed to query characters needing forced rest: %v", err)
		return
	}

	type depletedCharacter struct {
		id, location, intentID string
		energy, health, hunger int
	}
	var depleted []depletedCharacter
	for rows.Next() {
		var character depletedCharacter
		if err := rows.Scan(&character.id, &character.location, &character.energy, &character.health, &character.hunger, &character.intentID); err != nil {
			log.Printf("Failed to scan character needing forced rest: %v", err)
			continue
		}
		depleted = append(depleted, character)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Failed while reading characters needing forced rest: %v", err)
	}
	if err := rows.Close(); err != nil {
		log.Printf("Failed to close depleted character query: %v", err)
	}

	for _, character := range depleted {
		if character.health <= 0 && character.hunger <= 0 {
			location, recovered, err := s.reviveCollapsedCharacter(character.id, character.location, currentTick)
			if err != nil {
				log.Printf("Failed to revive collapsed character %s: %v", character.id, err)
				continue
			}
			if !recovered {
				continue
			}
			s.publishCollapseRecovery(character.id, character.intentID, character.location, location)
			continue
		}

		energyDuration := zeroEnergyRestDurationTicks
		healthDuration := zeroHealthRestDurationTicks
		if s.config != nil {
			if s.config.Rest.ZeroEnergyDurationTicks > 0 {
				energyDuration = s.config.Rest.ZeroEnergyDurationTicks
			}
			if s.config.Rest.ZeroHealthDurationTicks > 0 {
				healthDuration = s.config.Rest.ZeroHealthDurationTicks
			}
		}

		duration := energyDuration
		cause := "energy"
		if character.health <= 0 {
			duration = healthDuration
			cause = "health"
		}

		result, err := s.db.Exec(`
			UPDATE characters
			SET status = 'RESTING', action_start_tick = ?, action_end_tick = ?,
				action_target = current_location, action_intent_id = ''
			WHERE id = ? AND status != 'RESTING' AND (energy <= 0 OR health <= 0)
		`, currentTick, currentTick+uint64(duration), character.id)
		if err != nil {
			log.Printf("Failed to start forced rest for %s: %v", character.id, err)
			continue
		}
		updated, err := result.RowsAffected()
		if err != nil {
			log.Printf("Failed to check forced rest update for %s: %v", character.id, err)
			continue
		}
		if updated == 0 {
			continue
		}

		var message string
		if cause == "energy" {
			message = fmt.Sprintf("%s has slumped over and fallen asleep due to exhaustion.", character.id)
		} else {
			message = fmt.Sprintf("%s has collapsed from severe injury or illness.", character.id)
		}
		systemTopic := fmt.Sprintf("world/locations/%s/system", character.location)
		if err := s.mqttServer.Publish(systemTopic, []byte(message), false, 0); err != nil {
			log.Printf("Failed to publish forced rest for %s: %v", character.id, err)
		}

		directReason := "exhaustion"
		if cause == "health" {
			directReason = "severe injury or illness"
		}

		if character.intentID != "" {
			s.sendDirectError(character.id, character.intentID, "FORCED_REST", fmt.Sprintf("Your action was interrupted as you collapsed from %s.", directReason))
		}

		response := map[string]interface{}{
			"intent_id":      "",
			"status":         "SUCCESS",
			"action":         "REST",
			"forced":         true,
			"duration_ticks": duration,
			"message":        fmt.Sprintf("You collapsed from %s and must rest for %d ticks to recover.", directReason, duration),
		}
		s.includeCharacterSync(character.id, response)
		payload, err := json.Marshal(response)
		if err != nil {
			log.Printf("Failed to marshal forced rest response for %s: %v", character.id, err)
			continue
		}
		directTopic := fmt.Sprintf("world/agents/%s/direct", character.id)
		if err := s.mqttServer.Publish(directTopic, payload, false, 0); err != nil {
			log.Printf("Failed to publish forced rest response for %s: %v", character.id, err)
		}
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
			if err := s.clearCompletedAction(a.charID); err != nil {
				log.Printf("Failed to clear completed forage state for %s: %v", a.charID, err)
				continue
			}

			// Broadcast room system event
			sysTopic := fmt.Sprintf("world/locations/%s/system", a.locID)
			sysMsg := fmt.Sprintf("%s has finished foraging for %s.", a.charID, a.target)
			_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

			// Broadcast direct agent response on completion (RFC 007 Section 10.5)
			respMap := map[string]interface{}{
				"intent_id":        a.intentID,
				"status":           "SUCCESS",
				"ticks_spent":      res.TicksSpent,
				"energy_deducted":  res.EnergyDeducted,
				"skill_id":         res.SkillID,
				"skill_xp_gained":  res.SkillXPGained,
				"skill_level":      res.SkillLevel,
				"skill_experience": res.SkillExperience,
				"yield":            res.Yield,
				"total_weight_kg":  res.TotalWeightKg,
				"max_capacity_kg":  res.MaxCapacityKg,
				"narrative_log":    res.NarrativeLog,
			}
			if res.SkillLevel > res.PreviousLevel {
				respMap["message"] = fmt.Sprintf("You reached %s level %d!", res.SkillID, res.SkillLevel)
			}
			s.includeCharacterSync(a.charID, respMap)

			if respBytes, err := json.Marshal(respMap); err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}

		case "CRAFTING":
			res, err := db.ResolveCrafting(s.db, a.charID, a.target, int(a.endTick-a.startTick))
			if err != nil {
				log.Printf("Failed to resolve crafting for %s: %v", a.charID, err)
				continue
			}
			if err := s.clearCompletedAction(a.charID); err != nil {
				log.Printf("Failed to clear completed craft state for %s: %v", a.charID, err)
				continue
			}

			// Broadcast room system event
			sysTopic := fmt.Sprintf("world/locations/%s/system", a.locID)
			sysMsg := fmt.Sprintf("%s has finished crafting %s.", a.charID, res.RecipeName)
			_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

			// Broadcast direct agent response on completion (RFC 008)
			respMap := map[string]interface{}{
				"intent_id":        a.intentID,
				"status":           "SUCCESS",
				"action":           "CRAFT",
				"recipe_id":        res.RecipeID,
				"ticks_spent":      res.TicksSpent,
				"energy_deducted":  res.EnergyDeducted,
				"skill_id":         res.SkillID,
				"skill_xp_gained":  res.SkillXPGained,
				"skill_level":      res.SkillLevel,
				"skill_experience": res.SkillExperience,
				"yield":            res.Yield,
				"total_weight_kg":  res.TotalWeightKg,
				"max_capacity_kg":  res.MaxCapacityKg,
				"narrative_log":    res.NarrativeLog,
			}
			if res.SkillLevel > res.PreviousLevel {
				respMap["message"] = fmt.Sprintf("You reached %s level %d!", res.SkillID, res.SkillLevel)
			}
			s.includeCharacterSync(a.charID, respMap)

			if respBytes, err := json.Marshal(respMap); err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}

		case "RESTING":
			_, err = s.db.Exec("UPDATE characters SET energy = MIN(100, energy + 50), health = MIN(100, health + 15) WHERE id = ?", a.charID)
			if err != nil {
				log.Printf("Failed to resolve resting energy and health for %s: %v", a.charID, err)
				continue
			}

			if err := s.clearCompletedAction(a.charID); err != nil {
				log.Printf("Failed to clear completed rest state for %s: %v", a.charID, err)
				continue
			}

			var newEnergy, newHealth int
			_ = s.db.QueryRow("SELECT energy, health FROM characters WHERE id = ?", a.charID).Scan(&newEnergy, &newHealth)

			sysTopic := fmt.Sprintf("world/locations/%s/system", a.locID)
			sysMsg := fmt.Sprintf("%s has finished resting and feels refreshed.", a.charID)
			_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

			respMap := map[string]interface{}{
				"intent_id":     a.intentID,
				"status":        "SUCCESS",
				"action":        "REST",
				"energy":        newEnergy,
				"health":        newHealth,
				"narrative_log": fmt.Sprintf("You finished resting and recovered vitality (Energy: %d/100, Health: %d/100).", newEnergy, newHealth),
			}
			s.includeCharacterSync(a.charID, respMap)

			if respBytes, err := json.Marshal(respMap); err == nil {
				directTopic := fmt.Sprintf("world/agents/%s/direct", a.charID)
				_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
			}
		}

	}
}

func (s *Server) reviveCollapsedCharacter(characterID, currentLocation string, currentTick uint64) (string, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", false, fmt.Errorf("begin collapse recovery transaction: %w", err)
	}
	defer tx.Rollback()

	var hunger, health int
	if err := tx.QueryRow("SELECT hunger, health FROM characters WHERE id = ?", characterID).Scan(&hunger, &health); err != nil {
		return "", false, fmt.Errorf("read character condition: %w", err)
	}
	if hunger > 0 || health > 0 {
		return "", false, nil
	}

	var homeLocation string
	if err := tx.QueryRow("SELECT home_location_id FROM characters WHERE id = ?", characterID).Scan(&homeLocation); err != nil {
		return "", false, fmt.Errorf("read character home location: %w", err)
	}
	location := ""
	if homeLocation != "" {
		err = tx.QueryRow("SELECT id FROM locations WHERE id = ?", homeLocation).Scan(&location)
		if err != nil && err != sql.ErrNoRows {
			return "", false, fmt.Errorf("verify character's room: %w", err)
		}
	}
	if location == "" {
		err = tx.QueryRow("SELECT id FROM locations WHERE owner_id = ? ORDER BY id LIMIT 1", characterID).Scan(&location)
		if err != nil && err != sql.ErrNoRows {
			return "", false, fmt.Errorf("find character's owned room: %w", err)
		}
	}
	if location == "" || err == sql.ErrNoRows {
		location = currentLocation
		log.Printf("Character %s has no room to return to; reviving in place at %s", characterID, currentLocation)
	}

	result, err := tx.Exec(`
		UPDATE characters
		SET current_location = ?, health = 100, hunger = 100, status = 'RESTING',
			action_target = ?, action_start_tick = ?, action_end_tick = ?,
			action_intent_id = ''
		WHERE id = ? AND health <= 0 AND hunger <= 0
	`, location, location, currentTick, currentTick+collapseRecoveryRestDurationTicks, characterID)
	if err != nil {
		return "", false, fmt.Errorf("restore collapsed character: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return "", false, fmt.Errorf("check collapse recovery: %w", err)
	}
	if updated == 0 {
		return "", false, nil
	}
	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("commit collapse recovery: %w", err)
	}
	return location, true, nil
}

func (s *Server) publishCollapseRecovery(characterID, intentID, previousLocation, location string) {
	message := fmt.Sprintf("%s collapsed and was returned home to recover.", characterID)
	for _, locationID := range []string{previousLocation, location} {
		topic := fmt.Sprintf("world/locations/%s/system", locationID)
		if err := s.mqttServer.Publish(topic, []byte(message), false, 0); err != nil {
			log.Printf("Failed to publish collapse recovery for %s at %s: %v", characterID, locationID, err)
		}
		if previousLocation == location {
			break
		}
	}

	if intentID != "" {
		s.sendDirectError(characterID, intentID, "FORCED_REST", "Your action was interrupted after you collapsed. You were returned home and revived.")
	}

	response := map[string]interface{}{
		"intent_id":        "",
		"status":           "SUCCESS",
		"action":           "REST",
		"forced":           true,
		"duration_ticks":   collapseRecoveryRestDurationTicks,
		"health":           100,
		"hunger":           100,
		"message":          "You collapsed and were returned to your room, restored, and made to rest for 60 ticks.",
		"narrative_log":    "You were revived at home and must rest for 60 ticks.",
		"current_location": location,
	}
	s.includeCharacterSync(characterID, response)
	payload, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal collapse recovery response for %s: %v", characterID, err)
		return
	}
	topic := fmt.Sprintf("world/agents/%s/direct", characterID)
	if err := s.mqttServer.Publish(topic, payload, false, 0); err != nil {
		log.Printf("Failed to publish collapse recovery response for %s: %v", characterID, err)
	}
}

func (s *Server) clearCompletedAction(characterID string) error {
	_, err := s.db.Exec(`
		UPDATE characters
		SET status = 'IDLE', action_target = '', action_start_tick = 0, action_end_tick = 0, action_intent_id = ''
		WHERE id = ?
	`, characterID)
	return err
}

func (s *Server) includeCharacterSync(characterID string, response map[string]interface{}) {
	perception, err := db.GetCharacterPerception(s.db, characterID)
	if err != nil {
		log.Printf("Failed to refresh character skills for %s: %v", characterID, err)
		return
	}
	response["type"] = "STATE_SYNC"
	response["private_status"] = perception.Character
	response["location_graph"] = perception.LocationGraph
}
