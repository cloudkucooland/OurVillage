package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultAgentIntentGrammar(t *testing.T) {
	grammar := DefaultAgentIntentGrammar()
	if err := ValidateGBNF(grammar); err != nil {
		t.Fatalf("DefaultAgentIntentGrammar failed validation: %v", err)
	}
	assertObjectChatPayload(t, grammar)
	assertTravelPayload(t, grammar)
	assertFarmingPayloads(t, grammar)
}

func TestLoadGrammarFile(t *testing.T) {
	// Find config/grammars/agent_intent.gbnf from current or parent directories
	path := "config/grammars/agent_intent.gbnf"
	if _, err := os.Stat(path); err != nil {
		// In test runner, working directory might be internal/client/model
		path = filepath.Join("..", "..", "..", "config", "grammars", "agent_intent.gbnf")
	}

	content, err := LoadGrammarFile(path)
	if err != nil {
		t.Fatalf("failed to load agent_intent.gbnf: %v", err)
	}
	if content == "" {
		t.Fatal("grammar file content is empty")
	}
	assertObjectChatPayload(t, content)
	assertTravelPayload(t, content)
	assertFarmingPayloads(t, content)
}

func assertFarmingPayloads(t *testing.T, grammar string) {
	t.Helper()
	for _, expected := range []string{
		`\"PLANT\"`,
		`\"HARVEST\"`,
		`plant-payload ::= "{" ws "\"plot_id\"" ws ":" ws string ws "," ws "\"crop_id\"" ws ":" ws string ws "}"`,
		`harvest-payload ::= "{" ws "\"plot_id\"" ws ":" ws string ws "}"`,
	} {
		if !strings.Contains(grammar, expected) {
			t.Errorf("grammar missing farming support %q", expected)
		}
	}
}

func assertTravelPayload(t *testing.T, grammar string) {
	t.Helper()
	for _, expected := range []string{
		`\"TRAVEL\"`,
		"payload ::= move-payload | travel-payload",
		`travel-payload ::= "{" ws "\"target_location\"" ws ":" ws string ws "}"`,
	} {
		if !strings.Contains(grammar, expected) {
			t.Errorf("grammar missing TRAVEL support %q", expected)
		}
	}
	if !strings.Contains(grammar, `\"DRINK\"`) {
		t.Error("grammar should include DRINK alias for EAT")
	}
}

func assertObjectChatPayload(t *testing.T, grammar string) {
	t.Helper()
	const chatPayloadRule = `chat-payload ::= "{" ws "\"message\"" ws ":" ws string ws "}"`
	if !strings.Contains(grammar, chatPayloadRule) {
		t.Fatalf("chat payload grammar must require a message object; missing %q", chatPayloadRule)
	}
}

func TestValidateGBNF_Errors(t *testing.T) {
	if err := ValidateGBNF(""); err == nil {
		t.Error("expected error for empty grammar, got nil")
	}

	if err := ValidateGBNF("noroot ::= [a-z]+"); err == nil {
		t.Error("expected error for grammar without root rule, got nil")
	}
}

func TestSanitizeAndExtractJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAction string
		wantErr    bool
	}{
		{
			name:       "Clean direct JSON",
			input:      `{"thought": "Heading to mill", "action": "MOVE", "payload": {"target_location": "mill"}}`,
			wantAction: "MOVE",
			wantErr:    false,
		},
		{
			name:       "Markdown code fence with lowercase action",
			input:      "Here is what I decided to do:\n```json\n{\n  \"thought\": \"Saying hi\",\n  \"action\": \"chat\",\n  \"payload\": \"Hi everyone!\"\n}\n```\nHope that helps!",
			wantAction: "CHAT",
			wantErr:    false,
		},
		{
			name:       "JSON surrounded by conversational hallucination",
			input:      "I will definitely forage some wood. {\"action\": \"FORAGE\", \"payload\": {\"target_resource\": \"firewood\"}} That was my thought.",
			wantAction: "FORAGE",
			wantErr:    false,
		},
		{
			name:       "JSON with unquoted action enum",
			input:      `{"thought": "Saying hi", "action": CHAT, "payload": "Hi!"}`,
			wantAction: "CHAT",
			wantErr:    false,
		},
		{
			name:       "Clean direct JSON with DRINK alias",
			input:      `{"thought": "Drinking water", "action": "DRINK", "payload": {"item_id": "water"}}`,
			wantAction: "EAT",
			wantErr:    false,
		},
		{
			name:    "Invalid action enum",
			input:   `{"action": "EXPLODE_TOWN", "payload": {}}`,
			wantErr: true,
		},
		{
			name:    "Missing action field",
			input:   `{"thought": "Just thinking..."}`,
			wantErr: true,
		},
		{
			name:    "Missing payload field",
			input:   `{"thought": "I should look around", "action": "LOOK"}`,
			wantErr: true,
		},
		{
			name:    "Null payload field",
			input:   `{"thought": "I should look around", "action": "LOOK", "payload": null}`,
			wantErr: true,
		},
		{
			name:    "No JSON present",
			input:   "I am not sure what to do today.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeAndExtractJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SanitizeAndExtractJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(got), &parsed); err != nil {
					t.Fatalf("returned string is not valid JSON: %v (string: %s)", err, got)
				}
				if parsed["action"] != tt.wantAction {
					t.Errorf("expected normalized action %s, got %v", tt.wantAction, parsed["action"])
				}
			}
		})
	}
}
