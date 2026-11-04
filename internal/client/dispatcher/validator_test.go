package dispatcher

import (
	"encoding/json"
	"testing"
)

func TestValidateIntent_AllActions(t *testing.T) {
	tests := []struct {
		name       string
		intent     Intent
		wantErr    bool
		wantAction string
	}{
		{
			name: "Valid MOVE",
			intent: Intent{
				Action:  "move",
				Payload: json.RawMessage(`{"target_location": "tavern"}`),
			},
			wantErr:    false,
			wantAction: "MOVE",
		},
		{
			name: "MOVE with empty location",
			intent: Intent{
				Action:  "MOVE",
				Payload: json.RawMessage(`{"target_location": "   "}`),
			},
			wantErr: true,
		},
		{
			name: "Valid CHAT string payload",
			intent: Intent{
				Action:  "CHAT",
				Payload: json.RawMessage(`"Greetings!"`),
			},
			wantErr:    false,
			wantAction: "CHAT",
		},
		{
			name: "Valid CHAT object payload",
			intent: Intent{
				Action:  "CHAT",
				Payload: json.RawMessage(`{"message": "Hello!"}`),
			},
			wantErr:    false,
			wantAction: "CHAT",
		},
		{
			name: "CHAT with empty message",
			intent: Intent{
				Action:  "CHAT",
				Payload: json.RawMessage(`""`),
			},
			wantErr: true,
		},
		{
			name: "Valid TRANSFER with gold",
			intent: Intent{
				Action:  "TRANSFER",
				Payload: json.RawMessage(`{"recipient_id": "martha_barkeep", "amount": 10}`),
			},
			wantErr:    false,
			wantAction: "TRANSFER",
		},
		{
			name: "Valid TRANSFER with item",
			intent: Intent{
				Action:  "TRANSFER",
				Payload: json.RawMessage(`{"recipient_id": "martha_barkeep", "item_id": "wood_log", "quantity": 2}`),
			},
			wantErr:    false,
			wantAction: "TRANSFER",
		},
		{
			name: "Invalid TRANSFER without gold or item",
			intent: Intent{
				Action:  "TRANSFER",
				Payload: json.RawMessage(`{"recipient_id": "martha_barkeep", "amount": 0}`),
			},
			wantErr: true,
		},
		{
			name: "Valid FORAGE",
			intent: Intent{
				Action:  "FORAGE",
				Payload: json.RawMessage(`{"target_resource": "firewood"}`),
			},
			wantErr:    false,
			wantAction: "FORAGE",
		},
		{
			name: "FORAGE missing resource",
			intent: Intent{
				Action:  "FORAGE",
				Payload: json.RawMessage(`{"duration_ticks": 60}`),
			},
			wantErr: true,
		},
		{
			name: "Valid DROP",
			intent: Intent{
				Action:  "DROP",
				Payload: json.RawMessage(`{"item_id": "iron_ore", "quantity": 3}`),
			},
			wantErr:    false,
			wantAction: "DROP",
		},
		{
			name: "Valid PICKUP",
			intent: Intent{
				Action:  "PICKUP",
				Payload: json.RawMessage(`{"item_id": "iron_ore", "quantity": 1}`),
			},
			wantErr:    false,
			wantAction: "PICKUP",
		},
		{
			name: "Valid EQUIP",
			intent: Intent{
				Action:  "EQUIP",
				Payload: json.RawMessage(`{"item_id": "leather_backpack"}`),
			},
			wantErr:    false,
			wantAction: "EQUIP",
		},
		{
			name: "Valid UNEQUIP",
			intent: Intent{
				Action:  "UNEQUIP",
				Payload: json.RawMessage(`{}`),
			},
			wantErr:    false,
			wantAction: "UNEQUIP",
		},
		{
			name: "Valid CRAFT",
			intent: Intent{
				Action:  "CRAFT",
				Payload: json.RawMessage(`{"recipe_id": "bake_bread"}`),
			},
			wantErr:    false,
			wantAction: "CRAFT",
		},
		{
			name: "CRAFT missing recipe",
			intent: Intent{
				Action:  "CRAFT",
				Payload: json.RawMessage(`{}`),
			},
			wantErr: true,
		},
		{
			name: "Invalid Action Enum",
			intent: Intent{
				Action:  "EXPLODE_TOWN",
				Payload: json.RawMessage(`{}`),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ValidateIntent(tt.intent)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateIntent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if res.Action != tt.wantAction {
					t.Errorf("expected action %s, got %s", tt.wantAction, res.Action)
				}
				if res.IntentID == "" {
					t.Error("expected non-empty IntentID to be generated")
				}
			}
		})
	}
}
