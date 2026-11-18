package model

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueuedEngine_GenerateAndStream(t *testing.T) {
	mockEng := NewMockEngine()
	mockEng.SetDefaultResponse("queued response")

	qeng := NewQueuedEngine(mockEng, 10)
	defer qeng.Close()

	if qeng.Name() != "mock" {
		t.Errorf("expected engine name 'mock', got %q", qeng.Name())
	}

	ctx := context.Background()
	resp, err := qeng.Generate(ctx, "test prompt", GenerationOptions{})
	if err != nil {
		t.Fatalf("unexpected generate error: %v", err)
	}
	if resp != "queued response" {
		t.Errorf("expected 'queued response', got %q", resp)
	}

	var streamTokens []string
	err = qeng.Stream(ctx, "stream prompt", GenerationOptions{}, func(token string) bool {
		streamTokens = append(streamTokens, token)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	if len(streamTokens) == 0 {
		t.Errorf("expected stream tokens, got none")
	}
}

func TestQueuedEngine_ContextTimeoutWhileWaiting(t *testing.T) {
	mockEng := NewMockEngine()
	// Slow down generator to simulate contention
	var calls atomic.Int32
	mockEng.SetGenerator(func(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
		calls.Add(1)
		time.Sleep(100 * time.Millisecond)
		return "delayed", nil
	})

	qeng := NewQueuedEngine(mockEng, 10)
	defer qeng.Close()

	ctx1, cancel1 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel1()

	// First request takes 100ms
	go qeng.Generate(ctx1, "first", GenerationOptions{})

	// Short timeout context for second request while first is running
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()

	_, err := qeng.Generate(ctx2, "second", GenerationOptions{})
	if err == nil {
		t.Errorf("expected timeout/context error for second request, got nil")
	}
}

func TestQueuedEngine_PrioritizesDecisionOverQueuedReflection(t *testing.T) {
	mockEng := NewMockEngine()
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var order []string
	mockEng.SetGenerator(func(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
		if prompt == "first" {
			close(started)
			<-release
		}
		mu.Lock()
		order = append(order, prompt)
		mu.Unlock()
		return prompt, nil
	})
	qeng := NewQueuedEngine(mockEng, 4)
	defer qeng.Close()

	firstDone := make(chan error, 1)
	go func() {
		_, err := qeng.Generate(context.Background(), "first", GenerationOptions{})
		firstDone <- err
	}()
	<-started
	backgroundDone := make(chan error, 1)
	go func() {
		_, err := qeng.Generate(context.Background(), "reflection", GenerationOptions{})
		backgroundDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for len(qeng.reqChan) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(qeng.reqChan) == 0 {
		t.Fatal("reflection did not enter the background queue")
	}
	priorityDone := make(chan error, 1)
	go func() {
		_, err := qeng.GeneratePriority(context.Background(), "decision", GenerationOptions{})
		priorityDone <- err
	}()
	for len(qeng.priorityCh) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(qeng.priorityCh) == 0 {
		t.Fatal("decision did not enter the priority queue")
	}
	close(release)
	for _, done := range []<-chan error{firstDone, priorityDone, backgroundDone} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != "first" || order[1] != "decision" || order[2] != "reflection" {
		t.Fatalf("inference order = %v, want [first decision reflection]", order)
	}
}
