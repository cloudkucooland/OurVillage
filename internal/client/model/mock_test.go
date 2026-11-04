package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestMockEngine_Name(t *testing.T) {
	eng := NewMockEngine()
	if eng.Name() != "mock" {
		t.Fatalf("expected name 'mock', got %s", eng.Name())
	}
}

func TestMockEngine_DefaultResponse(t *testing.T) {
	eng := NewMockEngine()
	ctx := context.Background()
	opts := GenerationOptions{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   128,
	}

	resp, err := eng.Generate(ctx, "You are in the tavern. What do you do?", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it parses as valid JSON with thought, action, payload
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("expected valid JSON response, got error: %v, body: %s", err, resp)
	}

	if parsed["action"] == nil || parsed["action"] == "" {
		t.Errorf("expected non-empty action in default response, got %v", parsed["action"])
	}
}

func TestMockEngine_QueueResponses(t *testing.T) {
	eng := NewMockEngine()
	ctx := context.Background()
	opts := GenerationOptions{}

	custom1 := `{"action": "MOVE", "payload": {"target_location": "blacksmith"}}`
	custom2 := `{"action": "FORAGE", "payload": {"target_resource": "iron_ore"}}`

	eng.QueueResponses(custom1, custom2)

	// First call should return custom1
	resp1, err := eng.Generate(ctx, "prompt 1", opts)
	if err != nil || resp1 != custom1 {
		t.Fatalf("expected custom1, got %s (err: %v)", resp1, err)
	}

	// Second call should return custom2
	resp2, err := eng.Generate(ctx, "prompt 2", opts)
	if err != nil || resp2 != custom2 {
		t.Fatalf("expected custom2, got %s (err: %v)", resp2, err)
	}

	// Third call should fall back to default
	resp3, err := eng.Generate(ctx, "prompt 3", opts)
	if err != nil || resp3 != DefaultMockResponse {
		t.Fatalf("expected DefaultMockResponse, got %s (err: %v)", resp3, err)
	}

	if eng.GenerationCount() != 3 {
		t.Errorf("expected generation count 3, got %d", eng.GenerationCount())
	}
	if eng.LastPrompt() != "prompt 3" {
		t.Errorf("expected last prompt 'prompt 3', got %s", eng.LastPrompt())
	}
}

func TestMockEngine_CustomGenerator(t *testing.T) {
	eng := NewMockEngine()
	ctx := context.Background()

	eng.SetGenerator(func(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
		if strings.Contains(prompt, "tavern") {
			return `{"action": "CHAT", "payload": "Ordering an ale!"}`, nil
		}
		return `{"action": "MOVE", "payload": {"target_location": "tavern"}}`, nil
	})

	resp1, err := eng.Generate(ctx, "You see Martha at the tavern bar.", GenerationOptions{})
	if err != nil || !strings.Contains(resp1, "Ordering an ale") {
		t.Fatalf("unexpected response: %s (err: %v)", resp1, err)
	}

	resp2, err := eng.Generate(ctx, "You are wandering the dark woods.", GenerationOptions{})
	if err != nil || !strings.Contains(resp2, "tavern") {
		t.Fatalf("unexpected response: %s (err: %v)", resp2, err)
	}
}

func TestMockEngine_Stream(t *testing.T) {
	eng := NewMockEngine()
	ctx := context.Background()
	opts := GenerationOptions{}

	eng.SetDefaultResponse("Hello world, this is a streaming test.")

	var collected []string
	err := eng.Stream(ctx, "say hello", opts, func(token string) bool {
		collected = append(collected, token)
		return true // continue
	})
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}

	joined := strings.Join(collected, "")
	if joined != "Hello world, this is a streaming test." {
		t.Errorf("expected joined stream tokens to match source text, got: %q", joined)
	}

	// Test early abort from callback
	var earlyTokens []string
	err = eng.Stream(ctx, "say hello again", opts, func(token string) bool {
		earlyTokens = append(earlyTokens, token)
		return len(earlyTokens) < 2 // stop after 2 tokens
	})
	if err != nil {
		t.Fatalf("unexpected stream error with early stop: %v", err)
	}
	if len(earlyTokens) != 2 {
		t.Errorf("expected exactly 2 tokens before early stop, got %d", len(earlyTokens))
	}
}

func TestMockEngine_ContextCancellation(t *testing.T) {
	eng := NewMockEngine()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := eng.Generate(ctx, "test prompt", GenerationOptions{})
	if err == nil {
		t.Fatal("expected context error on cancelled context, got nil")
	}

	err = eng.Stream(ctx, "test stream", GenerationOptions{}, func(token string) bool {
		return true
	})
	if err == nil {
		t.Fatal("expected context error on cancelled stream, got nil")
	}
}

func TestMockEngine_Closed(t *testing.T) {
	eng := NewMockEngine()
	if err := eng.Close(); err != nil {
		t.Fatalf("unexpected error closing engine: %v", err)
	}

	_, err := eng.Generate(context.Background(), "test", GenerationOptions{})
	if err != ErrEngineClosed {
		t.Fatalf("expected ErrEngineClosed, got %v", err)
	}
}

func TestMockEngine_Reset(t *testing.T) {
	eng := NewMockEngine()
	eng.QueueResponses("custom")
	_, _ = eng.Generate(context.Background(), "p1", GenerationOptions{})

	if eng.GenerationCount() != 1 {
		t.Fatalf("expected count 1, got %d", eng.GenerationCount())
	}

	eng.Reset()

	if eng.GenerationCount() != 0 {
		t.Fatalf("expected count 0 after reset, got %d", eng.GenerationCount())
	}
	if eng.LastPrompt() != "" {
		t.Fatalf("expected empty last prompt after reset, got %s", eng.LastPrompt())
	}
}

func TestMockEngine_ConcurrentUsage(t *testing.T) {
	eng := NewMockEngine()
	ctx := context.Background()

	var wg sync.WaitGroup
	workers := 16
	callsPerWorker := 25

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < callsPerWorker; i++ {
				prompt := fmt.Sprintf("worker %d call %d", id, i)
				_, err := eng.Generate(ctx, prompt, GenerationOptions{})
				if err != nil {
					t.Errorf("worker %d failed: %v", id, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()

	expectedTotal := workers * callsPerWorker
	if eng.GenerationCount() != expectedTotal {
		t.Errorf("expected %d total generations, got %d", expectedTotal, eng.GenerationCount())
	}
}
