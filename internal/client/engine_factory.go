package client

import (
	"fmt"
	"strings"

	"agentmud/internal/client/model"
)

// NewEngine instantiates the model.InferenceEngine configured in cfg.
func NewEngine(cfg *Config) (model.InferenceEngine, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Model.Provider))
	switch provider {
	case "mock", "":
		return model.NewMockEngine(), nil

	case "cloud-openai", "openai":
		return model.NewCloudOpenAIEngine(model.OpenAIEngineConfig{
			APIBase:        cfg.Model.Cloud.APIBase,
			APIKey:         cfg.Model.Cloud.APIKey,
			ModelName:      cfg.Model.Cloud.ModelName,
			TimeoutSeconds: cfg.Model.Cloud.TimeoutSeconds,
			MaxTokens:      cfg.Model.Cloud.MaxTokens,
		}), nil

	case "llama-pure", "llama":
		return model.NewLlamaPureEngine(model.LlamaPureEngineConfig{
			LibraryPath:   cfg.Model.LlamaPure.LibraryPath,
			ModelPath:     cfg.Model.LlamaPure.ModelPath,
			ContextSize:   cfg.Model.LlamaPure.ContextSize,
			Threads:       cfg.Model.LlamaPure.Threads,
			GPULayers:     cfg.Model.LlamaPure.GPULayers,
			MaxTokens:     cfg.Model.LlamaPure.MaxTokens,
			Temperature:   float32(cfg.Model.LlamaPure.Temperature),
			TopP:          float32(cfg.Model.LlamaPure.TopP),
			RepeatPenalty: float32(cfg.Model.LlamaPure.RepeatPenalty),
			GrammarFile:   cfg.Model.LlamaPure.GrammarFile,
		})

	case "rkllm":
		return model.NewRkllmEngine(model.RkllmEngineConfig{
			LibraryPath:      cfg.Model.Rkllm.LibraryPath,
			ModelPath:        cfg.Model.Rkllm.ModelPath,
			ContextSize:      cfg.Model.Rkllm.ContextSize,
			Temperature:      float32(cfg.Model.Rkllm.Temperature),
			TopP:             float32(cfg.Model.Rkllm.TopP),
			TopK:             cfg.Model.Rkllm.TopK,
			RepeatPenalty:    float32(cfg.Model.Rkllm.RepeatPenalty),
			FrequencyPenalty: float32(cfg.Model.Rkllm.FrequencyPenalty),
			PresencePenalty:  float32(cfg.Model.Rkllm.PresencePenalty),
			NKeep:            cfg.Model.Rkllm.NKeep,
			SkipSpecialToken: cfg.Model.Rkllm.SkipSpecialToken,
			IgnoreEosToken:   cfg.Model.Rkllm.IgnoreEosToken,
			MaxTokens:        cfg.Model.Rkllm.MaxTokens,
			EnableThinking:   cfg.Model.Rkllm.EnableThinking,
		})

	case "cloud-gemini", "gemini":
		return model.NewCloudGeminiEngine(model.GeminiEngineConfig{
			APIBase:        cfg.Model.Cloud.APIBase,
			APIKey:         cfg.Model.Cloud.APIKey,
			ModelName:      cfg.Model.Cloud.ModelName,
			TimeoutSeconds: cfg.Model.Cloud.TimeoutSeconds,
			MaxTokens:      cfg.Model.Cloud.MaxTokens,
		}), nil

	case "cloud-anthropic":
		return nil, fmt.Errorf("provider %q is scheduled for future roadmap; currently supported: [mock, cloud-openai, llama-pure, rkllm]", provider)

	default:
		return nil, fmt.Errorf("unknown model provider %q; available: mock, cloud-openai, llama-pure, rkllm, cloud-gemini, cloud-anthropic", provider)
	}
}
