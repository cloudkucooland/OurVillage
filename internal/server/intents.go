package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"agentmud/internal/db"

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

type ForagePayload struct {
	TargetResource string `json:"target_resource"`
	DurationTicks  int    `json:"duration_ticks"`
}

type ItemActionPayload struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type EquipPayload struct {
	ItemID string `json:"item_id"`
}

type CraftPayload struct {
	RecipeID string `json:"recipe_id"`
}

func (s *Server) SetupIntentHandlers() error {
	// Subscribe to all agent intent topics: world/agents/{agent_id}/intent
	err := s.mqttServer.Subscribe("world/agents/+/intent", 0, func(cl *mqtt.Client, sub packets.Subscription, pk packets.Packet) {
		topic := pk.TopicName
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
		case "FORAGE":
			s.handleForage(agentID, intent.IntentID, intent.Payload)
		case "CANCEL_ACTION":
			s.handleCancelAction(agentID, intent.IntentID, intent.Payload)
		case "DROP":
			s.handleDrop(agentID, intent.IntentID, intent.Payload)
		case "PICKUP":
			s.handlePickup(agentID, intent.IntentID, intent.Payload)
		case "EQUIP":
			s.handleEquip(agentID, intent.IntentID, intent.Payload)
		case "UNEQUIP":
			s.handleUnequip(agentID, intent.IntentID, intent.Payload)
		case "CRAFT":
			s.handleCraft(agentID, intent.IntentID, intent.Payload)
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

	// 2. Concurrency Protection (RFC 007 Section 6: Action Locks)
	var currentLocation, status string
	err = s.db.QueryRow("SELECT current_location, status FROM characters WHERE id = ?", agentID).Scan(&currentLocation, &status)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", fmt.Sprintf("Character %s not found", agentID))
		return
	}

	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot move: character is currently %s", status))
		return
	}

	if currentLocation == payload.TargetLocation {
		s.sendDirectResponse(agentID, intentID, "SUCCESS", "Already at target location")
		return
	}

	// 3. Encumbrance & Overburdened Check (RFC 007 Section 2)
	wInfo, err := db.GetCharacterWeightInfo(s.db, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", fmt.Sprintf("Failed to evaluate carrying weight: %v", err))
		return
	}

	if wInfo.Encumbrance == db.EncumbranceOverburdened {
		s.sendDirectError(agentID, intentID, "OVERBURDENED", fmt.Sprintf("Movement rejected: overburdened carrying %.1f kg (max limit %.1f kg). Relieve excess weight first.", wInfo.TotalWeightKg, wInfo.OverburdenLimitKg))
		return
	}

	// 4. Spatial Container & Vehicle Accessibility Check (RFC 007 Section 4)
	canAccess, reason, err := db.CanAccessLocation(s.db, agentID, payload.TargetLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}
	if !canAccess {
		s.sendDirectError(agentID, intentID, "CONTAINER_ACCESS_RESTRICTED", reason)
		return
	}

	// 5. Energy Decay (Double decay while encumbered - RFC 007 Section 2)
	energyCost := 1
	if wInfo.Encumbrance == db.EncumbranceEncumbered {
		energyCost = 2
	}

	// 6. Update character location and energy
	_, err = s.db.Exec("UPDATE characters SET current_location = ?, energy = MAX(0, energy - ?) WHERE id = ?", payload.TargetLocation, energyCost, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	// 7. Publish leave & enter events
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

	// Concurrency check (RFC 007 Section 6)
	var status string
	err := s.db.QueryRow("SELECT status FROM characters WHERE id = ?", agentID).Scan(&status)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}
	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot trade: character is currently %s", status))
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

		// Recipient carrying capacity validation
		var itemWeight float64
		_ = tx.QueryRow("SELECT weight_kg FROM items WHERE id = ?", payload.ItemID).Scan(&itemWeight)

		recipWeightInfo, err := db.GetCharacterWeightInfo(s.db, payload.RecipientID)
		if err == nil && recipWeightInfo != nil {
			addedWeight := float64(payload.Quantity) * itemWeight
			if recipWeightInfo.TotalWeightKg+addedWeight > recipWeightInfo.OverburdenLimitKg {
				s.sendDirectError(agentID, intentID, "RECIPIENT_OVERBURDENED", "Recipient cannot carry that much weight")
				return
			}
			var recipItemQty int
			_ = tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", payload.RecipientID, payload.ItemID).Scan(&recipItemQty)
			if recipItemQty == 0 && recipWeightInfo.SlotsUsed >= recipWeightInfo.MaxSlots {
				s.sendDirectError(agentID, intentID, "RECIPIENT_INVENTORY_FULL", "Recipient has no available inventory slots")
				return
			}
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
	var messageText string
	if err := json.Unmarshal(rawPayload, &messageText); err != nil {
		var chatObj ChatPayload
		if err := json.Unmarshal(rawPayload, &chatObj); err != nil {
			s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid CHAT payload")
			return
		}
		messageText = chatObj.Message
	}

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

func (s *Server) handleForage(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload ForagePayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid FORAGE payload")
		return
	}

	if payload.TargetResource == "" {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "target_resource is required")
		return
	}
	if payload.DurationTicks <= 0 {
		payload.DurationTicks = 60
	}

	// 1. Check character status and location
	var status, currentLocation string
	var energy, hunger int
	err := s.db.QueryRow("SELECT status, current_location, energy, hunger FROM characters WHERE id = ?", agentID).Scan(&status, &currentLocation, &energy, &hunger)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}

	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot start foraging: character is currently %s", status))
		return
	}

	if energy <= 0 || hunger <= 0 {
		s.sendDirectError(agentID, intentID, "EXHAUSTED", "Character is too exhausted or hungry to forage")
		return
	}

	// 2. Check Overburden state
	wInfo, err := db.GetCharacterWeightInfo(s.db, agentID)
	if err == nil && wInfo != nil && wInfo.Encumbrance == db.EncumbranceOverburdened {
		s.sendDirectError(agentID, intentID, "OVERBURDENED", "Character is overburdened and cannot forage. Relieve excess weight first.")
		return
	}

	// 3. Verify valid foraging location and resource pool
	var resCount int
	err = s.db.QueryRow("SELECT COUNT(*) FROM location_resources WHERE location_id = ? AND resource_id = ?", currentLocation, payload.TargetResource).Scan(&resCount)
	if err != nil || resCount == 0 {
		s.sendDirectError(agentID, intentID, "INVALID_FORAGE_LOCATION", fmt.Sprintf("Resource '%s' cannot be foraged in location '%s'", payload.TargetResource, currentLocation))
		return
	}

	// 4. Action Lock: Set character status to FORAGING (RFC 007 Section 6)
	currentTick := s.GetTickCount()
	endTick := currentTick + uint64(payload.DurationTicks)

	_, err = s.db.Exec(`
		UPDATE characters 
		SET status = 'FORAGING', action_start_tick = ?, action_end_tick = ?, action_target = ?, action_intent_id = ?
		WHERE id = ?
	`, currentTick, endTick, payload.TargetResource, intentID, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", fmt.Sprintf("Failed to lock character action: %v", err))
		return
	}

	// 5. Broadcast foraging commencement to location
	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s has started foraging for %s (%d ticks).", agentID, payload.TargetResource, payload.DurationTicks)), false, 0)
}

func (s *Server) handleCancelAction(agentID string, intentID string, rawPayload json.RawMessage) {
	var status, currentLocation, targetResource string
	var startTick uint64
	err := s.db.QueryRow(`
		SELECT status, current_location, action_target, action_start_tick 
		FROM characters WHERE id = ?
	`, agentID).Scan(&status, &currentLocation, &targetResource, &startTick)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}

	if status == "IDLE" {
		s.sendDirectError(agentID, intentID, "NO_ACTIVE_ACTION", "Character is not currently performing an action")
		return
	}

	if status == "FORAGING" {
		currentTick := s.GetTickCount()
		ticksSpent := int(currentTick - startTick)
		if ticksSpent < 0 {
			ticksSpent = 0
		}

		res, err := db.ResolveForaging(s.db, agentID, currentLocation, targetResource, ticksSpent, s.GetWeather(), true)
		if err != nil {
			s.sendDirectError(agentID, intentID, "RESOLUTION_ERROR", err.Error())
			return
		}

		// Broadcast room cancellation event
		sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
		_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s stopped foraging early.", agentID)), false, 0)

		// Direct response matching RFC 007 Section 10.5
		respMap := map[string]interface{}{
			"intent_id":       intentID,
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
			directTopic := fmt.Sprintf("world/agents/%s/direct", agentID)
			_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
		}
	} else if status == "CRAFTING" {
		err := db.CancelCrafting(s.db, agentID)
		if err != nil {
			s.sendDirectError(agentID, intentID, "CANCEL_FAILED", err.Error())
			return
		}

		// Broadcast room cancellation event
		sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
		_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s stopped crafting.", agentID)), false, 0)

		s.sendDirectResponse(agentID, intentID, "SUCCESS", "Crafting action cancelled; ingredients refunded.")
	} else {
		// Generic cancel for other actions
		_, _ = s.db.Exec("UPDATE characters SET status = 'IDLE', action_end_tick = 0, action_start_tick = 0 WHERE id = ?", agentID)
		s.sendDirectResponse(agentID, intentID, "SUCCESS", "Action cancelled")
	}
}

func (s *Server) handleDrop(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload ItemActionPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid DROP payload")
		return
	}

	var status, currentLocation string
	err := s.db.QueryRow("SELECT status, current_location FROM characters WHERE id = ?", agentID).Scan(&status, &currentLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}
	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot drop items: character is currently %s", status))
		return
	}

	err = db.DropItem(s.db, agentID, payload.ItemID, payload.Quantity)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DROP_FAILED", err.Error())
		return
	}

	var itemName string
	_ = s.db.QueryRow("SELECT name FROM items WHERE id = ?", payload.ItemID).Scan(&itemName)
	if itemName == "" {
		itemName = payload.ItemID
	}

	// Broadcast room system message (RFC 007 Section 3)
	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s dropped %d %s on the ground.", agentID, payload.Quantity, itemName)), false, 0)

	s.sendDirectResponse(agentID, intentID, "SUCCESS", fmt.Sprintf("Dropped %d %s", payload.Quantity, itemName))
}

func (s *Server) handlePickup(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload ItemActionPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid PICKUP payload")
		return
	}

	var status, currentLocation string
	err := s.db.QueryRow("SELECT status, current_location FROM characters WHERE id = ?", agentID).Scan(&status, &currentLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}
	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot pickup items: character is currently %s", status))
		return
	}

	err = db.PickupItem(s.db, agentID, payload.ItemID, payload.Quantity)
	if err != nil {
		if strings.Contains(err.Error(), "EXCEEDS_CARRYING_CAPACITY") {
			s.sendDirectError(agentID, intentID, "EXCEEDS_CARRYING_CAPACITY", err.Error())
		} else if strings.Contains(err.Error(), "INVENTORY_FULL") {
			s.sendDirectError(agentID, intentID, "INVENTORY_FULL", err.Error())
		} else {
			s.sendDirectError(agentID, intentID, "PICKUP_FAILED", err.Error())
		}
		return
	}

	var itemName string
	_ = s.db.QueryRow("SELECT name FROM items WHERE id = ?", payload.ItemID).Scan(&itemName)
	if itemName == "" {
		itemName = payload.ItemID
	}

	// Broadcast room system message (RFC 007 Section 3)
	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s picked up %d %s from the ground.", agentID, payload.Quantity, itemName)), false, 0)

	s.sendDirectResponse(agentID, intentID, "SUCCESS", fmt.Sprintf("Picked up %d %s", payload.Quantity, itemName))
}

func (s *Server) handleEquip(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload EquipPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid EQUIP payload")
		return
	}

	var status string
	err := s.db.QueryRow("SELECT status FROM characters WHERE id = ?", agentID).Scan(&status)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}
	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot equip gear: character is currently %s", status))
		return
	}

	err = db.EquipContainer(s.db, agentID, payload.ItemID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "EQUIP_FAILED", err.Error())
		return
	}

	s.sendDirectResponse(agentID, intentID, "SUCCESS", fmt.Sprintf("Equipped %s", payload.ItemID))
}

func (s *Server) handleUnequip(agentID string, intentID string, rawPayload json.RawMessage) {
	var status string
	err := s.db.QueryRow("SELECT status FROM characters WHERE id = ?", agentID).Scan(&status)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}
	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot unequip gear: character is currently %s", status))
		return
	}

	err = db.UnequipContainer(s.db, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "UNEQUIP_FAILED", err.Error())
		return
	}

	s.sendDirectResponse(agentID, intentID, "SUCCESS", "Unequipped container")
}

func (s *Server) handleCraft(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload CraftPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid CRAFT payload")
		return
	}

	if payload.RecipeID == "" {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "recipe_id is required")
		return
	}

	// 1. Concurrency Check
	var status, currentLocation string
	err := s.db.QueryRow("SELECT status, current_location FROM characters WHERE id = ?", agentID).Scan(&status, &currentLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}
	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot craft: character is currently %s", status))
		return
	}

	// 2. Fetch Recipe
	recipe, err := db.GetRecipe(s.db, payload.RecipeID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "RECIPE_NOT_FOUND", err.Error())
		return
	}

	// 3. Start Crafting
	currentTick := s.GetTickCount()
	err = db.StartCrafting(s.db, agentID, payload.RecipeID, intentID, currentTick)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CRAFT_FAILED", err.Error())
		return
	}

	// 4. Broadcast room announcement
	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s has started crafting %s (%d ticks).", agentID, recipe.Name, recipe.DurationTicks)), false, 0)
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
