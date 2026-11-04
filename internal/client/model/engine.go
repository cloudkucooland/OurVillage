package model

import (
	"context"
)

// GenerationOptions parameterizes inference requests across all model backends.
type GenerationOptions struct {
	Temperature   float32
	TopP          float32
	MaxTokens     int
	StopSequences []string
	Grammar       string // Optional GBNF grammar or JSON schema constraint
}

// InferenceEngine defines the unified contract for all local and cloud model providers.
type InferenceEngine interface {
	// Name returns the provider identifier (e.g. "mock", "llama-pure", "cloud-openai").
	Name() string

	// Generate generates a complete text response given a structured prompt.
	Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error)

	// Stream generates tokens asynchronously, invoking onToken for each token.
	// Returning false from onToken stops generation early.
	Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(token string) bool) error

	// Close releases model weights, dynamic libraries, or network connections.
	Close() error
}
