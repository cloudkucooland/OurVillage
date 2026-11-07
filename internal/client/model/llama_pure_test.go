package model

import (
	"strings"
	"testing"
)

func TestNewLlamaPureEngine_MissingModelPath(t *testing.T) {
	_, err := NewLlamaPureEngine(LlamaPureEngineConfig{
		ModelPath: "",
	})
	if err == nil {
		t.Fatal("expected error when model_path is empty, got nil")
	}
	if !strings.Contains(err.Error(), "model_path cannot be empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewLlamaPureEngine_MissingLibrary(t *testing.T) {
	_, err := NewLlamaPureEngine(LlamaPureEngineConfig{
		ModelPath:   "/models/test.gguf",
		LibraryPath: "/nonexistent/libllama.so",
	})
	if err == nil {
		t.Fatal("expected error when library is missing, got nil")
	}
	if !strings.Contains(err.Error(), "failed to initialize libllama library") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildGenerateOptionsUsesConfiguredMaxTokens(t *testing.T) {
	engine := &LlamaPureEngine{config: LlamaPureEngineConfig{MaxTokens: 768}}
	if got := engine.buildGenerateOptions(GenerationOptions{}).MaxTokens; got != 768 {
		t.Fatalf("configured max tokens = %d, want 768", got)
	}
	if got := engine.buildGenerateOptions(GenerationOptions{MaxTokens: 96}).MaxTokens; got != 96 {
		t.Fatalf("request max tokens = %d, want override 96", got)
	}
}
