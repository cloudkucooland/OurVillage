package model

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var (
	// ErrEngineClosed is returned when an operation is performed on a closed engine.
	ErrEngineClosed = errors.New("inference engine is closed")

	// DefaultMockResponse is an RFC 002 compliant intent JSON payload returned by default.
	DefaultMockResponse = `{"thought": "I should observe the village square and say hello.", "action": "CHAT", "payload": "Good day, fellow villagers!"}`
)

// MockEngine is a deterministic stub inference engine for automated testing and CI/CD.
type MockEngine struct {
	mu              sync.RWMutex
	closed          bool
	defaultResponse string
	responseQueue   []string
	customGen       func(ctx context.Context, prompt string, opts GenerationOptions) (string, error)
	prompts         []string
}

// NewMockEngine creates a new initialized MockEngine with standard default response.
func NewMockEngine() *MockEngine {
	return &MockEngine{
		defaultResponse: DefaultMockResponse,
		responseQueue:   make([]string, 0),
		prompts:         make([]string, 0),
	}
}

// Name returns the provider identifier "mock".
func (m *MockEngine) Name() string {
	return "mock"
}

// SetDefaultResponse sets the fallback response returned when no queued responses remain.
func (m *MockEngine) SetDefaultResponse(resp string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultResponse = resp
}

// QueueResponses appends one or more canned responses to be returned in FIFO order.
func (m *MockEngine) QueueResponses(responses ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responseQueue = append(m.responseQueue, responses...)
}

// SetGenerator configures a dynamic generator callback for custom test evaluation.
func (m *MockEngine) SetGenerator(fn func(ctx context.Context, prompt string, opts GenerationOptions) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customGen = fn
}

// LastPrompt returns the most recent prompt passed to Generate or Stream, or empty string.
func (m *MockEngine) LastPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.prompts) == 0 {
		return ""
	}
	return m.prompts[len(m.prompts)-1]
}

// AllPrompts returns a copy of all prompts received by this engine.
func (m *MockEngine) AllPrompts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.prompts))
	copy(result, m.prompts)
	return result
}

// GenerationCount returns the total number of inference requests processed.
func (m *MockEngine) GenerationCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.prompts)
}

// Reset clears recorded prompts and queued responses while preserving the default response.
func (m *MockEngine) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = make([]string, 0)
	m.responseQueue = make([]string, 0)
	m.customGen = nil
	m.closed = false
}

// Generate returns a simulated model response.
func (m *MockEngine) Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrEngineClosed
	}

	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return "", err
	}

	m.prompts = append(m.prompts, prompt)

	if m.customGen != nil {
		gen := m.customGen
		m.mu.Unlock()
		return gen(ctx, prompt, opts)
	}

	var resp string
	if len(m.responseQueue) > 0 {
		resp = m.responseQueue[0]
		m.responseQueue = m.responseQueue[1:]
	} else {
		resp = m.defaultResponse
	}
	m.mu.Unlock()

	return resp, nil
}

// Stream simulates token-by-token emission of the response over onToken.
func (m *MockEngine) Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(token string) bool) error {
	fullText, err := m.Generate(ctx, prompt, opts)
	if err != nil {
		return err
	}

	// Tokenize into words with trailing whitespace to simulate realistic token streaming
	tokens := tokenizeString(fullText)
	for _, tok := range tokens {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !onToken(tok) {
			// Consumer aborted early
			break
		}
	}

	return nil
}

// Close releases the mock engine resources.
func (m *MockEngine) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// tokenizeString splits text into words preserving whitespace tokens for streaming simulation.
func tokenizeString(s string) []string {
	if s == "" {
		return nil
	}

	var tokens []string
	var current strings.Builder

	for _, r := range s {
		current.WriteRune(r)
		if r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == '!' || r == '?' || r == '}' || r == '{' {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}
