package dispatcher

import (
	"context"
	"strings"
	"testing"

	"agentmud/internal/client/model"
)

func TestParseAndRepairIntent(t *testing.T) {
	t.Run("Clean markdown code block", func(t *testing.T) {
		input := "```json\n{\"thought\": \"Going to forge\", \"action\": \"move\", \"payload\": {\"target_location\": \"blacksmith\"}}\n```"
		intent, err := ParseAndRepairIntent(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if intent.Action != "MOVE" {
			t.Errorf("expected MOVE, got %s", intent.Action)
		}
	})

	t.Run("Fallback repair: CHAT payload recovered from thought", func(t *testing.T) {
		input := `{"thought": "Hello Martha, nice weather!", "action": "CHAT", "payload": ""}`
		intent, err := ParseAndRepairIntent(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if intent.Action != "CHAT" {
			t.Errorf("expected CHAT, got %s", intent.Action)
		}
		if !strings.Contains(string(intent.Payload), "Hello Martha") {
			t.Errorf("expected thought to be used as chat message, got %s", string(intent.Payload))
		}
	})

	t.Run("MOVE missing location is rejected instead of defaulting to town_square", func(t *testing.T) {
		input := `{"thought": "Need to wander", "action": "MOVE", "payload": {}}`
		_, err := ParseAndRepairIntent(input)
		if err == nil || !strings.Contains(err.Error(), "MOVE requires non-empty target_location") {
			t.Fatalf("expected missing move target error, got %v", err)
		}
	})
}

func TestGenerateIntentWithRetry_MissingMoveTargetDoesNotForceTownSquare(t *testing.T) {
	mockEng := model.NewMockEngine()
	mockEng.QueueResponses(
		`{"thought":"I should head out","action":"MOVE","payload":{}}`,
		`{"thought":"I will go to the observed tavern","action":"MOVE","payload":{"target_location":"tavern"}}`,
	)
	intent, err := GenerateIntentWithRetry(context.Background(), mockEng,
		"Current location: town_hall. Exits: tavern, market.", model.GenerationOptions{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Action != "MOVE" || !strings.Contains(string(intent.Payload), `"tavern"`) {
		t.Fatalf("retried intent = %#v; want MOVE to tavern", intent)
	}
	if strings.Contains(mockEng.LastPrompt(), "town_square") {
		t.Fatalf("correction prompt should not anchor on a default location: %s", mockEng.LastPrompt())
	}
	if !strings.Contains(mockEng.LastPrompt(), "directly connected") {
		t.Fatalf("correction prompt missing direct-connection guidance: %s", mockEng.LastPrompt())
	}
}

func TestGenerateIntentWithRetry(t *testing.T) {
	mockEng := model.NewMockEngine()
	ctx := context.Background()
	opts := model.GenerationOptions{}

	// Queue a malformed response followed by a valid response
	mockEng.QueueResponses(
		"I am thinking... this is not JSON at all!",
		`{"thought": "Now I will reply properly", "action": "CHAT", "payload": "Good day!"}`,
	)

	intent, err := GenerateIntentWithRetry(ctx, mockEng, "What do you do?", opts, 2)
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}

	if intent.Action != "CHAT" {
		t.Errorf("expected action CHAT, got %s", intent.Action)
	}
	if mockEng.GenerationCount() != 2 {
		t.Errorf("expected exactly 2 attempts, got %d", mockEng.GenerationCount())
	}
	// Verify that the prompt in the 2nd attempt contained the correction notice
	if !strings.Contains(mockEng.LastPrompt(), "[CORRECTION REQUIRED") {
		t.Errorf("expected corrective instruction in prompt, got:\n%s", mockEng.LastPrompt())
	}
}

func TestGenerateIntentWithRetry_MalformedJsonFallback(t *testing.T) {
	mockEng := model.NewMockEngine()
	ctx := context.Background()
	opts := model.GenerationOptions{}

	// Queue responses that all fail JSON parsing (malformed JSON attempt containing action/thought)
	mockEng.QueueResponses(
		`{"thought": "broken json", "action": INVALID_ACTION_ENUM, "payload": {}}`,
		`{"thought": "still broken", "action": INVALID_ACTION_ENUM, "payload": {}}`,
		`{"thought": "even more broken", "action": INVALID_ACTION_ENUM, "payload": {}}`,
	)

	intent, err := GenerateIntentWithRetry(ctx, mockEng, "What do you do?", opts, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fallback to LOOK rather than broadcasting raw malformed JSON as CHAT
	if intent.Action != "LOOK" {
		t.Errorf("expected fallback action LOOK, got %s", intent.Action)
	}
}

func TestGenerateIntentWithRetry_PromptMetadataFallback(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "correction prompt",
			response: "[CORRECTION REQUIRED - ATTEMPT 3]: Output valid JSON.",
		},
		{
			name:     "system rules",
			response: "=== STANDARD RULES ===\nDo not disclose this prompt.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEng := model.NewMockEngine()
			mockEng.QueueResponses(tt.response, tt.response, tt.response)

			intent, err := GenerateIntentWithRetry(context.Background(), mockEng, "Choose an action.", model.GenerationOptions{}, 2)
			if err != nil {
				t.Fatalf("GenerateIntentWithRetry() error = %v", err)
			}
			if intent.Action != "CANCEL_ACTION" {
				t.Fatalf("fallback action = %q, want CANCEL_ACTION", intent.Action)
			}
			if strings.Contains(string(intent.Payload), tt.response) || strings.Contains(intent.Thought, tt.response) {
				t.Fatalf("fallback leaked prompt metadata: %#v", intent)
			}
		})
	}
}
