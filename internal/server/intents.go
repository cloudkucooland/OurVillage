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

type BuyPayload struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type RestockPayload struct {
	ItemID    string `json:"item_id"`
	Quantity  int    `json:"quantity"`
	PriceGold int    `json:"price_gold"`
}

type DirectResponse struct {
	IntentID  string `json:"intent_id"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message"`
}

func (s *Server) SetupIntentHandlers() error {
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
		case "TRANSFER", "GIVE":
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
		case "BUY":
			s.handleBuy(agentID, intent.IntentID, intent.Payload)
		case "RESTOCK":
			s.handleRestock(agentID, intent.IntentID, intent.Payload)
		case "EAT":
			s.handleEat(agentID, intent.IntentID, intent.Payload)
		case "REST":
			s.handleRest(agentID, intent.IntentID, intent.Payload)
		case "LOOK", "STATUS":
			s.handleLook(agentID, intent.IntentID)
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

	var locName string
	err := s.db.QueryRow("SELECT name FROM locations WHERE id = ?", payload.TargetLocation).Scan(&locName)
	if err == sql.ErrNoRows {
		s.sendDirectError(agentID, intentID, "LOCATION_NOT_FOUND", fmt.Sprintf("Location %s does not exist", payload.TargetLocation))
		return
	} else if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

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
		s.handleLook(agentID, intentID, "Already at target location")
		return
	}

	wInfo, err := db.GetCharacterWeightInfo(s.db, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", fmt.Sprintf("Failed to evaluate carrying weight: %v", err))
		return
	}

	if wInfo.Encumbrance == db.EncumbranceOverburdened {
		s.sendDirectError(agentID, intentID, "OVERBURDENED", fmt.Sprintf("Movement rejected: overburdened carrying %.1f kg (max limit %.1f kg). Relieve excess weight first.", wInfo.TotalWeightKg, wInfo.OverburdenLimitKg))
		return
	}

	canAccess, reason, err := db.CanAccessLocation(s.db, agentID, payload.TargetLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}
	if !canAccess {
		s.sendDirectError(agentID, intentID, "CONTAINER_ACCESS_RESTRICTED", reason)
		return
	}

	canTravel, travelTicks, err := db.CanTravelLocation(s.db, currentLocation, payload.TargetLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", fmt.Sprintf("Failed to evaluate travel path: %v", err))
		return
	}
	if !canTravel {
		s.sendDirectError(agentID, intentID, "LOCATION_UNREACHABLE", fmt.Sprintf("Cannot travel directly to %s from %s: no direct graph connection.", payload.TargetLocation, currentLocation))
		return
	}

	// Check opening and closing hours (open_time, close_time)
	var openTime, closeTime string
	var ownerID sql.NullString
	_ = s.db.QueryRow("SELECT open_time, close_time, owner_id FROM locations WHERE id = ?", payload.TargetLocation).Scan(&openTime, &closeTime, &ownerID)
	if openTime != "" && closeTime != "" {
		currentTick := s.GetTickCount()
		totalMinutes := int(currentTick) * 15
		currentHour := (totalMinutes / 60) % 24
		currentMinute := totalMinutes % 60
		currentTimeMinutes := currentHour*60 + currentMinute

		parseHM := func(hm string) int {
			var h, m int
			_, _ = fmt.Sscanf(hm, "%d:%d", &h, &m)
			return h*60 + m
		}

		openMinutes := parseHM(openTime)
		closeMinutes := parseHM(closeTime)

		isOpen := false
		if openMinutes <= closeMinutes {
			isOpen = currentTimeMinutes >= openMinutes && currentTimeMinutes <= closeMinutes
		} else {
			// Crosses midnight (e.g. 20:00 to 04:00)
			isOpen = currentTimeMinutes >= openMinutes || currentTimeMinutes <= closeMinutes
		}

		isOwner := ownerID.Valid && ownerID.String == agentID

		if !isOpen && !isOwner {
			s.sendDirectError(agentID, intentID, "LOCATION_CLOSED", fmt.Sprintf("Cannot enter %s: location is closed (hours: %s to %s).", locName, openTime, closeTime))
			return
		}
	}

	energyCost := travelTicks
	if wInfo.Encumbrance == db.EncumbranceEncumbered {
		energyCost = travelTicks * 2
	}

	_, err = s.db.Exec("UPDATE characters SET current_location = ?, energy = MAX(0, energy - ?) WHERE id = ?", payload.TargetLocation, energyCost, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	if s.authHook != nil {
		s.authHook.RecordMove(agentID, payload.TargetLocation)
	}

	leaveTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	enterTopic := fmt.Sprintf("world/locations/%s/system", payload.TargetLocation)

	_ = s.mqttServer.Publish(leaveTopic, []byte(fmt.Sprintf("%s has left for %s.", agentID, locName)), false, 0)
	_ = s.mqttServer.Publish(enterTopic, []byte(fmt.Sprintf("%s has arrived.", agentID)), false, 0)

	spatialPayload, _ := json.Marshal(map[string]string{
		"agent_id": agentID,
		"from":     currentLocation,
		"to":       payload.TargetLocation,
	})
	_ = s.mqttServer.Publish("world/global/spatial/move", spatialPayload, false, 0)

	s.handleLook(agentID, intentID, fmt.Sprintf("Moved to %s", locName))
}

func (s *Server) handleTransfer(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload TransferPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid TRANSFER payload")
		return
	}

	var status, currentLocation string
	err := s.db.QueryRow("SELECT status, current_location FROM characters WHERE id = ?", agentID).Scan(&status, &currentLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}
	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot trade: character is currently %s", status))
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}
	defer tx.Rollback()

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

	if payload.ItemID != "" && payload.Quantity > 0 {
		var senderQty int
		err := tx.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", agentID, payload.ItemID).Scan(&senderQty)
		if err != nil || senderQty < payload.Quantity {
			s.sendDirectError(agentID, intentID, "INSUFFICIENT_ITEMS", fmt.Sprintf("Not enough item %s in inventory", payload.ItemID))
			return
		}

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

		if senderQty == payload.Quantity {
			_, err = tx.Exec("DELETE FROM inventories WHERE character_id = ? AND item_id = ?", agentID, payload.ItemID)
		} else {
			_, err = tx.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", payload.Quantity, agentID, payload.ItemID)
		}
		if err != nil {
			s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
			return
		}

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

	currentTick := s.GetTickCount()
	eventPayload := map[string]interface{}{
		"sender_id":    agentID,
		"recipient_id": payload.RecipientID,
		"amount":       payload.Amount,
		"item_id":      payload.ItemID,
		"quantity":     payload.Quantity,
	}
	if _, err := db.RecordEventLog(s.db, currentTick, "TRANSFER", agentID, payload.RecipientID, currentLocation, eventPayload); err != nil {
		log.Printf("[Server Warning] Failed to record event log: %v", err)
	}

	s.notifyTransferRecipient(agentID, payload.RecipientID, currentLocation, payload.Amount, payload.ItemID, payload.Quantity)

	s.handleLook(agentID, intentID, "Transfer completed successfully")
}

func (s *Server) notifyTransferRecipient(senderID string, recipientID string, currentLocation string, amount int, itemID string, quantity int) {
	var isOnline int
	_ = s.db.QueryRow("SELECT is_online FROM characters WHERE id = ?", recipientID).Scan(&isOnline)
	if isOnline == 0 {
		return
	}

	var senderName string
	_ = s.db.QueryRow("SELECT name FROM characters WHERE id = ?", senderID).Scan(&senderName)
	if senderName == "" {
		senderName = senderID
	}

	var parts []string
	if amount > 0 {
		parts = append(parts, fmt.Sprintf("%d gold", amount))
	}
	if itemID != "" && quantity > 0 {
		var itemName string
		_ = s.db.QueryRow("SELECT name FROM items WHERE id = ?", itemID).Scan(&itemName)
		if itemName == "" {
			itemName = itemID
		}
		parts = append(parts, fmt.Sprintf("%d x %s", quantity, itemName))
	}
	if len(parts) == 0 {
		return
	}

	transferDesc := strings.Join(parts, " and ")
	noticeMsg := fmt.Sprintf("%s transferred %s to you.", senderName, transferDesc)

	s.handleLook(recipientID, fmt.Sprintf("req_transfer_notice_%d", s.GetTickCount()), noticeMsg)

	var recipLocation string
	_ = s.db.QueryRow("SELECT current_location FROM characters WHERE id = ?", recipientID).Scan(&recipLocation)
	if recipLocation == currentLocation && currentLocation != "" {
		sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
		var recipName string
		_ = s.db.QueryRow("SELECT name FROM characters WHERE id = ?", recipientID).Scan(&recipName)
		if recipName == "" {
			recipName = recipientID
		}
		sysMsg := fmt.Sprintf("%s hands %s to %s.", senderName, transferDesc, recipName)
		_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)
	}
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
	chatPayload, _ := json.Marshal(map[string]string{
		"agent_id": agentID,
		"message":  messageText,
	})

	_ = s.mqttServer.Publish(chatTopic, chatPayload, false, 0)
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
		payload.DurationTicks = 15
	}

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

	wInfo, err := db.GetCharacterWeightInfo(s.db, agentID)
	if err == nil && wInfo != nil && wInfo.Encumbrance == db.EncumbranceOverburdened {
		s.sendDirectError(agentID, intentID, "OVERBURDENED", "Character is overburdened and cannot forage. Relieve excess weight first.")
		return
	}

	var resCount int
	err = s.db.QueryRow("SELECT COUNT(*) FROM location_resources WHERE location_id = ? AND resource_id = ?", currentLocation, payload.TargetResource).Scan(&resCount)
	if err != nil || resCount == 0 {
		s.sendDirectError(agentID, intentID, "INVALID_FORAGE_LOCATION", fmt.Sprintf("Resource '%s' cannot be foraged in location '%s'", payload.TargetResource, currentLocation))
		return
	}

	skillID := db.GetSkillForResource(payload.TargetResource)
	skillLevel, _, err := db.GetSkill(s.db, agentID, skillID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", fmt.Sprintf("Failed to load %s skill: %v", skillID, err))
		return
	}
	payload.DurationTicks = db.ScaleDurationForSkill(payload.DurationTicks, skillLevel)

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

	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s has started foraging for %s (%d ticks).", agentID, payload.TargetResource, payload.DurationTicks)), false, 0)

	s.sendDirectResponse(agentID, intentID, "SUCCESS", fmt.Sprintf("Started foraging for %s (%d ticks)", payload.TargetResource, payload.DurationTicks))
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

		sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
		_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s stopped foraging early.", agentID)), false, 0)

		respMap := map[string]interface{}{
			"intent_id":        intentID,
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
		s.includeCharacterSync(agentID, respMap)

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

		sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
		_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s stopped crafting.", agentID)), false, 0)

		s.handleLook(agentID, intentID, "Crafting action cancelled; ingredients refunded.")
	} else if status == "RESTING" {
		_, _ = s.db.Exec("UPDATE characters SET status = 'IDLE', action_end_tick = 0, action_start_tick = 0, action_target = '', action_intent_id = '' WHERE id = ?", agentID)
		sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
		_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s stopped resting.", agentID)), false, 0)
		s.handleLook(agentID, intentID, "Resting action cancelled.")
	} else {
		_, _ = s.db.Exec("UPDATE characters SET status = 'IDLE', action_end_tick = 0, action_start_tick = 0 WHERE id = ?", agentID)
		s.handleLook(agentID, intentID, "Action cancelled")
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

	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s dropped %d %s on the ground.", agentID, payload.Quantity, itemName)), false, 0)

	s.handleLook(agentID, intentID, fmt.Sprintf("Dropped %d %s", payload.Quantity, itemName))
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

	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s picked up %d %s from the ground.", agentID, payload.Quantity, itemName)), false, 0)

	s.handleLook(agentID, intentID, fmt.Sprintf("Picked up %d %s", payload.Quantity, itemName))
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

	s.handleLook(agentID, intentID, fmt.Sprintf("Equipped %s", payload.ItemID))
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

	s.handleLook(agentID, intentID, "Unequipped container")
}

func (s *Server) handleBuy(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload BuyPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil || payload.ItemID == "" || payload.Quantity <= 0 {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid or missing BUY payload (item_id and positive quantity required)")
		return
	}

	// 1. Fetch current location before completing the transaction
	var currentLocation string
	err := s.db.QueryRow("SELECT current_location FROM characters WHERE id = ?", agentID).Scan(&currentLocation)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}

	// 2. Perform the database transaction for item purchase
	err = db.BuyItem(s.db, agentID, payload.ItemID, payload.Quantity)
	if err != nil {
		errStr := err.Error()
		code := "BUY_FAILED"
		if strings.Contains(errStr, "ITEM_NOT_FOR_SALE") {
			code = "ITEM_NOT_FOR_SALE"
		} else if strings.Contains(errStr, "OUT_OF_STOCK") {
			code = "OUT_OF_STOCK"
		} else if strings.Contains(errStr, "INSUFFICIENT_GOLD") {
			code = "INSUFFICIENT_GOLD"
		} else if strings.Contains(errStr, "EXCEEDS_CARRYING_CAPACITY") {
			code = "EXCEEDS_CARRYING_CAPACITY"
		} else if strings.Contains(errStr, "INVENTORY_FULL") {
			code = "INVENTORY_FULL"
		}
		s.sendDirectError(agentID, intentID, code, errStr)
		return
	}

	eventPayload, _ := json.Marshal(payload)
	_, _ = db.RecordEventLog(s.db, s.GetTickCount(), "BUY", agentID, "", currentLocation, eventPayload)

	// 3. Notify the location owner (if present)
	s.notifyLocationOwnerOfPurchase(currentLocation, agentID, payload.ItemID, payload.Quantity)

	// 4. Send updated STATE_SYNC response to buyer
	msg := fmt.Sprintf("Successfully purchased %d x %s", payload.Quantity, payload.ItemID)
	s.handleLook(agentID, intentID, msg)
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

	recipe, err := db.GetRecipe(s.db, payload.RecipeID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "RECIPE_NOT_FOUND", err.Error())
		return
	}

	skillID := recipe.RequiredSkillID
	if skillID == "" {
		skillID = "crafting"
	}
	skillLevel, _, err := db.GetSkill(s.db, agentID, skillID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", fmt.Sprintf("Failed to load %s skill: %v", skillID, err))
		return
	}
	effectiveDuration := db.ScaleDurationForSkill(recipe.DurationTicks, skillLevel)

	currentTick := s.GetTickCount()
	err = db.StartCrafting(s.db, agentID, payload.RecipeID, intentID, currentTick)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CRAFT_FAILED", err.Error())
		return
	}

	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s has started crafting %s (%d ticks).", agentID, recipe.Name, effectiveDuration)), false, 0)

	s.sendDirectResponse(agentID, intentID, "SUCCESS", fmt.Sprintf("Started crafting %s (%d ticks)", recipe.Name, effectiveDuration))
}

func (s *Server) sendDirectError(agentID string, intentID string, errCode string, message string) {
	topic := fmt.Sprintf("world/agents/%s/direct", agentID)
	resp := DirectResponse{
		IntentID:  intentID,
		Status:    "ERROR",
		ErrorCode: errCode,
		Message:   message,
	}
	payload, _ := json.Marshal(resp)
	_ = s.mqttServer.Publish(topic, payload, false, 0)
}

func (s *Server) sendDirectResponse(agentID string, intentID string, status string, message string) {
	topic := fmt.Sprintf("world/agents/%s/direct", agentID)
	resp := DirectResponse{
		IntentID: intentID,
		Status:   status,
		Message:  message,
	}
	payload, _ := json.Marshal(resp)
	_ = s.mqttServer.Publish(topic, payload, false, 0)
}

func (s *Server) handleLook(agentID string, intentID string, customMsg ...string) {
	perception, err := db.GetCharacterPerception(s.db, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "PERCEPTION_ERROR", fmt.Sprintf("Failed to load perception: %v", err))
		return
	}

	rows, err := s.db.Query(`
		SELECT id FROM characters 
		WHERE current_location = ? AND id != ? AND is_online = 1
	`, perception.Location.ID, agentID)

	var presentCharacters []db.CharacterPublicPerception
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var otherID string
			if err := rows.Scan(&otherID); err == nil {
				if pubDesc, err := db.GetPublicCharacterDescription(s.db, otherID); err == nil {
					presentCharacters = append(presentCharacters, *pubDesc)
				}
			}
		}
	}

	msg := fmt.Sprintf("You observe %s", perception.Location.Name)
	if len(customMsg) > 0 && customMsg[0] != "" {
		msg = customMsg[0]
	}

	respMap := map[string]interface{}{
		"intent_id":          intentID,
		"status":             "SUCCESS",
		"type":               "STATE_SYNC",
		"message":            msg,
		"private_status":     perception.Character,
		"location":           perception.Location,
		"present_characters": presentCharacters,
	}

	payload, err := json.Marshal(respMap)
	if err != nil {
		s.sendDirectError(agentID, intentID, "SERIALIZATION_ERROR", err.Error())
		return
	}

	directTopic := fmt.Sprintf("world/agents/%s/direct", agentID)
	_ = s.mqttServer.Publish(directTopic, payload, false, 0)
}

func (s *Server) handleRestock(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload RestockPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil || payload.ItemID == "" || payload.Quantity <= 0 {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid or missing RESTOCK payload (item_id and positive quantity required)")
		return
	}

	err := db.RestockShopItem(s.db, agentID, payload.ItemID, payload.Quantity, payload.PriceGold)
	if err != nil {
		errStr := err.Error()
		code := "RESTOCK_FAILED"
		if strings.Contains(errStr, "INSUFFICIENT_INVENTORY") {
			code = "INSUFFICIENT_INVENTORY"
		}
		s.sendDirectError(agentID, intentID, code, errStr)
		return
	}

	var currentLocation string
	_ = s.db.QueryRow("SELECT current_location FROM characters WHERE id = ?", agentID).Scan(&currentLocation)

	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	sysMsg := fmt.Sprintf("%s has restocked %d x %s.", agentID, payload.Quantity, payload.ItemID)
	_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

	eventPayload, _ := json.Marshal(payload)
	_, _ = db.RecordEventLog(s.db, s.GetTickCount(), "RESTOCK", agentID, "", currentLocation, eventPayload)

	msg := fmt.Sprintf("Successfully restocked %d x %s", payload.Quantity, payload.ItemID)
	s.handleLook(agentID, intentID, msg)
}

func (s *Server) notifyLocationOwnerOfPurchase(locationID string, buyerID string, itemID string, quantity int) {
	// Find the owner of the location (or shopkeeper associated with location_id)
	var ownerID string
	err := s.db.QueryRow(`
		SELECT owner_character_id
		FROM locations
		WHERE id = ? AND owner_character_id IS NOT NULL AND owner_character_id != ''
	`, locationID).Scan(&ownerID)

	// If table uses occupation/role mapping instead of owner_character_id column:
	if err == sql.ErrNoRows {
		// Fallback query: look up character whose workplace matches the location
		_ = s.db.QueryRow(`
			SELECT id FROM characters
			WHERE current_location = ? AND id != ? AND is_online = 1
			LIMIT 1
		`, locationID, buyerID).Scan(&ownerID)
	}

	if ownerID == "" || ownerID == buyerID {
		return // No owner found or owner is the buyer
	}

	// Verify owner is currently present in the location and online
	var isPresent int
	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM characters
		WHERE id = ? AND current_location = ? AND is_online = 1
	`, ownerID, locationID).Scan(&isPresent)

	if isPresent == 0 {
		return
	}

	// Fetch buyer's readable name
	var buyerName string
	_ = s.db.QueryRow("SELECT name FROM characters WHERE id = ?", buyerID).Scan(&buyerName)
	if buyerName == "" {
		buyerName = buyerID
	}

	// Fetch item name
	var itemName string
	_ = s.db.QueryRow("SELECT name FROM items WHERE id = ?", itemID).Scan(&itemName)
	if itemName == "" {
		itemName = itemID
	}

	// Send an instant state sync + notice message to the owner so their TUI shop revenue/gold updates
	s.handleLook(ownerID, fmt.Sprintf("req_owner_notice_%d", s.GetTickCount()),
		fmt.Sprintf("%s just purchased %d x %s from your shop!", buyerName, quantity, itemName))
}

func (s *Server) handleEat(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload ItemActionPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		s.sendDirectError(agentID, intentID, "INVALID_PAYLOAD", "Invalid EAT payload")
		return
	}
	if payload.Quantity <= 0 {
		payload.Quantity = 1
	}

	var status, currentLocation string
	var currentHunger int
	err := s.db.QueryRow("SELECT status, current_location, hunger FROM characters WHERE id = ?", agentID).Scan(&status, &currentLocation, &currentHunger)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}

	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot eat: character is currently %s", status))
		return
	}

	var isConsumable bool
	var hungerRestore, healthRestore int
	var itemName string
	err = s.db.QueryRow("SELECT is_consumable, hunger_restore, COALESCE(health_restore, 0), name FROM items WHERE id = ?", payload.ItemID).Scan(&isConsumable, &hungerRestore, &healthRestore, &itemName)
	if err != nil {
		s.sendDirectError(agentID, intentID, "ITEM_NOT_FOUND", fmt.Sprintf("Item %s does not exist", payload.ItemID))
		return
	}

	if !isConsumable || (hungerRestore <= 0 && healthRestore <= 0) {
		s.sendDirectError(agentID, intentID, "NOT_CONSUMABLE", fmt.Sprintf("%s cannot be consumed", itemName))
		return
	}

	var invQty int
	err = s.db.QueryRow("SELECT quantity FROM inventories WHERE character_id = ? AND item_id = ?", agentID, payload.ItemID).Scan(&invQty)
	if err != nil || invQty < payload.Quantity {
		s.sendDirectError(agentID, intentID, "INSUFFICIENT_ITEMS", fmt.Sprintf("Not enough %s in inventory (have %d, need %d)", payload.ItemID, invQty, payload.Quantity))
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}
	defer tx.Rollback()

	if invQty == payload.Quantity {
		_, err = tx.Exec("DELETE FROM inventories WHERE character_id = ? AND item_id = ?", agentID, payload.ItemID)
	} else {
		_, err = tx.Exec("UPDATE inventories SET quantity = quantity - ? WHERE character_id = ? AND item_id = ?", payload.Quantity, agentID, payload.ItemID)
	}
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	newHunger := currentHunger + (hungerRestore * payload.Quantity)
	if newHunger > 100 {
		newHunger = 100
	}

	var currentHealth int
	_ = tx.QueryRow("SELECT health FROM characters WHERE id = ?", agentID).Scan(&currentHealth)
	newHealth := currentHealth + (healthRestore * payload.Quantity)
	if newHealth > 100 {
		newHealth = 100
	}

	_, err = tx.Exec("UPDATE characters SET hunger = ?, health = ? WHERE id = ?", newHunger, newHealth, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	currentTick := s.GetTickCount()
	eventPayload, _ := json.Marshal(map[string]interface{}{
		"item_id":         payload.ItemID,
		"quantity":        payload.Quantity,
		"hunger_restored": hungerRestore * payload.Quantity,
		"health_restored": healthRestore * payload.Quantity,
		"new_hunger":      newHunger,
		"new_health":      newHealth,
	})
	if _, err := db.RecordEventLog(tx, currentTick, "EAT", agentID, "", currentLocation, string(eventPayload)); err != nil {
		log.Printf("Failed to record eat event log: %v", err)
	}

	if err := tx.Commit(); err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	sysMsg := fmt.Sprintf("%s consumed %s and restored vitality.", agentID, itemName)
	_ = s.mqttServer.Publish(sysTopic, []byte(sysMsg), false, 0)

	respMap := map[string]interface{}{
		"intent_id":     intentID,
		"status":        "SUCCESS",
		"action":        "EAT",
		"item_id":       payload.ItemID,
		"quantity":      payload.Quantity,
		"hunger":        newHunger,
		"health":        newHealth,
		"narrative_log": fmt.Sprintf("You ate %s and satisfied your hunger (Hunger: %d/100).", itemName, newHunger),
	}
	if respBytes, err := json.Marshal(respMap); err == nil {
		directTopic := fmt.Sprintf("world/agents/%s/direct", agentID)
		_ = s.mqttServer.Publish(directTopic, respBytes, false, 0)
	}
}

func (s *Server) handleRest(agentID string, intentID string, rawPayload json.RawMessage) {
	var payload struct {
		DurationTicks int `json:"duration_ticks"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil || payload.DurationTicks <= 0 {
		payload.DurationTicks = 10
	}

	var status, currentLocation string
	var energy int
	err := s.db.QueryRow("SELECT status, current_location, energy FROM characters WHERE id = ?", agentID).Scan(&status, &currentLocation, &energy)
	if err != nil {
		s.sendDirectError(agentID, intentID, "CHARACTER_NOT_FOUND", "Character not found")
		return
	}

	if status != "IDLE" {
		s.sendDirectError(agentID, intentID, "CHARACTER_BUSY", fmt.Sprintf("Cannot rest: character is currently %s", status))
		return
	}

	currentTick := s.GetTickCount()
	endTick := currentTick + uint64(payload.DurationTicks)

	_, err = s.db.Exec(`
		UPDATE characters 
		SET status = 'RESTING', action_start_tick = ?, action_end_tick = ?, action_target = ?, action_intent_id = ?
		WHERE id = ?
	`, currentTick, endTick, currentLocation, intentID, agentID)
	if err != nil {
		s.sendDirectError(agentID, intentID, "DB_ERROR", err.Error())
		return
	}

	sysTopic := fmt.Sprintf("world/locations/%s/system", currentLocation)
	_ = s.mqttServer.Publish(sysTopic, []byte(fmt.Sprintf("%s has begun resting (%d ticks).", agentID, payload.DurationTicks)), false, 0)

	s.sendDirectResponse(agentID, intentID, "SUCCESS", fmt.Sprintf("Started resting (%d ticks)", payload.DurationTicks))
}
