package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentmud/internal/client/model"
)

// ParseAndRepairIntent parses raw text from inference output, extracts JSON,
// repairs casing, validates RFC 002 constraints, and applies fallback repairs if needed.
func ParseAndRepairIntent(rawText string) (Intent, error) {
	// 1. Sanitize and extract JSON using the GBNF regex/JSON repair step
	cleanJSON, err := model.SanitizeAndExtractJSON(rawText)
	if err != nil {
		return Intent{}, fmt.Errorf("failed to extract valid JSON: %w", err)
	}

	// 2. Decode into raw map
	var rawMap map[string]interface{}
	if err := json.Unmarshal([]byte(cleanJSON), &rawMap); err != nil {
		return Intent{}, fmt.Errorf("failed to unmarshal sanitized JSON: %w", err)
	}

	intent := Intent{
		IntentID: GenerateIntentID(),
	}

	if th, ok := rawMap["thought"].(string); ok {
		intent.Thought = th
	}

	if act, ok := rawMap["action"].(string); ok {
		intent.Action = strings.ToUpper(strings.TrimSpace(act))
	}

	if p, ok := rawMap["payload"]; ok {
		payloadBytes, err := json.Marshal(p)
		if err == nil {
			intent.Payload = payloadBytes
		}
	}
	if len(intent.Payload) == 0 {
		intent.Payload = json.RawMessage("{}")
	}

	// 3. Validate
	validated, err := ValidateIntent(intent)
	if err == nil {
		return validated, nil
	}

	// 4. Fallback repairs for common SLM omissions:
	// A. If CHAT has empty payload but non-empty thought, use thought as chat payload
	if intent.Action == "CHAT" && intent.Thought != "" {
		intent.Payload, _ = json.Marshal(intent.Thought)
		if fixed, fixErr := ValidateIntent(intent); fixErr == nil {
			return fixed, nil
		}
	}

	// B. If MOVE target is missing or malformed, default to town_square
	if intent.Action == "MOVE" {
		intent.Payload, _ = json.Marshal(map[string]string{"target_location": "town_square"})
		if fixed, fixErr := ValidateIntent(intent); fixErr == nil {
			return fixed, nil
		}
	}

	// C. If payload is missing for FORAGE, default to firewood
	if intent.Action == "FORAGE" {
		intent.Payload, _ = json.Marshal(map[string]interface{}{"target_resource": "firewood", "duration_ticks": 60})
		if fixed, fixErr := ValidateIntent(intent); fixErr == nil {
			return fixed, nil
		}
	}

	return intent, err
}

// GenerateIntentWithRetry generates model output using the InferenceEngine,
// parsing and validating the intent, and retrying with corrective instructions upon failure.
func GenerateIntentWithRetry(
	ctx context.Context,
	engine model.InferenceEngine,
	prompt string,
	opts model.GenerationOptions,
	maxRetries int,
) (Intent, error) {
	if maxRetries <= 0 {
		maxRetries = 2
	}

	currentPrompt := prompt
	var lastErr error
	var lastRawOutput string

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return Intent{}, err
		}

		rawOutput, err := engine.Generate(ctx, currentPrompt, opts)
		if err != nil {
			lastErr = fmt.Errorf("inference generation error (attempt %d): %w", attempt+1, err)
			continue
		}
		lastRawOutput = rawOutput

		intent, err := ParseAndRepairIntent(rawOutput)
		if err == nil {
			return intent, nil
		}

		lastErr = err
		// Append corrective instruction for the next retry attempt
		currentPrompt = fmt.Sprintf(
			"%s\n\n[CORRECTION REQUIRED - ATTEMPT %d]: Your previous output failed validation: %s. Respond ONLY with valid JSON having 'thought', 'action', and 'payload'.",
			prompt, attempt+1, err.Error(),
		)
	}

	// Ultimate fallback for SLMs when retries are exhausted:
	// If model outputted plain conversational text without JSON, wrap it as a CHAT intent.
	// If it was a JSON/intent generation attempt, fallback to a safe action like LOOK rather than leaking raw JSON into chat.
	trimmed := strings.TrimSpace(lastRawOutput)
	if trimmed != "" {
		isJsonAttempt := strings.Contains(trimmed, "{") || strings.Contains(trimmed, "\"action\"") || strings.Contains(trimmed, "\"thought\"") || strings.Contains(trimmed, "```")

		var fallbackIntent Intent
		if isJsonAttempt {
			fallbackIntent = Intent{
				IntentID: GenerateIntentID(),
				Thought:  "Fallback recovery from malformed output",
				Action:   "LOOK",
				Payload:  json.RawMessage("{}"),
			}
		} else {
			fallbackIntent = Intent{
				IntentID: GenerateIntentID(),
				Thought:  trimmed,
				Action:   "CHAT",
			}
			fallbackIntent.Payload, _ = json.Marshal(trimmed)
		}

		if validated, valErr := ValidateIntent(fallbackIntent); valErr == nil {
			return validated, nil
		}
	}

	return Intent{}, fmt.Errorf("intent generation failed after %d attempts: %w", maxRetries+1, lastErr)
}
