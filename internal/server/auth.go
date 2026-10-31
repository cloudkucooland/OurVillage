package server

import (
	"database/sql"
	"log"
	"strings"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type DBAuthHook struct {
	mqtt.HookBase
	db *sql.DB
}

func NewDBAuthHook(db *sql.DB) *DBAuthHook {
	return &DBAuthHook{
		db: db,
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

	// Update character to online
	_, _ = h.db.Exec("UPDATE characters SET is_online = 1 WHERE id = ?", username)
	log.Printf("[Auth] Successfully authenticated character: '%s'", username)
	return true
}

func (h *DBAuthHook) OnDisconnect(cl *mqtt.Client, err error, expire bool) {
	if cl.Net.Inline {
		return
	}

	username := string(cl.Properties.Username)
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

	if write {
		// Characters can publish intents to their own topic: world/agents/{username}/intent
		if topic == "world/agents/"+username+"/intent" {
			return true
		}

		// Characters can publish chat to room chat: world/locations/{loc}/chat
		if strings.HasPrefix(topic, "world/locations/") && strings.HasSuffix(topic, "/chat") {
			return true
		}

		log.Printf("[ACL] Write denied for user '%s' on topic '%s'", username, topic)
		return false
	}

	// Read checks (Subscribing or receiving forwarded packets)
	// 1. Global updates (time, announcements)
	if strings.HasPrefix(topic, "world/global/") {
		return true
	}

	// 2. Character's own direct messages
	if strings.HasPrefix(topic, "world/agents/"+username+"/") {
		return true
	}

	// 3. Location topics (chat, system, etc.)
	if strings.HasPrefix(topic, "world/locations/") {
		return true
	}

	log.Printf("[ACL] Read denied for user '%s' on topic '%s'", username, topic)
	return false
}
