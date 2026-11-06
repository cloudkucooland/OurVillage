package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GeminiEngineConfig configures the CloudGeminiEngine.
type GeminiEngineConfig struct {
	APIBase        string
	APIKey         string
	ModelName      string
	TimeoutSeconds int
	MaxTokens      int
	HTTPClient     *http.Client
}

// CloudGeminiEngine implements InferenceEngine for Google Gemini REST endpoints.
type CloudGeminiEngine struct {
	apiBase   string
	apiKey    string
	modelName string
	timeout   time.Duration
	maxTokens int
	client    *http.Client
}

// NewCloudGeminiEngine creates a new Google Gemini inference engine.
func NewCloudGeminiEngine(cfg GeminiEngineConfig) *CloudGeminiEngine {
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}

	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" {
		modelName = "gemini-3.5-flash-lite"
	}

	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 30
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

	return &CloudGeminiEngine{
		apiBase:   base,
		apiKey:    strings.TrimSpace(cfg.APIKey),
		modelName: modelName,
		timeout:   time.Duration(timeoutSec) * time.Second,
		maxTokens: maxTokens,
		client:    httpClient,
	}
}

func (c *CloudGeminiEngine) Name() string {
	return "cloud-gemini"
}

type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	GenerationConfig *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature      *float32 `json:"temperature,omitempty"`
	TopP             *float32 `json:"topP,omitempty"`
	MaxOutputTokens  int      `json:"maxOutputTokens,omitempty"`
	ResponseMimeType string   `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

func (c *CloudGeminiEngine) Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", c.apiBase, c.modelName, c.apiKey)

	var genConfig geminiGenerationConfig
	genConfig.MaxOutputTokens = c.maxTokens
	if opts.MaxTokens > 0 {
		genConfig.MaxOutputTokens = opts.MaxTokens
	}

	if opts.Temperature > 0 {
		t := opts.Temperature
		genConfig.Temperature = &t
	}
	if opts.TopP > 0 {
		p := opts.TopP
		genConfig.TopP = &p
	}

	if opts.Grammar != "" {
		genConfig.ResponseMimeType = "application/json"
	}

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: &genConfig,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create gemini http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("gemini api request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read gemini response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini api returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBytes, &gemResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal gemini response: %w (body: %s)", err, string(respBytes))
	}

	if gemResp.Error != nil {
		return "", fmt.Errorf("gemini api error [%d] %s: %s", gemResp.Error.Code, gemResp.Error.Status, gemResp.Error.Message)
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini api returned empty response candidates")
	}

	return gemResp.Candidates[0].Content.Parts[0].Text, nil
}

func (c *CloudGeminiEngine) Stream(ctx context.Context, prompt string, opts GenerationOptions, callback func(string) bool) error {
	text, err := c.Generate(ctx, prompt, opts)
	if err != nil {
		return err
	}
	callback(text)
	return nil
}

func (c *CloudGeminiEngine) Close() error {
	return nil
}
