package server

import (
	"database/sql"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type moveRecord struct {
	locationID string
	time       time.Time
}

type DBAuthHook struct {
	mqtt.HookBase
	db          *sql.DB
	mu          sync.Mutex
	recentMoves map[string]moveRecord
}

func NewDBAuthHook(db *sql.DB) *DBAuthHook {
	return &DBAuthHook{
		db:          db,
		recentMoves: make(map[string]moveRecord),
	}
}

func (h *DBAuthHook) RecordMove(characterID, locationID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recentMoves[characterID] = moveRecord{
		locationID: locationID,
		time:       time.Now(),
	}
}

func (h *DBAuthHook) ID() string {
	return "db-auth-hook"
}

func (h *DBAuthHook) Provides(b byte) bool {
	return b == mqtt.OnConnectAuthenticate || b == mqtt.OnACLCheck || b == mqtt.OnDisconnect
}

func (h *DBAuthHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	if cl.Net.Inline {
		return true
	}

	username := string(pk.Connect.Username)
	password := string(pk.Connect.Password)

	if username == "" {
		log.Printf("[Auth] Denied connection: empty username")
		return false
	}

	var expectedPassword string
	err := h.db.QueryRow("SELECT mqtt_password FROM characters WHERE id = ?", username).Scan(&expectedPassword)
	if err == sql.ErrNoRows {
		log.Printf("[Auth] Denied connection: unknown character '%s'", username)
		return false
	} else if err != nil {
		log.Printf("[Auth] Database error authenticating '%s': %v", username, err)
		return false
	}

	if password != expectedPassword {
		log.Printf("[Auth] Denied connection for character '%s': invalid password", username)
		return false
	}

	// Update character state to online
	_, _ = h.db.Exec("UPDATE characters SET is_online = 1 WHERE id = ?", username)
	log.Printf("[Auth] Successfully authenticated character: '%s'", username)
	return true
}

func (h *DBAuthHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
	if cl.Net.Inline {
		return
	}

	username := string(cl.Properties.Username)
	if username == "" {
		username = cl.ID
	}

	if username != "" {
		_, _ = h.db.Exec("UPDATE characters SET is_online = 0 WHERE id = ?", username)
		log.Printf("[Auth] Character disconnected: '%s'", username)
	}
}

func (h *DBAuthHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	if cl.Net.Inline {
		return true
	}

	username := string(cl.Properties.Username)
	if username == "" {
		username = cl.ID
	}

	if username == "" {
		return false
	}

	// 1. Write Permissions (Publishing Intents & Chat)
	if write {
		// Agents can publish intent payloads to their own intent channel: world/agents/{username}/intent
		if topic == "world/agents/"+username+"/intent" {
			return true
		}

		// Agents can publish chat to room chat IF they are physically in that location
		if strings.HasPrefix(topic, "world/locations/") && strings.HasSuffix(topic, "/chat") {
			parts := strings.Split(topic, "/")
			if len(parts) >= 4 {
				targetLocationID := parts[2]
				return h.isCharacterAtLocation(username, targetLocationID)
			}
		}

		log.Printf("[ACL] Write denied for user '%s' on topic '%s'", username, topic)
		return false
	}

	// 2. Read Permissions (Subscribing / Receiving)

	// Global broadcasts (time, announcements) are readable by all authenticated agents
	if strings.HasPrefix(topic, "world/global/") {
		return true
	}

	// Strictly isolate personal agent channels (status, direct, whisper)
	if strings.HasPrefix(topic, "world/agents/") {
		parts := strings.Split(topic, "/")
		if len(parts) >= 3 {
			targetAgentID := parts[2]
			// Strictly deny if the client is not the owner of this agent topic
			if targetAgentID != username {
				log.Printf("[ACL] Read denied for user '%s' on private topic '%s'", username, topic)
				return false
			}
			return true
		}
	}

	// Spatial location checks (world/locations/{location_id}/*)
	if strings.HasPrefix(topic, "world/locations/") {
		parts := strings.Split(topic, "/")
		if len(parts) >= 4 {
			targetLocationID := parts[2]
			channelType := parts[3]

			// Must be physically present in the room
			if !h.isCharacterAtLocation(username, targetLocationID) {
				log.Printf("[ACL] Read denied for user '%s' on location '%s': character not present", username, targetLocationID)
				return false
			}

			// Handle whispers (world/locations/{loc}/whisper/{target_agent_id})
			if channelType == "whisper" {
				if len(parts) == 5 {
					targetAgentID := parts[4]
					// Characters can only read whispers directed at themselves
					return targetAgentID == username
				}
				return false
			}

			// Room system alerts and public chat are allowed if present
			return channelType == "chat" || channelType == "system"
		}
	}

	log.Printf("[ACL] Read denied for user '%s' on topic '%s'", username, topic)
	return false
}

// Helper to query spatial presence from SQLite
func (h *DBAuthHook) isCharacterAtLocation(characterID, locationID string) bool {
	h.mu.Lock()
	if rec, ok := h.recentMoves[characterID]; ok {
		if rec.locationID == locationID && time.Since(rec.time) < 5*time.Second {
			h.mu.Unlock()
			return true
		}
	}
	h.mu.Unlock()

	var currentLoc string
	err := h.db.QueryRow("SELECT current_location FROM characters WHERE id = ?", characterID).Scan(&currentLoc)
	if err != nil {
		return false
	}
	return currentLoc == locationID
}
