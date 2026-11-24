package model

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	llama "github.com/cloudkucooland/go-llama-pure"
)

// LlamaPureEngineConfig parameterizes embedded local inference via go-llama-pure.
type LlamaPureEngineConfig struct {
	LibraryPath    string
	ModelPath      string
	ContextSize    int
	Threads        int
	GPULayers      int
	MaxTokens      int
	Temperature    float32
	TopP           float32
	RepeatPenalty  float32
	GrammarFile    string
	EnableThinking bool
}

// LlamaPureEngine implements InferenceEngine using pure Go dynamic bindings to libllama.so.
type LlamaPureEngine struct {
	mu             sync.Mutex
	config         LlamaPureEngineConfig
	model          *llama.Model
	llamaCtx       *llama.Context
	defaultGrammar string
	closed         bool
}

// NewLlamaPureEngine initializes and loads a local model via go-llama-pure.
func NewLlamaPureEngine(cfg LlamaPureEngineConfig) (*LlamaPureEngine, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("model_path cannot be empty for llama-pure provider")
	}

	libPath := cfg.LibraryPath
	if libPath == "" {
		libPath = "/usr/local/lib/libllama.so.0.9.0"
	}

	var defaultGrammar string
	if cfg.GrammarFile != "" {
		b, err := os.ReadFile(cfg.GrammarFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read grammar file %q: %w", cfg.GrammarFile, err)
		}
		defaultGrammar = string(b)
	}

	// Initialize dynamic symbols via purego (guarded by sync.Once inside go-llama-pure)
	if err := llama.Init(libPath); err != nil {
		return nil, fmt.Errorf("failed to initialize libllama library at %q: %w", libPath, err)
	}

	modelOpts := llama.DefaultModelOptions()
	if cfg.GPULayers > 0 {
		modelOpts.NGPULayers = int32(cfg.GPULayers)
	}

	model, err := llama.LoadModel(cfg.ModelPath, modelOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to load model from %q: %w", cfg.ModelPath, err)
	}

	ctxOpts := llama.DefaultContextOptions()
	if cfg.ContextSize > 0 {
		ctxOpts.ContextSize = uint32(cfg.ContextSize)
	}
	if cfg.Threads > 0 {
		ctxOpts.Threads = int32(cfg.Threads)
	}
	if ctxOpts.BatchSize == 0 {
		ctxOpts.BatchSize = 512
	}

	llamaCtx, err := model.NewContext(ctxOpts)
	if err != nil {
		model.Close()
		return nil, fmt.Errorf("failed to create llama context: %w", err)
	}

	return &LlamaPureEngine{
		config:         cfg,
		model:          model,
		llamaCtx:       llamaCtx,
		defaultGrammar: defaultGrammar,
	}, nil
}

func (p *LlamaPureEngine) Name() string {
	return "llama-pure"
}

func (p *LlamaPureEngine) ModelLabel() string {
	return localModelLabel(p.Name(), p.config.ModelPath)
}

func (p *LlamaPureEngine) isQwen3Family() bool {
	lower := strings.ToLower(p.config.ModelPath)
	return strings.Contains(lower, "qwen3") || strings.Contains(lower, "qwen-3")
}

func (p *LlamaPureEngine) formatPrompt(prompt string) (string, error) {
	formatted, err := p.model.ApplyChatTemplate([]llama.ChatMessage{
		{Role: "user", Content: prompt},
	}, true)
	if err != nil {
		return "", fmt.Errorf("failed to apply chat template: %w", err)
	}

	// Qwen3 and Qwen3.5 models default to thinking mode unless an empty
	// <think></think> block is prefilled at the start of the assistant turn.
	if !p.config.EnableThinking && p.isQwen3Family() {
		formatted += "<think>\n\n</think>\n\n"
	}

	return formatted, nil
}

func (p *LlamaPureEngine) buildGenerateOptions(opts GenerationOptions) llama.GenerateOptions {
	genOpts := llama.DefaultGenerateOptions()

	if opts.MaxTokens > 0 {
		genOpts.MaxTokens = opts.MaxTokens
	} else if p.config.MaxTokens > 0 {
		genOpts.MaxTokens = p.config.MaxTokens
	} else {
		genOpts.MaxTokens = 512
	}

	grammar := opts.Grammar
	if grammar == "" {
		grammar = p.defaultGrammar
	}
	if grammar != "" {
		genOpts.Sampling.Grammar = grammar
	}

	if opts.Temperature > 0 {
		genOpts.Sampling.Temperature = opts.Temperature
	} else if grammar != "" {
		genOpts.Sampling.Temperature = 0.25
	} else if p.config.Temperature > 0 {
		genOpts.Sampling.Temperature = p.config.Temperature
	} else {
		genOpts.Sampling.Temperature = 0.7
	}

	if opts.TopP > 0 {
		genOpts.Sampling.TopP = opts.TopP
	} else if p.config.TopP > 0 {
		genOpts.Sampling.TopP = p.config.TopP
	} else {
		genOpts.Sampling.TopP = 0.9
	}

	if p.config.RepeatPenalty > 0 {
		genOpts.Sampling.RepeatPenalty = p.config.RepeatPenalty
	} else {
		genOpts.Sampling.RepeatPenalty = 1.05
	}

	return genOpts
}

func (p *LlamaPureEngine) Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if p.closed {
		return "", ErrEngineClosed
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	formattedPrompt, err := p.formatPrompt(prompt)
	if err != nil {
		return "", err
	}

	genOpts := p.buildGenerateOptions(opts)

	var builder strings.Builder
	genOpts.Callback = func(token llama.LlamaToken, text string) bool {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		builder.WriteString(text)
		return true
	}

	if _, err := p.llamaCtx.Generate(formattedPrompt, genOpts); err != nil {
		return "", fmt.Errorf("llama-pure generation error: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return builder.String(), nil
}

func (p *LlamaPureEngine) Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(token string) bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if p.closed {
		return ErrEngineClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	formattedPrompt, err := p.formatPrompt(prompt)
	if err != nil {
		return err
	}

	genOpts := p.buildGenerateOptions(opts)
	genOpts.Callback = func(token llama.LlamaToken, text string) bool {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		return onToken(text)
	}

	if _, err := p.llamaCtx.Generate(formattedPrompt, genOpts); err != nil {
		return fmt.Errorf("llama-pure streaming error: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return nil
}

func (p *LlamaPureEngine) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	if p.llamaCtx != nil {
		p.llamaCtx.Close()
		p.llamaCtx = nil
	}
	if p.model != nil {
		p.model.Close()
		p.model = nil
	}

	return nil
}
