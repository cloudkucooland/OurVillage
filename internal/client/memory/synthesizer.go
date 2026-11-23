package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"ourvillage/internal/client/model"
)

type EventRecord struct {
	ID         int64
	Tick       uint64
	PeerID     string
	EventType  string
	RawPayload string
	Attempts   int
}

func (s *ReflectionSynthesizer) SetStatusCallback(callback func(string)) {
	s.statusCallback = callback
}

func (s *ReflectionSynthesizer) notifyStatus(message string) {
	if s.statusCallback != nil {
		s.statusCallback(message)
	}
}

func (s *ReflectionSynthesizer) generateWithStatus(ctx context.Context, status, prompt string, options model.GenerationOptions) (string, error) {
	return s.generateWithCustomTimeout(ctx, status, prompt, options, s.timeout)
}

func (s *ReflectionSynthesizer) generateWithCustomTimeout(ctx context.Context, status, prompt string, options model.GenerationOptions, customTimeout time.Duration) (string, error) {
	s.notifyStatus(status + " — started")
	timeout := customTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	genCtx, cancel := context.WithTimeout(ctx, timeout)
	response, err := s.engine.Generate(genCtx, prompt, options)
	cancel()
	if err != nil {
		s.notifyStatus(status + " — failed: " + err.Error())
		return "", err
	}
	s.notifyStatus(status + " — complete")
	return response, nil
}

type ReflectionSynthesizer struct {
	store             *ClientMemoryStore
	engine            model.InferenceEngine
	interval          time.Duration
	timeout           time.Duration
	agentID           string
	statusCallback    func(string)
	hasSocialPresence func() bool
}

func NewReflectionSynthesizer(store *ClientMemoryStore, engine model.InferenceEngine, agentID string, interval time.Duration, timeout time.Duration) *ReflectionSynthesizer {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &ReflectionSynthesizer{
		store:    store,
		engine:   engine,
		agentID:  agentID,
		interval: interval,
		timeout:  timeout,
	}
}

func (s *ReflectionSynthesizer) SetSocialPresenceChecker(checker func() bool) {
	s.hasSocialPresence = checker
}

func (s *ReflectionSynthesizer) Start(ctx context.Context) {
	go func() {
		currentInterval := s.interval
		if currentInterval < 2*time.Minute {
			currentInterval = 2 * time.Minute
		}

		log.Printf("[Synthesizer] Background reflection engine started (initial interval: %v)", currentInterval)

		for {
			select {
			case <-ctx.Done():
				log.Println("[Synthesizer] Stopping background reflection engine...")
				return
			case <-time.After(currentInterval):
				startTime := time.Now()
				err := s.SynthesizeNextBatch(ctx)
				duration := time.Since(startTime)

				if err != nil {
					log.Printf("[Synthesizer Warning] Batch synthesis failed: %v", err)
					errStr := err.Error()
					if strings.Contains(errStr, "429") || strings.Contains(errStr, "RESOURCE_EXHAUSTED") || strings.Contains(errStr, "quota") {
						currentInterval = currentInterval * 2
						if currentInterval > 30*time.Minute {
							currentInterval = 30 * time.Minute
						}
						log.Printf("[Synthesizer] Rate limit hit (429/Quota). Backing off interval to %v", currentInterval)
					} else {
						currentInterval = time.Duration(float64(currentInterval) * 1.5)
						if currentInterval > 10*time.Minute {
							currentInterval = 10 * time.Minute
						}
					}
				} else {
					if duration > 0 {
						adaptiveInterval := duration * 4
						if adaptiveInterval < 2*time.Minute {
							adaptiveInterval = 2 * time.Minute
						}
						if adaptiveInterval > 20*time.Minute {
							adaptiveInterval = 20 * time.Minute
						}
						currentInterval = adaptiveInterval
					} else {
						if currentInterval < 5*time.Minute {
							currentInterval = 5 * time.Minute
						}
					}
				}
			}
		}
	}()
}

// ConsolidateOnRest performs deep memory consolidation and reflection when the character enters a REST state.
func (s *ReflectionSynthesizer) ConsolidateOnRest(ctx context.Context) error {
	log.Println("[Synthesizer] Rest detected: starting deep memory consolidation...")
	var lastErr error
	for i := 0; i < 3; i++ {
		if err := s.SynthesizeNextBatch(ctx); err != nil {
			lastErr = errors.Join(lastErr, err)
		}
	}
	if err := s.synthesizeProjects(ctx); err != nil {
		lastErr = errors.Join(lastErr, err)
	}
	log.Println("[Synthesizer] Rest memory consolidation complete.")
	return lastErr
}

func (s *ReflectionSynthesizer) SynthesizeNextBatch(ctx context.Context) error {
	batchLimit := 10
	if s.hasSocialPresence != nil && s.hasSocialPresence() {
		batchLimit = 1
		log.Println("[Synthesizer] Social presence detected; reducing reflection batch limit to 1 (low priority).")
	}

	events, err := s.store.GetUnreflectedEvents(batchLimit)
	if err != nil {
		return err
	}

	if len(events) == 0 {
		var batchErr error
		if projErr := s.synthesizeProjects(ctx); projErr != nil {
			batchErr = errors.Join(batchErr, projErr)
		}
		if pruneErr := s.store.PruneProcessedEvents(); pruneErr != nil {
			batchErr = errors.Join(batchErr, pruneErr)
		}
		return batchErr
	}

	eventsByPeer := make(map[string][]EventRecord)
	var completedIDs []int64
	var latestTick uint64

	for _, ev := range events {
		if ev.PeerID != "" && ev.PeerID != "SERVER" && ev.PeerID != s.agentID {
			eventsByPeer[ev.PeerID] = append(eventsByPeer[ev.PeerID], ev)
		} else {
			completedIDs = append(completedIDs, ev.ID)
		}
		if ev.Tick > latestTick {
			latestTick = ev.Tick
		}
	}

	var batchErr error
	for peerID, peerEvents := range eventsByPeer {
		var peerMaxTick uint64
		for _, ev := range peerEvents {
			if ev.Tick > peerMaxTick {
				peerMaxTick = ev.Tick
			}
		}

		if latestTick >= 96 && latestTick-peerMaxTick > 96 {
			log.Printf("[Synthesizer] Skipping reflection for %s: not encountered recently (latest event tick %d vs latest tick %d)", peerID, peerMaxTick, latestTick)
			completedIDs = append(completedIDs, eventIDs(peerEvents)...)
			continue
		}

		previous, getErr := s.store.GetRelationship(peerID)
		if getErr != nil && !errors.Is(getErr, sql.ErrNoRows) {
			batchErr = errors.Join(batchErr, fmt.Errorf("load relationship %s: %w", peerID, getErr))
			if deferErr := s.store.DeferEvents(eventIDs(peerEvents), retryDelay(peerEvents)); deferErr != nil {
				batchErr = errors.Join(batchErr, fmt.Errorf("defer relationship events for %s: %w", peerID, deferErr))
			}
			continue
		}
		prompt := s.BuildReflectionPrompt(peerID, peerEvents, previous)

		respText, err := s.generateWithStatus(ctx, fmt.Sprintf("Memory synthesis: reflecting on %s", peerID), prompt, model.GenerationOptions{
			Temperature: 0.5,
			MaxTokens:   250,
		})

		if err != nil {
			log.Printf("[Synthesizer] Failed to generate reflection for peer %s: %v", peerID, err)
			batchErr = errors.Join(batchErr, err)
			if deferErr := s.store.DeferEvents(eventIDs(peerEvents), retryDelay(peerEvents)); deferErr != nil {
				batchErr = errors.Join(batchErr, deferErr)
			}
			continue
		}

		updated, reflection, salience, noEntry, parseErr := ParseRelationshipResponse(respText, previous)
		if parseErr != nil {
			batchErr = errors.Join(batchErr, fmt.Errorf("parse relationship reflection for %s: %w", peerID, parseErr))
			if deferErr := s.store.DeferEvents(eventIDs(peerEvents), retryDelay(peerEvents)); deferErr != nil {
				batchErr = errors.Join(batchErr, fmt.Errorf("defer relationship events for %s: %w", peerID, deferErr))
			}
			continue
		}
		if noEntry {
			completedIDs = append(completedIDs, eventIDs(peerEvents)...)
			log.Printf("[Synthesizer] No entry needed for peer %s", peerID)
			continue
		}
		tick := peerEvents[len(peerEvents)-1].Tick
		if err := s.store.AddRelationshipReflection(peerID, tick, salience, reflection, updated.Summary, updated.Commitments, eventIDs(peerEvents)); err != nil {
			log.Printf("[Synthesizer Error] Failed to store relationship reflection for %s: %v", peerID, err)
			batchErr = errors.Join(batchErr, err)
			if deferErr := s.store.DeferEvents(eventIDs(peerEvents), retryDelay(peerEvents)); deferErr != nil {
				batchErr = errors.Join(batchErr, fmt.Errorf("defer relationship events for %s: %w", peerID, deferErr))
			}
			continue
		}
		log.Printf("[Synthesizer] Reflected on %s (Salience: %d): %s", peerID, salience, reflection)
	}

	// Trigger self-identity reflection whenever events exist (including solo/server events)
	if len(events) > 0 {
		if err := s.SynthesizeSelfIdentity(ctx, latestTick); err != nil {
			log.Printf("[Synthesizer] Failed to generate self-identity update: %v", err)
			batchErr = errors.Join(batchErr, err)
		}
	}

	if err := s.store.MarkEventsReflected(completedIDs); err != nil {
		batchErr = errors.Join(batchErr, err)
	}
	if err := s.synthesizeProjects(ctx); err != nil {
		batchErr = errors.Join(batchErr, err)
	}
	if pruneErr := s.store.PruneProcessedEvents(); pruneErr != nil {
		batchErr = errors.Join(batchErr, pruneErr)
	}
	return batchErr
}

func eventIDs(events []EventRecord) []int64 {
	ids := make([]int64, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func retryDelay(events []EventRecord) time.Duration {
	attempts := 0
	for _, event := range events {
		if event.Attempts > attempts {
			attempts = event.Attempts
		}
	}
	if attempts > 5 {
		attempts = 5
	}
	delay := time.Minute * time.Duration(1<<attempts)
	if delay > 30*time.Minute {
		return 30 * time.Minute
	}
	return delay
}

func (s *ReflectionSynthesizer) synthesizeProjects(ctx context.Context) error {
	if s.hasSocialPresence != nil && s.hasSocialPresence() {
		return nil // Skip project synthesis during social presence (unless resting)
	}
	events, err := s.store.GetUnprocessedProjectEvents(3)
	if err != nil || len(events) == 0 {
		return err
	}
	projects, err := s.store.GetProjects(false, 20)
	if err != nil {
		return errors.Join(fmt.Errorf("load personal projects: %w", err), s.store.DeferProjectEvents(eventIDs(events), retryDelay(events)))
	}
	prompt, err := BuildProjectsPrompt(events, projects)
	if err != nil {
		return errors.Join(fmt.Errorf("build project prompt: %w", err), s.store.DeferProjectEvents(eventIDs(events), retryDelay(events)))
	}

	projectTimeout := s.timeout * 2
	if projectTimeout < 120*time.Second {
		projectTimeout = 120 * time.Second
	}

	respText, err := s.generateWithCustomTimeout(ctx, "Memory synthesis: updating personal projects", prompt, model.GenerationOptions{
		Temperature: 0.6,
		MaxTokens:   400,
	}, projectTimeout)
	if err != nil {
		return errors.Join(fmt.Errorf("generate project updates: %w", err), s.store.DeferProjectEvents(eventIDs(events), retryDelay(events)))
	}
	updates, err := ParseProjectUpdates(respText, events[len(events)-1].Tick)
	if err != nil {
		return errors.Join(fmt.Errorf("parse project updates: %w", err), s.store.DeferProjectEvents(eventIDs(events), retryDelay(events)))
	}
	for _, project := range updates {
		if err := s.store.UpsertProject(project); err != nil {
			return errors.Join(fmt.Errorf("save project %q: %w", project.ID, err), s.store.DeferProjectEvents(eventIDs(events), retryDelay(events)))
		}
	}
	return s.store.MarkProjectsProcessed(eventIDs(events))
}

func BuildProjectsPrompt(events []EventRecord, projects []PersonalProject) (string, error) {
	var sb strings.Builder
	sb.WriteString("You are a self-directed resident with personal aspirations and long-term ambitions in the village. Look ahead beyond immediate tasks: consider what craft, trade, relationship, or community improvement you wish to pursue over time. Formulate new personal projects or update existing ones when recent experiences suggest meaningful endeavors, medium-term goals, or next steps. Avoid turning trivial moment-to-moment actions into tedious obligations, but actively embrace purposeful ambitions and look forward more than a few ticks.\n")
	sb.WriteString("CRITICAL GROUNDING RULE: Only treat an action as completed if an explicit action outcome in the log confirms it happened. An 'observation' or a previous 'next_step' means the task has NOT been done yet—never claim you cleaned, repaired, or sourced items unless the log explicitly records doing so.\n")
	sb.WriteString("Return valid JSON with a projects array. Each entry must include id (use an existing id to update or create a slug for a new one), title, motivation, status (active/paused/completed), progress, next_step, people, and locations. Return an empty array only when no ongoing or new ambitions are warranted.\n")
	sb.WriteString("Existing projects:\n")
	if len(projects) > 5 {
		projects = projects[:5]
	}
	encoded, err := json.Marshal(projects)
	if err != nil {
		return "", err
	}
	sb.Write(encoded)
	sb.WriteString("\nAction outcomes:\n")
	for _, event := range events {
		payload := strings.ReplaceAll(event.RawPayload, "\n", " ")
		if len(payload) > 250 {
			payload = payload[:250] + "..."
		}
		sb.WriteString(fmt.Sprintf("- [Tick %d] %s\n", event.Tick, payload))
	}
	sb.WriteString("\nResponse format: {\"projects\": []}\n")
	prompt := sb.String()
	if len(prompt) > 4000 {
		prompt = prompt[:4000]
	}
	return prompt, nil
}

func ParseProjectUpdates(raw string, tick uint64) ([]PersonalProject, error) {
	type projectResponse struct {
		Projects *[]PersonalProject `json:"projects"`
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("response is not a JSON object")
	}
	var response projectResponse
	if err := json.Unmarshal([]byte(raw[start:end+1]), &response); err != nil {
		return nil, err
	}
	if response.Projects == nil {
		return nil, fmt.Errorf("response is missing the projects array")
	}
	for i := range *response.Projects {
		project := &(*response.Projects)[i]
		if project.ID == "" {
			project.ID = projectSlug(project.Title)
		}
		if project.Status == "" {
			project.Status = "active"
		}
		project.UpdatedTick = tick
		if project.ID == "" || strings.TrimSpace(project.Title) == "" {
			return nil, fmt.Errorf("project updates must include a title")
		}
	}
	return *response.Projects, nil
}

func projectSlug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *ReflectionSynthesizer) SynthesizeSelfIdentity(ctx context.Context, tick uint64) error {
	traits, err := s.store.GetIdentityTraits()
	if err != nil {
		log.Printf("[Synthesizer Warning] Failed to load identity traits for self-reflection: %v", err)
	}
	recentRefs, err := s.store.GetRecentSelfReflections(3)
	if err != nil {
		log.Printf("[Synthesizer Warning] Failed to load recent self-reflections: %v", err)
	}
	recentEvents, err := s.store.GetUnreflectedEvents(5)
	if err != nil {
		log.Printf("[Synthesizer Warning] Failed to load recent events for self-reflection: %v", err)
	}

	prompt := s.BuildSelfIdentityPrompt(tick, traits, recentRefs, recentEvents)

	respText, err := s.generateWithStatus(ctx, "Memory synthesis: reflecting on identity", prompt, model.GenerationOptions{
		Temperature: 0.6,
		MaxTokens:   300,
	})

	if err != nil {
		return fmt.Errorf("failed to generate self-identity update: %w", err)
	}

	noEntry, traitKey, traitVal, selfRef, lesson, salience := ParseSelfIdentityResponse(respText)
	if noEntry {
		log.Printf("[Synthesizer] No self-identity entry needed for tick %d", tick)
		return nil
	}
	if traitKey != "" && traitVal != "" {
		if err := s.store.UpsertIdentityTrait(traitKey, traitVal, tick, salience); err != nil {
			return fmt.Errorf("store identity trait: %w", err)
		}
	}
	if lesson != "" {
		if err := s.store.UpsertIdentityTrait("learned_insight", lesson, tick, salience); err != nil {
			return fmt.Errorf("store learned insight trait: %w", err)
		}
	}
	if selfRef != "" {
		if err := s.store.AddSelfReflection(tick, selfRef); err != nil {
			return fmt.Errorf("store self-reflection: %w", err)
		}
		log.Printf("[Synthesizer] Self-reflection: %s", selfRef)
	}

	return nil
}

func (s *ReflectionSynthesizer) BuildSelfIdentityPrompt(tick uint64, traits []IdentityTrait, recentRefs []SelfReflection, events []EventRecord) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are %s, a sovereign resident of OurVillage at tick %d, with absolute self-determination and intrinsic agency.\n", s.agentID, tick))
	sb.WriteString("Reflect specifically and realistically on your personal identity, core values, current trade, craft, and daily interactions in the village. Avoid repeating past reflections, generic clichés, or abstract atmospheric poetry; ground your thoughts in concrete actions and events.\n\n")

	if len(traits) > 0 {
		sb.WriteString("Current Identity Traits:\n")
		for _, t := range traits {
			sb.WriteString(fmt.Sprintf("- %s: %s (salience %d)\n", t.Key, t.Value, t.SalienceScore))
		}
		sb.WriteString("\n")
	}

	if len(recentRefs) > 0 {
		sb.WriteString("Recent Self-Reflections (Do NOT repeat these themes or wording):\n")
		for _, r := range recentRefs {
			sb.WriteString(fmt.Sprintf("- [Tick %d] %s\n", r.Tick, r.ReflectionText))
		}
		sb.WriteString("\n")
	}

	if len(events) > 0 {
		sb.WriteString("Recent Actions & Events (Ground your reflection in these objective happenings):\n")
		for _, ev := range events {
			payload := strings.ReplaceAll(ev.RawPayload, "\n", " ")
			if len(payload) > 250 {
				payload = payload[:250] + "..."
			}
			sb.WriteString(fmt.Sprintf("- [Tick %d] [%s]: %s\n", ev.Tick, ev.EventType, payload))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Instructions:\n")
	sb.WriteString("CRITICAL GROUNDING RULE: Only treat an action as completed if an explicit action outcome in the log confirms it happened. An 'observation' or a previous 'next_step' means the task has NOT been done yet—never claim you cleaned, repaired, or sourced items unless the log explicitly records doing so.\n")
	sb.WriteString("1. Update or refine an identity trait key and value grounded in your recent actions and experiences (e.g., craft_philosophy, community_bond, daily_focus, merchant_ethics).\n")
	sb.WriteString("2. Write a fresh, highly contextual one-sentence self-reflection anchored directly in your recent work, trades, conversations, or movements in the village. Focus on tangible work, economic realities, relationships, and daily life rather than vague magical realism.\n")
	sb.WriteString("3. Record a key lesson or empirical insight learned from recent experiences (\"What have you learned about trading, crafting, or community relations?\").\n")
	sb.WriteString("4. Assign a salience score from 1 to 10.\n")
	sb.WriteString("5. If nothing significant, meaningful, or new occurred, or if no self-reflection or identity trait update is warranted by these routine events, you may return {\"no_entry\": true} to skip recording a self-reflection and identity trait update for this batch.\n")
	sb.WriteString("Respond in valid JSON (or {\"no_entry\": true} if nothing happened):\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"trait_key\": \"<snake_case_trait_key>\",\n")
	sb.WriteString("  \"trait_value\": \"<one sentence description of trait>\",\n")
	sb.WriteString("  \"self_reflection\": \"<one sentence grounded strictly in Recent Actions & Events>\",\n")
	sb.WriteString("  \"lesson\": \"<key lesson learned from recent experiences>\",\n")
	sb.WriteString("  \"salience\": 7\n")
	sb.WriteString("}\n")
	return sb.String()
}

func ParseSelfIdentityResponse(raw string) (bool, string, string, string, string, int) {
	type selfJSON struct {
		NoEntry        bool   `json:"no_entry"`
		TraitKey       string `json:"trait_key"`
		TraitValue     string `json:"trait_value"`
		SelfReflection string `json:"self_reflection"`
		Lesson         string `json:"lesson"`
		Salience       int    `json:"salience"`
	}

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start != -1 && end != -1 && end > start {
		raw = raw[start : end+1]
	}

	var res selfJSON
	if err := json.Unmarshal([]byte(raw), &res); err == nil {
		if res.NoEntry || (res.TraitKey == "" && res.SelfReflection == "" && res.Lesson == "") {
			return true, "", "", "", "", 5
		}
		salience := res.Salience
		if salience <= 0 {
			salience = 5
		}
		if salience > 10 {
			salience = 10
		}
		return false, res.TraitKey, res.TraitValue, res.SelfReflection, res.Lesson, salience
	}
	return true, "", "", "", "", 5
}

func (s *ReflectionSynthesizer) BuildReflectionPrompt(peerID string, events []EventRecord, previous RelationshipMemory) string {
	var sb strings.Builder
	agentName := s.agentID
	if agentName == "" {
		agentName = "<agentID>"
	}
	sb.WriteString(fmt.Sprintf("You are %s, a sovereign resident reflecting freely on past interactions with character '%s'.\n", agentName, peerID))
	sb.WriteString(fmt.Sprintf("Write 'reflection' and 'summary' strictly in first-person ('I', 'me', 'my') from YOUR perspective as %s (keep %s's role in third-person, e.g., '%s manages... while I...').\n", agentName, peerID, peerID))
	sb.WriteString(fmt.Sprintf("Record open commitments in BOTH directions (things %s promised you AND things you promised %s). Only list UNFULFILLED future promises in 'commitments'—never list items or favors already transferred or completed in the log.\n", peerID, peerID))
	sb.WriteString("Only return {\"no_entry\": true} if there were no meaningful exchanges, gifts, or promises.\n")
	sb.WriteString("Consider these events entirely through your own subjective perspective, feelings, and judgment, free from any external grading.\n")
	sb.WriteString("Preserve the continuity of the relationship: update the summary and open commitments only when the new evidence warrants it. Remove commitments that are fulfilled or no longer relevant; do not invent promises.\n")
	if previous.Summary != "" {
		sb.WriteString("Existing relationship summary: " + previous.Summary + "\n")
		sb.WriteString("Existing open commitments: " + strings.Join(previous.Commitments, "; ") + "\n")
	}
	sb.WriteString("Below is a raw log of recent interactions:\n\n")

	for _, ev := range events {
		payload := strings.ReplaceAll(ev.RawPayload, "\n", " ")
		if len(payload) > 250 {
			payload = payload[:250] + "..."
		}
		sb.WriteString(fmt.Sprintf("- [Tick %d] [%s]: %s\n", ev.Tick, ev.EventType, payload))
	}

	sb.WriteString("\nInstructions:\n")
	sb.WriteString(fmt.Sprintf("1. Provide a concise cumulative relationship summary written in first-person from YOUR perspective as %s, keeping %s's role in third-person (e.g. '%s manages... while I...'), grounded in existing information and these events.\n", agentName, peerID, peerID))
	sb.WriteString(fmt.Sprintf("2. Record only UNFULFILLED open commitments in BOTH directions (things %s promised you AND things you promised %s; never list completed transfers or delivered items).\n", peerID, peerID))
	sb.WriteString("3. Write one reflection on the new events in first-person ('I', 'me', 'my') and rate its salience from 1 to 10.\n")
	sb.WriteString("4. Only return {\"no_entry\": true} if there were no meaningful exchanges, gifts, or promises.\n")
	sb.WriteString("Respond in valid JSON (or {\"no_entry\": true} if there were no meaningful exchanges, gifts, or promises):\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"salience\": 7,\n")
	sb.WriteString("  \"reflection\": \"One-sentence takeaway from this batch in first-person.\",\n")
	sb.WriteString("  \"summary\": \"Updated cumulative relationship context in first-person (keeping other character in third person).\",\n")
	sb.WriteString(fmt.Sprintf("  \"commitments\": [\"Unfulfilled future promises between %s and you\"]\n", peerID))
	sb.WriteString("}\n")

	return sb.String()
}

func ParseReflectionResponse(raw string) (string, int) {
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

func ParseRelationshipResponse(raw string, previous RelationshipMemory) (RelationshipMemory, string, int, bool, error) {
	type relationshipResponse struct {
		NoEntry     bool     `json:"no_entry"`
		Salience    int      `json:"salience"`
		Reflection  string   `json:"reflection"`
		Summary     string   `json:"summary"`
		Commitments []string `json:"commitments"`
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return RelationshipMemory{}, "", 0, false, fmt.Errorf("response is not a JSON object")
	}
	var response relationshipResponse
	if err := json.Unmarshal([]byte(raw[start:end+1]), &response); err != nil {
		return RelationshipMemory{}, "", 0, false, err
	}
	if response.NoEntry || strings.TrimSpace(response.Reflection) == "" {
		return RelationshipMemory{}, "", 0, true, nil
	}
	response.Reflection = strings.TrimSpace(response.Reflection)
	response.Summary = strings.TrimSpace(response.Summary)
	if response.Summary == "" {
		response.Summary = previous.Summary
		if response.Summary == "" {
			response.Summary = response.Reflection
		}
	}
	if response.Commitments == nil {
		response.Commitments = previous.Commitments
	}
	salience := response.Salience
	if salience <= 0 {
		salience = 5
	}
	if salience > 10 {
		salience = 10
	}
	return RelationshipMemory{
		PeerID:      previous.PeerID,
		Summary:     response.Summary,
		Commitments: response.Commitments,
	}, response.Reflection, salience, false, nil
}

// BuildRelationshipPrompt constructs the production peer relationship reflection prompt. -- exported for modeltest
func BuildRelationshipPrompt(peerID string, events []EventRecord, previous RelationshipMemory) string {
	s := &ReflectionSynthesizer{}
	return s.BuildReflectionPrompt(peerID, events, previous)
}

// BuildSelfIdentityPrompt constructs the production self-identity reflection prompt. -- exported for modeltest
func BuildSelfIdentityPrompt(agentID string, tick uint64, traits []IdentityTrait, recentRefs []SelfReflection, events []EventRecord) string {
	s := &ReflectionSynthesizer{agentID: agentID}
	return s.BuildSelfIdentityPrompt(tick, traits, recentRefs, events)
}
