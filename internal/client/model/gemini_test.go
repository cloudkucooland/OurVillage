package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudGeminiEngine_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/v1beta/models/gemini-1.5-flash:generateContent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-gemini-key" {
			t.Errorf("expected api key test-gemini-key, got %s", r.URL.Query().Get("key"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [
				{
					"content": {
						"parts": [
							{
								"text": "{\"thought\": \"Testing gemini.\", \"action\": \"LOOK\", \"payload\": {}}"
							}
						]
					},
					"finishReason": "STOP"
				}
			]
		}`))
	}))
	defer server.Close()

	engine := NewCloudGeminiEngine(GeminiEngineConfig{
		APIBase:   server.URL,
		APIKey:    "test-gemini-key",
		ModelName: "gemini-1.5-flash",
	})

	if engine.Name() != "cloud-gemini" {
		t.Errorf("expected engine name cloud-gemini, got %s", engine.Name())
	}

	resp, err := engine.Generate(context.Background(), "Hello Gemini", GenerationOptions{Temperature: 0.7})
	if err != nil {
		t.Fatalf("unexpected generation error: %v", err)
	}

	expected := "{\"thought\": \"Testing gemini.\", \"action\": \"LOOK\", \"payload\": {}}"
	if resp != expected {
		t.Errorf("expected %q, got %q", expected, resp)
	}
}
