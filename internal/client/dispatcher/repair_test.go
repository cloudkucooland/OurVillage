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

	t.Run("Fallback repair: MOVE missing location repaired to town_square", func(t *testing.T) {
		input := `{"thought": "Need to wander", "action": "MOVE", "payload": {}}`
		intent, err := ParseAndRepairIntent(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(intent.Payload), "town_square") {
			t.Errorf("expected town_square fallback, got %s", string(intent.Payload))
		}
	})
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
