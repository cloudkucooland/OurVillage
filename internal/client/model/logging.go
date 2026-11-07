package model

import (
	"context"
	"log"
	"time"
)

type LoggingEngine struct {
	engine InferenceEngine
	logger *log.Logger
}

func NewLoggingEngine(engine InferenceEngine, logger *log.Logger) *LoggingEngine {
	if logger == nil {
		logger = log.Default()
	}
	return &LoggingEngine{engine: engine, logger: logger}
}

func (e *LoggingEngine) Name() string {
	return e.engine.Name()
}

func (e *LoggingEngine) Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	started := time.Now()
	e.logRequest("Generate", prompt, opts)
	response, err := e.engine.Generate(ctx, prompt, opts)
	e.logger.Printf(
		"[Inference] complete engine=%s method=Generate duration=%s output_bytes=%d err=%v",
		e.Name(), time.Since(started), len(response), err,
	)
	return response, err
}

func (e *LoggingEngine) Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(token string) bool) error {
	started := time.Now()
	e.logRequest("Stream", prompt, opts)
	outputBytes := 0
	err := e.engine.Stream(ctx, prompt, opts, func(token string) bool {
		outputBytes += len(token)
		return onToken(token)
	})
	e.logger.Printf(
		"[Inference] complete engine=%s method=Stream duration=%s output_bytes=%d err=%v",
		e.Name(), time.Since(started), outputBytes, err,
	)
	return err
}

func (e *LoggingEngine) Close() error {
	return e.engine.Close()
}

func (e *LoggingEngine) logRequest(method, prompt string, opts GenerationOptions) {
	e.logger.Printf(
		"[Inference] start engine=%s method=%s prompt_bytes=%d prompt_chars=%d max_tokens_requested=%d",
		e.Name(), method, len(prompt), len([]rune(prompt)), opts.MaxTokens,
	)
}

var _ InferenceEngine = (*LoggingEngine)(nil)
