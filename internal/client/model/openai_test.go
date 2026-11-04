package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudOpenAIEngine_Generate(t *testing.T) {
	expectedPrompt := "What should Eldon do next?"
	expectedResponse := `{"thought": "I should buy flour", "action": "TRANSFER", "payload": {"recipient_id": "martha_barkeep", "amount": 2}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-api-key" {
			t.Errorf("expected Bearer test-api-key, got %s", auth)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if req.Model != "gpt-4o-mini" {
			t.Errorf("expected model gpt-4o-mini, got %s", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != expectedPrompt {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"choices": [
				{
					"message": {
						"content": %q
					},
					"finish_reason": "stop"
				}
			]
		}`, expectedResponse)
	}))
	defer server.Close()

	engine := NewCloudOpenAIEngine(OpenAIEngineConfig{
		APIBase:   server.URL + "/v1",
		APIKey:    "test-api-key",
		ModelName: "gpt-4o-mini",
	})
	defer engine.Close()

	if engine.Name() != "cloud-openai" {
		t.Errorf("expected name cloud-openai, got %s", engine.Name())
	}

	resp, err := engine.Generate(context.Background(), expectedPrompt, GenerationOptions{
		Temperature: 0.7,
		Grammar:     "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp != expectedResponse {
		t.Errorf("expected response %s, got %s", expectedResponse, resp)
	}
}

func TestCloudOpenAIEngine_Stream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":" village"}}]}`,
			`{"choices":[{"delta":{"content":"!"}}]}`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	engine := NewCloudOpenAIEngine(OpenAIEngineConfig{
		APIBase: server.URL,
	})
	defer engine.Close()

	var collected strings.Builder
	err := engine.Stream(context.Background(), "say hello", GenerationOptions{}, func(token string) bool {
		collected.WriteString(token)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error streaming: %v", err)
	}

	if collected.String() != "Hello village!" {
		t.Errorf("expected 'Hello village!', got %q", collected.String())
	}
}

func TestCloudOpenAIEngine_StreamEarlyStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		for i := 0; i < 10; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"token%d \"}}]}\n\n", i)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	engine := NewCloudOpenAIEngine(OpenAIEngineConfig{
		APIBase: server.URL,
	})
	defer engine.Close()

	count := 0
	err := engine.Stream(context.Background(), "test", GenerationOptions{}, func(token string) bool {
		count++
		return count < 3 // stop after 3 tokens
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count != 3 {
		t.Errorf("expected exactly 3 tokens received, got %d", count)
	}
}

func TestCloudOpenAIEngine_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": {"message": "Invalid API Key"}}`)
	}))
	defer server.Close()

	engine := NewCloudOpenAIEngine(OpenAIEngineConfig{
		APIBase: server.URL,
		APIKey:  "bad-key",
	})
	defer engine.Close()

	_, err := engine.Generate(context.Background(), "test", GenerationOptions{})
	if err == nil {
		t.Fatal("expected error on 401 Unauthorized, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected error to mention status 401, got: %v", err)
	}
}
