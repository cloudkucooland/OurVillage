package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentmud/internal/client/model"
)

func TestReflectionSynthesizer_FailedPeerEventRemainsRetryable(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.AddEvent(12, "mira", "CHAT", "Mira offered help."); err != nil {
		t.Fatal(err)
	}
	engine := model.NewMockEngine()
	engine.SetGenerator(func(context.Context, string, model.GenerationOptions) (string, error) {
		return "", errors.New("temporary model failure")
	})
	synth := NewReflectionSynthesizer(store, engine, "elda", 0, 0)
	if err := synth.SynthesizeNextBatch(context.Background()); err == nil {
		t.Fatal("expected reflection generation error")
	}

	var processed int
	var attempts int
	var retryAfter string
	if err := store.db.QueryRow(`
		SELECT reflection_processed, attempt_count, retry_after
		FROM client_event_history
	`).Scan(&processed, &attempts, &retryAfter); err != nil {
		t.Fatal(err)
	}
	if processed != 0 || attempts != 1 || retryAfter == "" {
		t.Fatalf("event retry state = processed:%d attempts:%d retry_after:%q", processed, attempts, retryAfter)
	}
}

func TestReflectionSynthesizer_PersistsProjectUpdatesFromActionOutcomes(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	defer store.Close()
	if err := store.AddEvent(30, "elda", "ACTION_RESULT", `{"status":"SUCCESS","message":"Crafted a wooden sign."}`); err != nil {
		t.Fatal(err)
	}
	engine := model.NewMockEngine()
	engine.SetDefaultResponse(`{"projects":[{"id":"market-sign","title":"Make a sign for the market","motivation":"Help visitors find the stalls","status":"active","progress":"Crafted the sign","next_step":"Ask the market keeper where to hang it","people":["keeper"],"locations":["market"]}]}`)
	synth := NewReflectionSynthesizer(store, engine, "elda", 0, 0)
	var statuses []string
	synth.SetStatusCallback(func(status string) {
		statuses = append(statuses, status)
	})
	if err := synth.SynthesizeNextBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	hasProjectStarted := false
	hasProjectComplete := false
	for _, status := range statuses {
		if strings.Contains(status, "updating personal projects — started") {
			hasProjectStarted = true
		}
		if strings.Contains(status, "updating personal projects — complete") {
			hasProjectComplete = true
		}
	}
	if !hasProjectStarted || !hasProjectComplete {
		t.Fatalf("synthesis statuses = %#v; want project update start and completion", statuses)
	}
	projects, err := store.GetProjects(true, 10)
	if err != nil || len(projects) != 1 || projects[0].UpdatedTick != 30 {
		t.Fatalf("projects = %#v, err = %v", projects, err)
	}
	var processed int
	if err := store.db.QueryRow("SELECT project_processed FROM client_event_history").Scan(&processed); err != nil {
		// Event was pruned because reflection_processed = 1 and project_processed = 1
	} else if processed != 1 {
		t.Fatalf("project_processed = %d, err = %v", processed, err)
	}
}

func TestMemorySynthesis_ProjectsAreOptionalAndRelationshipsRemainCumulative(t *testing.T) {
	projects, err := parseProjectUpdates(`{"projects":[]}`, 7)
	if err != nil || len(projects) != 0 {
		t.Fatalf("empty project update = %#v, err = %v; want a valid no-op", projects, err)
	}
	if _, err := parseProjectUpdates(`{}`, 7); err == nil {
		t.Fatal("expected malformed project response to remain retryable")
	}

	previous := RelationshipMemory{
		PeerID: "mira", Summary: "We help each other restore the mill.",
		Commitments: []string{"Ask about timber"},
	}
	updated, reflection, _, err := parseRelationshipResponse(
		`{"reflection":"Mira shared a useful design.","salience":6}`, previous)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Summary != previous.Summary || len(updated.Commitments) != 1 || reflection == "" {
		t.Fatalf("updated relationship = %#v, reflection = %q", updated, reflection)
	}
}

func TestReflectionSynthesizer_PrunesProcessedEvents(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.AddEvent(10, "mira", "CHAT", "Hello"); err != nil {
		t.Fatal(err)
	}
	engine := model.NewMockEngine()
	engine.SetDefaultResponse(`{"reflection":"Met Mira.","salience":5,"summary":"Met Mira.","commitments":[]}`)
	synth := NewReflectionSynthesizer(store, engine, "elda", 0, 0)
	if err := synth.SynthesizeNextBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM client_event_history").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected event history to be pruned, got %d events remaining", count)
	}
}

func TestReflectionSynthesizer_SocialPresenceThrottling(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 1; i <= 3; i++ {
		if err := store.AddEvent(uint64(i), "mira", "CHAT", "Event"); err != nil {
			t.Fatal(err)
		}
	}
	engine := model.NewMockEngine()
	engine.SetDefaultResponse(`{"reflection":"Met Mira.","salience":5,"summary":"Met Mira.","commitments":[]}`)
	synth := NewReflectionSynthesizer(store, engine, "elda", 0, 0)
	synth.SetSocialPresenceChecker(func() bool {
		return true
	})
	if err := synth.SynthesizeNextBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM client_event_history").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 remaining events due to batch limit 1 throttling, got %d", count)
	}
}

func TestReflectionSynthesizer_SkipsStalePeersNotEncounteredRecently(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Old event for mira at tick 10 (>96 ticks older than latestTick 150)
	if err := store.AddEvent(10, "mira", "CHAT", "Old chat with Mira."); err != nil {
		t.Fatal(err)
	}
	// Recent event to push latestTick to 150
	if err := store.AddEvent(150, "gideon", "CHAT", "Recent chat with Gideon."); err != nil {
		t.Fatal(err)
	}

	engine := model.NewMockEngine()
	generateCalled := 0
	engine.SetGenerator(func(ctx context.Context, prompt string, opts model.GenerationOptions) (string, error) {
		generateCalled++
		if strings.Contains(prompt, "mira") {
			t.Fatal("should not generate reflection for stale peer mira")
		}
		return `{"reflection":"Gideon is working at the forge.","salience":5,"summary":"Gideon works hard.","commitments":[]}`, nil
	})

	synth := NewReflectionSynthesizer(store, engine, "elda", 0, 0)
	if err := synth.SynthesizeNextBatch(context.Background()); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM client_event_history").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected all events (including skipped old peer events) to be processed and pruned, got %d remaining", count)
	}
}

func TestReflectionSynthesizer_ConsolidateOnRest(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.AddEvent(40, "elda", "ACTION_RESULT", `{"status":"SUCCESS","message":"Purchased 10 vegetable seeds."}`); err != nil {
		t.Fatal(err)
	}
	engine := model.NewMockEngine()
	engine.SetDefaultResponse(`{"projects":[{"id":"gardening","title":"Grow vegetables","motivation":"Food security","status":"active","progress":"Purchased 10 vegetable seeds","next_step":"Plant seeds in garden plots","people":[],"locations":["garden"]}]}`)
	synth := NewReflectionSynthesizer(store, engine, "elda", 0, 0)
	if err := synth.ConsolidateOnRest(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := store.GetProjects(true, 10)
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects after rest consolidation = %#v, err = %v", projects, err)
	}
}

func TestReflectionSynthesizer_SelfIdentityWithLesson(t *testing.T) {
	store, err := NewClientMemoryStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.AddEvent(50, "elda", "ACTION_RESULT", `{"status":"SUCCESS","message":"Crafted iron sword."}`); err != nil {
		t.Fatal(err)
	}
	engine := model.NewMockEngine()
	engine.SetDefaultResponse(`{"trait_key":"craft_focus","trait_value":"Focusing on weapon smithing.","self_reflection":"Hammered iron into a sturdy blade today.","lesson":"Good swords require higher carbon charcoal and patience.","salience":8}`)
	synth := NewReflectionSynthesizer(store, engine, "elda", 0, 0)
	if err := synth.SynthesizeSelfIdentity(context.Background(), 50); err != nil {
		t.Fatal(err)
	}

	traits, err := store.GetIdentityTraits()
	if err != nil {
		t.Fatal(err)
	}
	foundInsight := false
	foundFocus := false
	for _, tr := range traits {
		if tr.Key == "learned_insight" && tr.Value == "Good swords require higher carbon charcoal and patience." {
			foundInsight = true
		}
		if tr.Key == "craft_focus" && tr.Value == "Focusing on weapon smithing." {
			foundFocus = true
		}
	}
	if !foundInsight || !foundFocus {
		t.Fatalf("traits = %#v; want learned_insight and craft_focus traits", traits)
	}

	refs, err := store.GetRecentSelfReflections(5)
	if err != nil || len(refs) != 1 || refs[0].ReflectionText != "Hammered iron into a sturdy blade today." {
		t.Fatalf("self reflections = %#v, err = %v", refs, err)
	}
}
