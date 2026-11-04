package dispatcher

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Intent represents a normalized RFC 002 intent ready for MQTT dispatch.
type Intent struct {
	IntentID string          `json:"intent_id"`
	Thought  string          `json:"thought,omitempty"`
	Action   string          `json:"action"`
	Payload  json.RawMessage `json:"payload"`
}

// GenerateIntentID creates a unique intent correlation identifier.
func GenerateIntentID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// ValidActionTypes lists all supported RFC 002 action enums.
var ValidActionTypes = map[string]struct{}{
	"MOVE":          {},
	"CHAT":          {},
	"TRANSFER":      {},
	"FORAGE":        {},
	"COLLECT":       {},
	"CANCEL_ACTION": {},
	"DROP":          {},
	"PICKUP":        {},
	"EQUIP":         {},
	"UNEQUIP":       {},
	"CRAFT":         {},
	"LOOK":          {},
	"STATUS":        {},
	"BUY":           {},
	"RESTOCK":       {},
}

// IsValidAction checks if an action string is a recognized RFC 002 action.
func IsValidAction(action string) bool {
	_, ok := ValidActionTypes[strings.ToUpper(strings.TrimSpace(action))]
	return ok
}

// ValidateIntent validates the action enum and ensures required payload attributes are present.
// Returns a normalized Intent or descriptive error.
func ValidateIntent(intent Intent) (Intent, error) {
	if intent.IntentID == "" {
		intent.IntentID = GenerateIntentID()
	}

	actionUpper := strings.ToUpper(strings.TrimSpace(intent.Action))
	if !IsValidAction(actionUpper) {
		return intent, fmt.Errorf("invalid action enum %q: must be one of MOVE, CHAT, TRANSFER, FORAGE, COLLECT, CANCEL_ACTION, DROP, PICKUP, EQUIP, UNEQUIP, CRAFT, BUY, RESTOCK", intent.Action)
	}

	if actionUpper == "COLLECT" {
		actionUpper = "FORAGE"
	}
	intent.Action = actionUpper

	// Validate action-specific payload constraints
	switch actionUpper {
	case "RESTOCK":
		var p struct {
			ItemID    string `json:"item_id"`
			Quantity  int    `json:"quantity"`
			PriceGold int    `json:"price_gold"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid RESTOCK payload: %w", err)
		}
		if strings.TrimSpace(p.ItemID) == "" {
			return intent, fmt.Errorf("RESTOCK requires non-empty item_id")
		}
		if p.Quantity <= 0 {
			p.Quantity = 1
		}
		cleanPayload, _ := json.Marshal(map[string]interface{}{
			"item_id":    strings.TrimSpace(p.ItemID),
			"quantity":   p.Quantity,
			"price_gold": p.PriceGold,
		})
		intent.Payload = cleanPayload
	case "BUY":
		var p struct {
			ItemID   string `json:"item_id"`
			Quantity int    `json:"quantity"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid BUY payload: %w", err)
		}
		if strings.TrimSpace(p.ItemID) == "" {
			return intent, fmt.Errorf("BUY requires item_id")
		}
		if p.Quantity <= 0 {
			p.Quantity = 1
		}
		cleanPayload, _ := json.Marshal(map[string]interface{}{
			"item_id":  strings.TrimSpace(p.ItemID),
			"quantity": p.Quantity,
		})
		intent.Payload = cleanPayload
	case "MOVE":
		var p struct {
			TargetLocation string `json:"target_location"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid MOVE payload: %w", err)
		}
		if strings.TrimSpace(p.TargetLocation) == "" {
			return intent, fmt.Errorf("MOVE requires non-empty target_location")
		}
		// Re-marshal clean payload
		cleanPayload, _ := json.Marshal(map[string]string{
			"target_location": strings.TrimSpace(p.TargetLocation),
		})
		intent.Payload = cleanPayload

	case "CHAT":
		// Payload can be a raw string or an object {"message": string}
		var msgStr string
		var p struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err == nil && p.Message != "" {
			msgStr = p.Message
		} else if err := json.Unmarshal(intent.Payload, &msgStr); err != nil || msgStr == "" {
			return intent, fmt.Errorf("CHAT requires a non-empty message")
		}
		cleanPayload, _ := json.Marshal(strings.TrimSpace(msgStr))
		intent.Payload = cleanPayload

	case "TRANSFER":
		var p struct {
			RecipientID string `json:"recipient_id"`
			Amount      int    `json:"amount"`
			ItemID      string `json:"item_id"`
			Quantity    int    `json:"quantity"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid TRANSFER payload: %w", err)
		}
		if strings.TrimSpace(p.RecipientID) == "" {
			return intent, fmt.Errorf("TRANSFER requires non-empty recipient_id")
		}
		if p.Amount <= 0 && (strings.TrimSpace(p.ItemID) == "" || p.Quantity <= 0) {
			return intent, fmt.Errorf("TRANSFER requires either gold amount > 0 or item_id with quantity > 0")
		}

	case "FORAGE":
		var p struct {
			TargetResource string `json:"target_resource"`
			DurationTicks  int    `json:"duration_ticks"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid FORAGE payload: %w", err)
		}
		if strings.TrimSpace(p.TargetResource) == "" {
			return intent, fmt.Errorf("FORAGE requires non-empty target_resource")
		}
		if p.DurationTicks <= 0 {
			p.DurationTicks = 60
		}
		cleanPayload, _ := json.Marshal(p)
		intent.Payload = cleanPayload

	case "CANCEL_ACTION", "UNEQUIP", "LOOK", "STATUS":
		// Empty payload is valid
		if len(intent.Payload) == 0 {
			intent.Payload = json.RawMessage("{}")
		}

	case "DROP", "PICKUP":
		var p struct {
			ItemID   string `json:"item_id"`
			Quantity int    `json:"quantity"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid %s payload: %w", actionUpper, err)
		}
		if strings.TrimSpace(p.ItemID) == "" {
			return intent, fmt.Errorf("%s requires non-empty item_id", actionUpper)
		}
		if p.Quantity <= 0 {
			p.Quantity = 1
		}
		cleanPayload, _ := json.Marshal(p)
		intent.Payload = cleanPayload

	case "EQUIP":
		var p struct {
			ItemID string `json:"item_id"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid EQUIP payload: %w", err)
		}
		if strings.TrimSpace(p.ItemID) == "" {
			return intent, fmt.Errorf("EQUIP requires non-empty item_id")
		}
		cleanPayload, _ := json.Marshal(p)
		intent.Payload = cleanPayload

	case "CRAFT":
		var p struct {
			RecipeID string `json:"recipe_id"`
		}
		if err := json.Unmarshal(intent.Payload, &p); err != nil {
			return intent, fmt.Errorf("invalid CRAFT payload: %w", err)
		}
		if strings.TrimSpace(p.RecipeID) == "" {
			return intent, fmt.Errorf("CRAFT requires non-empty recipe_id")
		}
		cleanPayload, _ := json.Marshal(p)
		intent.Payload = cleanPayload
	}

	return intent, nil
}
