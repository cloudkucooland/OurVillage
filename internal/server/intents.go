package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type IntentMessage struct {
	IntentID string          `json:"intent_id"`
	Action   string          `json:"action"`
	Payload  json.RawMessage `json:"payload"`
}

type MovePayload struct {
	TargetLocation string `json:"target_location"`
}

type TransferPayload struct {
	RecipientID string `json:"recipient_id"`
	Amount      int    `json:"amount"`
	ItemID      string `json:"item_id"`
	Quantity    int    `json:"quantity"`
}

type ChatPayload struct {
	Message string `json:"message"`
}

func (s *Server) SetupIntentHandlers() error {
	// Subscribe to all agent intent topics: world/agents/{agent_id}/intent
	err := s.mqttServer.Subscribe("world/agents/+/intent", 0, func(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
		topic := pk.Topic
		parts := strings.Split(topic, "/")
		if len(parts) < 4 {
			log.Printf("Invalid intent topic format: %s", topic)
			return
		}
		agentID := parts[2]

		var intent IntentMessage
		if err := json.Unmarshal(pk.Payload, &intent); err != nil {
			log.Printf("Failed to unmarshal intent for agent %s: %v", agentID, err)
			s.sendDirectError(agentID, "", "INVALID_JSON", err.Error())
			return
		}

		log.Printf("[Intent] Received %s from agent %s (intent_id: %s)", intent.Action, agentID, intent.IntentID)

		switch strings.ToUpper(intent.Action) {
		case "MOVE":
			s.handleMove(agentID, intent.IntentID, intent.Payload)
		case "TRANSFER":
			s.handleTransfer(agentID, intent.IntentID, intent.Payload)
		case "CHAT":
			s.handleChat(agentID, intent.IntentID, intent.Payload)
		default:
			s.sendDirectError(agentID, intent.IntentID, "UNKNOWN_ACTION", fmt.Sprintf("Unsupported action: %s", intent.Action))
		}
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to agent intents: %w", err)
	}

	log.Println("MQTT intent handlers successfully registered on world/agents/+/intent")
	return nil
}

func (s *Server) handleMove(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload MovePayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid MOVE payload")
		return
	}

	// 1. Verify target location exists
	var locName string
	err := s.db.QueryRow("SELECT name FROM locations WHERE id = ?", payload.TargetLocation).Scan(&locName)
	if err == sql.ErrNoRows {
		s.sendDirectError(agentID, intentID, "LOCATION_NOT_FOUND", fmt.Sprintf("Location %s does not exist", payload.TargetLocation))
		return
	} else if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	// 2. Get current location of agent
	var currentLocation string
	err = s.db.QueryRow("SELECT current_location FROM characters WHERE id = ?", agentID).Scan(&currentLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", fmt.Sprintf("Character %s not found", agentID))
		return
	}

	if currentLocation == payload.TargetLocation {
		s.sendDirectResponse(agentID, intentID, "SUCCESS", "Already at target location")
		return
	}

	// 3. Update character location
	_, err = s.db.Exec("UPDATE characters SET current_location = ? WHERE id = ?", payload.TargetLocation, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	// 4. Publish leave & enter events
	leaveTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	enterTopic := fmt.Sprintf("world/locations/%s/system", payload.TargetLocation)

	_ = s.mqttServer.Publish(leaveTopic, []byte(fmt.Sprintf("%s has left for %s.", agentID, locName)), false, 0)
	_ = s.mqttServer.Publish(enterTopic, []byte(fmt.Sprintf("%s has arrived.", agentID)), false, 0)
	_ = s.mqttServer.Publish("world/global/spatial/move", []byte(fmt.Sprintf(`{"agent_id":"%s","from":"%s","to":"%s"}`, agentID, currentLocation, payload.TargetLocation)), false, 0)

	s.sendDirectResponse(agentID, intentID, "SUCCESS", fmt.Sprintf("Moved to %s", locName))
}

func (s *Server) handleTransfer(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload TransferPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid TRANSFER payload")
		return
	}

	// Begin transaction for safe economic transfer
	tx, err := s.db.Begin()
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}
	defer tx.Rollback()

	// 1. Handle Gold Transfer if amount > 0
	if payload.Amount > 0 {
		var senderGold int
		err := tx.QueryRow("SELECT gold FROM characters WHERE id = ?", agentID).Scan(&senderGold)
		if err != nil {
			s.sendDirectError(agentID, intentID, "SENDER_NOT_FOUND", "Sender character not found")
			return
		}
		if senderGold < payload.Amount {
			s.sendDirectError(agentID, intentID, "INSUFFICIENT_GOLD", fmt.Sprintf("Have %d gold, requested transfer of %d", senderGold, payload.Amount))
			return
		}

		var recipientExists int
		err = tx.QueryRow("SELECT COUNT(*) FROM characters WHERE id = ?", payload.RecipientID).Scan(&recipientExists)
		if err != nil || recipientExists == 0 {
			s.sendDirectError(agentID, intentID, "RECIPIENT_NOT_FOUND", fmt.Sprintf("Recipient %s not found", payload.RecipientID))
			return
		}

		_, err = tx.Exec("UPDATE characters SET gold = gold - ? WHERE id = ?", payload.Amount, agentID)
		if err != nil {
			s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
			return
		}
		_, err = tx.Exec("UPDATE characters SET gold = gold + ? WHERE id = ?", payload.Amount, payload.RecipientID)
		if err != nil {
			s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
			return
		}
	}

	// 2. Handle Item Transfer if ItemID != "" and Quantity > 0
	if payload.ItemID != "" && payload.Quantity > 0 {
		var senderQty int
		err := tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", agentID, payload.ItemID).Scan(&senderQty)
		if err != nil || senderQty < payload.Quantity {
			s.sendDirectError(agentID, intentID, "INSUFFICIENT_ITEMS", fmt.Sprintf("Not enough item %s in inventory", payload.ItemID))
			return
		}

		// Deduct from sender
		_, err = tx.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", payload.Quantity, agentID, payload.ItemID)
		if err != nil {
			s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
			return
		}

		// Add to recipient
		_, err = tx.Exec(`
			INSERT INTO inventories (character_id, item_id, quantity) VALUES (?, ?, ?)
			ON CONFLICT(character_id, item_id) DO UPDATE SET quantity = quantity + ?
		`, payload.RecipientID, payload.ItemID, payload.Quantity, payload.Quantity)
		if err != nil {
			s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	s.sendDirectResponse(agentID, intentID, "SUCCESS", "Transfer completed successfully")
}

func (s *Server) handleChat(agentID string, intentID string, rawPayload json.RawMessage) {
	// Payload can be a raw string or JSON object
	var messageText string
	if err := json.Unmarshal(rawPayload, &messageText); err != nil {
		var chatObj ChatPayload
		if err := json.Unmarshal(rawPayload, &chatObj); err != nil {
			s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid CHAT payload")
			return
		}
		messageText = chatObj.Message
	}

	// Get agent's current location
	var currentLocation string
	err := s.db.QueryRow("SELECT current_location FROM characters WHERE id = ?", agentID).Scan(&currentLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}

	chatTopic := fmt.Sprintf("world/locations/%s/chat", currentLocation)
	chatPayload := fmt.Sprintf(`{"agent_id":"%s","message":"%s"}`, agentID, messageText)

	_ = s.mqttServer.Publish(chatTopic, []byte(chatPayload), false, 0)
	s.sendDirectResponse(agentID, intentID, "SUCCESS", "Message broadcasted")
}

func (s *Server) sendDirectError(agentID string, intentID string, errCode string, message string) {
	topic := fmt.Sprintf("world/agents/%s/direct", agentID)
	resp := fmt.Sprintf(`{"intent_id":"%s","status":"ERROR","error_code":"%s","message":"%s"}`, intentID, errCode, message)
	_ = s.mqttServer.Publish(topic, []byte(resp), false, 0)
}

func (s *Server) sendDirectResponse(agentID string, intentID string, status string, message string) {
	topic := fmt.Sprintf("world/agents/%s/direct", agentID)
	resp := fmt.Sprintf(`{"intent_id":"%s","status":"%s","message":"%s"}`, intentID, status, message)
	_ = s.mqttServer.Publish(topic, []byte(resp), false, 0)
}
