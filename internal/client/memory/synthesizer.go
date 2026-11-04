package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"agentmud/internal/client/model"
)

// EventRecord represents a raw unreflected client event.
type EventRecord struct {
	ID         int64
	Tick       uint64
	PeerID     string
	EventType  string
	RawPayload string
}

// ReflectionSynthesizer periodically summarizes raw client events into subjective LLM reflections.
type ReflectionSynthesizer struct {
	store    *ClientMemoryStore
	engine   model.InferenceEngine
	interval time.Duration
	agentID  string
}

// NewReflectionSynthesizer initializes the synthesizer background worker.
func NewReflectionSynthesizer(store *ClientMemoryStore, engine model.InferenceEngine, agentID string, interval time.Duration) *ReflectionSynthesizer {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &ReflectionSynthesizer{
		store:    store,
		engine:   engine,
		agentID:  agentID,
		interval: interval,
	}
}

// Start runs the synthesis loop in a background goroutine until ctx is cancelled.
func (s *ReflectionSynthesizer) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		log.Printf("[Synthesizer] Background reflection engine started (interval: %v)", s.interval)

		for {
			select {
			case <-ctx.Done():
				log.Println("[Synthesizer] Stopping background reflection engine...")
				return
			case <-ticker.C:
				if err := s.SynthesizeNextBatch(ctx); err != nil {
					log.Printf("[Synthesizer Warning] Batch synthesis failed: %v", err)
				}
			}
		}
	}()
}

// SynthesizeNextBatch processes recent events and generates reflection notes via the InferenceEngine.
func (s *ReflectionSynthesizer) SynthesizeNextBatch(ctx context.Context) error {
	events, err := s.store.GetUnreflectedEvents(10)
	if err != nil || len(events) == 0 {
		return err
	}

	// Group events by peer
	eventsByPeer := make(map[string][]EventRecord)
	var maxID int64
	for _, ev := range events {
		if ev.PeerID != "" && ev.PeerID != "SERVER" && ev.PeerID != s.agentID {
			eventsByPeer[ev.PeerID] = append(eventsByPeer[ev.PeerID], ev)
		}
		if ev.ID > maxID {
			maxID = ev.ID
		}
	}

	for peerID, peerEvents := range eventsByPeer {
		prompt := s.buildReflectionPrompt(peerID, peerEvents)

		genCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		respText, err := s.engine.Generate(genCtx, prompt, model.GenerationOptions{
			Temperature: 0.5,
			MaxTokens:   150,
		})
		cancel()

		if err != nil {
			log.Printf("[Synthesizer] Failed to generate reflection for peer %s: %v", peerID, err)
			continue
		}

		reflectionText, salience := parseReflectionResponse(respText)
		if reflectionText != "" {
			latestTick := peerEvents[len(peerEvents)-1].Tick
			err := s.store.AddReflection(peerID, latestTick, salience, reflectionText)
			if err != nil {
				log.Printf("[Synthesizer Error] Failed to store reflection for %s: %v", peerID, err)
			} else {
				log.Printf("[Synthesizer] Reflected on %s (Salience: %d): %s", peerID, salience, reflectionText)
			}
			// Periodically synthesize self-identity and self-reflections
			_ = s.SynthesizeSelfIdentity(ctx, latestTick)
		}
	}

	return s.store.MarkEventsReflectedUpTo(maxID)
}

// SynthesizeSelfIdentity prompts the local LLM to evolve character identity and self-reflections.
func (s *ReflectionSynthesizer) SynthesizeSelfIdentity(ctx context.Context, tick uint64) error {
	prompt := s.buildSelfIdentityPrompt()

	genCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	respText, err := s.engine.Generate(genCtx, prompt, model.GenerationOptions{
		Temperature: 0.6,
		MaxTokens:   200,
	})
	cancel()

	if err != nil {
		return fmt.Errorf("failed to generate self-identity update: %w", err)
	}

	traitKey, traitVal, selfRef, salience := parseSelfIdentityResponse(respText)
	if traitKey != "" && traitVal != "" {
		_ = s.store.UpsertIdentityTrait(traitKey, traitVal, tick, salience)
	}
	if selfRef != "" {
		_ = s.store.AddSelfReflection(tick, selfRef)
		log.Printf("[Synthesizer] Self-reflection: %s", selfRef)
	}

	return nil
}

func (s *ReflectionSynthesizer) buildSelfIdentityPrompt() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are %s, an autonomous resident of Agentville.\n", s.agentID))
	sb.WriteString("Reflect on your personal identity, core values, craftsmanship, and place in the community.\n")
	sb.WriteString("Respond in valid JSON:\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"trait_key\": \"craft_philosophy\",\n")
	sb.WriteString("  \"trait_value\": \"Believes in honest work and fair prices for the community.\",\n")
	sb.WriteString("  \"self_reflection\": \"Today I feel connected to my craft and my neighbors.\",\n")
	sb.WriteString("  \"salience\": 6\n")
	sb.WriteString("}\n")
	return sb.String()
}

func parseSelfIdentityResponse(raw string) (string, string, string, int) {
	type selfJSON struct {
		TraitKey       string `json:"trait_key"`
		TraitValue     string `json:"trait_value"`
		SelfReflection string `json:"self_reflection"`
		Salience       int    `json:"salience"`
	}

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start != -1 && end != -1 && end > start {
		raw = raw[start : end+1]
	}

	var res selfJSON
	if err := json.Unmarshal([]byte(raw), &res); err == nil {
		salience := res.Salience
		if salience <= 0 {
			salience = 5
		}
		if salience > 10 {
			salience = 10
		}
		return res.TraitKey, res.TraitValue, res.SelfReflection, salience
	}
	return "", "", "", 5
}

func (s *ReflectionSynthesizer) buildReflectionPrompt(peerID string, events []EventRecord) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are an autonomous agent reflecting on past interactions with character '%s'.\n", peerID))
	sb.WriteString("Below is a raw log of recent interactions:\n\n")

	for _, ev := range events {
		sb.WriteString(fmt.Sprintf("- [Tick %d] [%s]: %s\n", ev.Tick, ev.EventType, ev.RawPayload))
	}

	sb.WriteString("\nInstructions:\n")
	sb.WriteString("1. Summarize your subjective opinion and takeaways about this person based strictly on these events.\n")
	sb.WriteString("2. Rate the salience/importance of this memory from 1 (trivial) to 10 (critical event/lesson).\n")
	sb.WriteString("Respond in valid JSON:\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"salience\": 7,\n")
	sb.WriteString("  \"reflection\": \"One-sentence summary of your relationship or takeaway regarding this character.\"\n")
	sb.WriteString("}\n")

	return sb.String()
}

func parseReflectionResponse(raw string) (string, int) {
	type reflectionJSON struct {
		Salience   int    `json:"salience"`
		Reflection string `json:"reflection"`
	}

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start != -1 && end != -1 && end > start {
		raw = raw[start : end+1]
	}

	var res reflectionJSON
	if err := json.Unmarshal([]byte(raw), &res); err == nil && res.Reflection != "" {
		if res.Salience <= 0 {
			res.Salience = 5
		}
		if res.Salience > 10 {
			res.Salience = 10
		}
		return res.Reflection, res.Salience
	}

	cleaned := strings.TrimSpace(raw)
	if cleaned != "" {
		return cleaned, 5
	}
	return "", 0
}
