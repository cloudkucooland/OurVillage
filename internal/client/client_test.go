package client

import (
	"encoding/json"
	"testing"
)

func TestBuildIntentMessage(t *testing.T) {
	tests := []struct {
		name       string
		action     ActionConfig
		wantAction string
		checkFunc  func(t *testing.T, payload map[string]interface{})
	}{
		{
			name: "MOVE action",
			action: ActionConfig{
				Action:    "MOVE",
				TargetLoc: "tavern",
			},
			wantAction: "MOVE",
			checkFunc: func(t *testing.T, p map[string]interface{}) {
				if p["target_location"] != "tavern" {
					t.Errorf("expected target_location tavern, got %v", p["target_location"])
				}
			},
		},
		{
			name: "CHAT action",
			action: ActionConfig{
				Action:  "CHAT",
				ChatMsg: "Hello village!",
			},
			wantAction: "CHAT",
		},
		{
			name: "TRANSFER action",
			action: ActionConfig{
				Action:    "TRANSFER",
				Recipient: "martha_barkeep",
				Amount:    10,
				Item:      "gold",
				Qty:       10,
			},
			wantAction: "TRANSFER",
			checkFunc: func(t *testing.T, p map[string]interface{}) {
				if p["recipient_id"] != "martha_barkeep" {
					t.Errorf("expected recipient martha_barkeep, got %v", p["recipient_id"])
				}
				if int(p["amount"].(float64)) != 10 {
					t.Errorf("expected amount 10, got %v", p["amount"])
				}
			},
		},
		{
			name: "FORAGE action",
			action: ActionConfig{
				Action:   "FORAGE",
				Resource: "herbs",
				Duration: 45,
			},
			wantAction: "FORAGE",
			checkFunc: func(t *testing.T, p map[string]interface{}) {
				if p["target_resource"] != "herbs" {
					t.Errorf("expected target_resource herbs, got %v", p["target_resource"])
				}
				if int(p["duration_ticks"].(float64)) != 45 {
					t.Errorf("expected duration_ticks 45, got %v", p["duration_ticks"])
				}
			},
		},
		{
			name: "CANCEL_ACTION",
			action: ActionConfig{
				Action: "CANCEL_ACTION",
			},
			wantAction: "CANCEL_ACTION",
		},
		{
			name: "DROP action",
			action: ActionConfig{
				Action: "DROP",
				Item:   "wood_log",
				Qty:    3,
			},
			wantAction: "DROP",
			checkFunc: func(t *testing.T, p map[string]interface{}) {
				if p["item_id"] != "wood_log" {
					t.Errorf("expected wood_log, got %v", p["item_id"])
				}
				if int(p["quantity"].(float64)) != 3 {
					t.Errorf("expected quantity 3, got %v", p["quantity"])
				}
			},
		},
		{
			name: "PICKUP action",
			action: ActionConfig{
				Action: "PICKUP",
				Item:   "apple",
				Qty:    2,
			},
			wantAction: "PICKUP",
			checkFunc: func(t *testing.T, p map[string]interface{}) {
				if p["item_id"] != "apple" {
					t.Errorf("expected apple, got %v", p["item_id"])
				}
			},
		},
		{
			name: "EQUIP action",
			action: ActionConfig{
				Action: "EQUIP",
				Item:   "iron_axe",
			},
			wantAction: "EQUIP",
			checkFunc: func(t *testing.T, p map[string]interface{}) {
				if p["item_id"] != "iron_axe" {
					t.Errorf("expected iron_axe, got %v", p["item_id"])
				}
			},
		},
		{
			name: "UNEQUIP action",
			action: ActionConfig{
				Action: "UNEQUIP",
			},
			wantAction: "UNEQUIP",
		},
		{
			name: "CRAFT action",
			action: ActionConfig{
				Action: "CRAFT",
				Recipe: "bake_bread",
			},
			wantAction: "CRAFT",
			checkFunc: func(t *testing.T, p map[string]interface{}) {
				if p["recipe_id"] != "bake_bread" {
					t.Errorf("expected recipe_id bake_bread, got %v", p["recipe_id"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bytes, err := BuildIntentMessage(tt.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var raw map[string]interface{}
			if err := json.Unmarshal(bytes, &raw); err != nil {
				t.Fatalf("failed to unmarshal generated json: %v", err)
			}

			if raw["action"] != tt.wantAction {
				t.Errorf("expected action %s, got %v", tt.wantAction, raw["action"])
			}
			if raw["intent_id"] == nil || raw["intent_id"] == "" {
				t.Error("expected non-empty intent_id")
			}

			if tt.checkFunc != nil {
				var payloadMap map[string]interface{}
				payloadBytes, err := json.Marshal(raw["payload"])
				if err != nil {
					t.Fatalf("failed to remarshal payload: %v", err)
				}
				if err := json.Unmarshal(payloadBytes, &payloadMap); err != nil {
					t.Fatalf("failed to unmarshal payload map: %v", err)
				}
				tt.checkFunc(t, payloadMap)
			}
		})
	}

	// Unknown action test
	_, err := BuildIntentMessage(ActionConfig{Action: "INVALID_ACTION"})
	if err == nil {
		t.Error("expected error for INVALID_ACTION, got nil")
	}
}

func TestNewEngine(t *testing.T) {
	// Default / mock
	cfg := DefaultConfig()
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating mock engine: %v", err)
	}
	if eng.Name() != "mock" {
		t.Errorf("expected engine name mock, got %s", eng.Name())
	}
	_ = eng.Close()

	// Cloud OpenAI
	cfg.Model.Provider = "cloud-openai"
	engOpenAI, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating cloud-openai engine: %v", err)
	}
	if engOpenAI.Name() != "cloud-openai" {
		t.Errorf("expected engine name cloud-openai, got %s", engOpenAI.Name())
	}
	_ = engOpenAI.Close()

	// Llama-pure without model path
	cfg.Model.Provider = "llama-pure"
	cfg.Model.LlamaPure.ModelPath = ""
	_, err = NewEngine(cfg)
	if err == nil {
		t.Error("expected error for llama-pure with empty model path, got nil")
	}

	// Unknown provider
	cfg.Model.Provider = "invalid-provider-xyz"
	_, err = NewEngine(cfg)
	if err == nil {
		t.Error("expected error for invalid provider, got nil")
	}
}
