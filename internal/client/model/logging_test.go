package model

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestLoggingEngineRecordsGenerateAndStreamRequests(t *testing.T) {
	engine := NewMockEngine()
	engine.SetDefaultResponse("response")
	var output bytes.Buffer
	logged := NewLoggingEngine(engine, log.New(&output, "", 0))

	if _, err := logged.Generate(context.Background(), "héllo", GenerationOptions{MaxTokens: 42}); err != nil {
		t.Fatal(err)
	}
	if err := logged.Stream(context.Background(), "stream prompt", GenerationOptions{}, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}

	got := output.String()
	for _, expected := range []string{
		"method=Generate prompt_bytes=6 prompt_chars=5 max_tokens_requested=42",
		"method=Generate duration=",
		"method=Stream prompt_bytes=13 prompt_chars=13 max_tokens_requested=0",
		"method=Stream duration=",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("inference logs missing %q:\n%s", expected, got)
		}
	}
	if strings.Contains(got, "héllo") || strings.Contains(got, "stream prompt") {
		t.Errorf("inference logs should not contain prompt contents:\n%s", got)
	}
}
