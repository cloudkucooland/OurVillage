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

const recentMoveTTL = 10 * time.Minute

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
	now := time.Now()
	h.purgeRecentMovesLocked(now)
	h.recentMoves[characterID] = moveRecord{
		locationID: locationID,
		time:       now,
	}
}

func (h *DBAuthHook) purgeRecentMovesLocked(now time.Time) {
	for characterID, move := range h.recentMoves {
		if now.Sub(move.time) > recentMoveTTL {
			delete(h.recentMoves, characterID)
		}
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
		_, _ = h.db.Exec("UPDATE characters SET is_online = 0, status = 'IDLE', action_target = '', action_start_tick = 0, action_end_tick = 0, action_intent_id = '' WHERE id = ?", username)
		log.Printf("[Auth] Character disconnected: '%s' (resetting active actions to IDLE)", username)
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

	if write {
		if topic == "world/agents/"+username+"/intent" {
			return true
		}

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

	if strings.HasPrefix(topic, "world/global/") {
		return true
	}

	if strings.HasPrefix(topic, "world/agents/") {
		parts := strings.Split(topic, "/")
		if len(parts) >= 3 {
			targetAgentID := parts[2]
			if targetAgentID != username {
				log.Printf("[ACL] Read denied for user '%s' on private topic '%s'", username, topic)
				return false
			}
			return true
		}
	}

	if strings.HasPrefix(topic, "world/locations/") {
		parts := strings.Split(topic, "/")
		if len(parts) >= 4 {
			targetLocationID := parts[2]
			channelType := parts[3]

			if !h.isCharacterAtLocation(username, targetLocationID) {
				log.Printf("[ACL] Read denied for user '%s' on location '%s': character not present", username, targetLocationID)
				return false
			}

			if channelType == "whisper" {
				if len(parts) == 5 {
					targetAgentID := parts[4]
					return targetAgentID == username
				}
				return false
			}

			return channelType == "chat" || channelType == "system"
		}
	}

	log.Printf("[ACL] Read denied for user '%s' on topic '%s'", username, topic)
	return false
}

func (h *DBAuthHook) isCharacterAtLocation(characterID, locationID string) bool {
	h.mu.Lock()
	h.purgeRecentMovesLocked(time.Now())
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
