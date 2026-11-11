package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"agentmud/internal/client/model"
)

// ParseAndRepairIntent parses raw text from inference output, extracts JSON,
// normalizes casing, and validates RFC 002 constraints.
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
		if err != nil {
			return intent, fmt.Errorf("failed to marshal intent payload: %w", err)
		}
		intent.Payload = payloadBytes
	}

	return ValidateIntent(intent)
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

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return Intent{}, err
		}

		rawOutput, err := engine.Generate(ctx, currentPrompt, opts)
		if err != nil {
			lastErr = fmt.Errorf("inference generation error (attempt %d): %w", attempt+1, err)
			continue
		}
		intent, err := ParseAndRepairIntent(rawOutput)
		if err == nil {
			if attempt > 0 {
				log.Printf("[Intent Repair] Success on retry attempt %d", attempt+1)
			}
			return intent, nil
		}

		log.Printf("[Intent Repair] Attempt %d failed validation: %v. Retrying with correction...", attempt+1, err)
		lastErr = err
		correction := fmt.Sprintf(
			"Your previous output failed validation: %s. Respond ONLY with valid JSON having 'thought', 'action', and 'payload'. Do not invent missing action parameters or substitute defaults; choose another action if the required information is unavailable.",
			err.Error(),
		)
		if intent.Action == "MOVE" {
			correction += " For MOVE, choose a target_location ID explicitly listed among the exits in your current observation; movement is only to a directly connected location. Never use a default destination, guess an ID, or target your current location. If no move is appropriate, choose another action."
		}
		if intent.Action == "TRAVEL" {
			correction += " For TRAVEL, choose the final destination's exact location ID; the system will plan each connected movement step."
		}
		currentPrompt = fmt.Sprintf("%s\n\n[CORRECTION REQUIRED]: %s", prompt, correction)
	}

	return Intent{}, fmt.Errorf("intent generation failed after %d attempts: %w", maxRetries+1, lastErr)
}
