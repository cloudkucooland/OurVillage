package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAIEngineConfig struct {
	APIBase        string
	APIKey         string
	ModelName      string
	TimeoutSeconds int
	MaxTokens      int
	HTTPClient     *http.Client
}

type CloudOpenAIEngine struct {
	apiBase   string
	apiKey    string
	modelName string
	timeout   time.Duration
	maxTokens int
	client    *http.Client
}

func NewCloudOpenAIEngine(cfg OpenAIEngineConfig) *CloudOpenAIEngine {
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}

	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 15
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		}
	}

	return &CloudOpenAIEngine{
		apiBase:   base,
		apiKey:    strings.TrimSpace(cfg.APIKey),
		modelName: modelName,
		timeout:   time.Duration(timeoutSec) * time.Second,
		maxTokens: maxTokens,
		client:    httpClient,
	}
}

func (c *CloudOpenAIEngine) Name() string {
	return "cloud-openai"
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    *float32        `json:"temperature,omitempty"`
	TopP           *float32        `json:"top_p,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	Stream         bool            `json:"stream"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *CloudOpenAIEngine) buildEndpoint() string {
	if strings.HasSuffix(c.apiBase, "/chat/completions") {
		return c.apiBase
	}
	return c.apiBase + "/chat/completions"
}

func (c *CloudOpenAIEngine) preparePayload(prompt string, opts GenerationOptions, stream bool) chatRequest {
	req := chatRequest{
		Model: c.modelName,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Stream: stream,
	}

	if opts.MaxTokens > 0 {
		req.MaxTokens = opts.MaxTokens
	} else {
		req.MaxTokens = c.maxTokens
	}

	if opts.Temperature > 0 {
		t := opts.Temperature
		req.Temperature = &t
	}
	if opts.TopP > 0 {
		p := opts.TopP
		req.TopP = &p
	}

	if len(opts.StopSequences) > 0 {
		req.Stop = opts.StopSequences
	}

	if opts.Grammar != "" {
		req.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	return req
}

func (c *CloudOpenAIEngine) Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	reqBody := c.preparePayload(prompt, opts, false)
	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := c.buildEndpoint()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		return "", fmt.Errorf("failed to decode response json: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("openai api error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("openai returned empty choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}

func (c *CloudOpenAIEngine) Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(token string) bool) error {
	reqBody := c.preparePayload(prompt, opts, true)
	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal streaming request: %w", err)
	}

	endpoint := c.buildEndpoint()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai streaming request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai streaming api error (status %d): %s", resp.StatusCode, string(errBytes))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}

		if chunk.Error != nil {
			return fmt.Errorf("streaming error from provider: %s", chunk.Error.Message)
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if !onToken(choice.Delta.Content) {
					return nil
				}
			}
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("error reading sse stream: %w", err)
	}

	return nil
}

func (c *CloudOpenAIEngine) Close() error {
	if tr, ok := c.client.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
	return nil
}
