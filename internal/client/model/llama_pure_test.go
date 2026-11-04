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
