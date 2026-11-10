package model

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type RkllmEngineConfig struct {
	LibraryPath string
	ModelPath   string
	ContextSize int
	Temperature float32
	TopP        float32
	MaxTokens   int
}

type LLMHandle unsafe.Pointer
type LLMCallState int32

const (
	RKLLM_RUN_NORMAL  LLMCallState = 0
	RKLLM_RUN_WAITING              = 1
	RKLLM_RUN_FINISH               = 2
	RKLLM_RUN_ERROR                = 3
)

type RKLLMInputType int32

const (
	RKLLM_INPUT_PROMPT RKLLMInputType = 0
)

type RKLLMInferMode int32

const (
	RKLLM_INFER_MODE_NORMAL RKLLMInferMode = 0
)

type RKLLMResultLastHiddenLayer struct {
	HiddenStates *float32
	EmbdSize     int32
	NumTokens    int32
}

type RKLLMResultLogits struct {
	Logits    *float32
	VocabSize int32
	NumTokens int32
}

type RKLLMPerfStat struct {
	PrefillTimeMs  float32
	PrefillTokens  int32
	GenerateTimeMs float32
	GenerateTokens int32
	MemoryUsageMB  float32
}

type RKLLMResult struct {
	Text            *byte
	TokenID         int32
	LastHiddenLayer RKLLMResultLastHiddenLayer
	Logits          RKLLMResultLogits
	Perf            RKLLMPerfStat
}

type RKLLMParam struct {
	ModelPath        *byte
	MaxContextLen    int32
	MaxNewTokens     int32
	TopK             int32
	NKeep            int32
	TopP             float32
	Temperature      float32
	RepeatPenalty    float32
	FrequencyPen     float32
	PresencePen      float32
	Mirostat         int32
	MirostatTau      float32
	MirostatEta      float32
	SkipSpecialToken bool
	IgnoreEosToken   bool
	IsAsync          bool
	ExtendParam      [128]byte
}

type RKLLMInput struct {
	Role           *byte
	EnableThinking bool
	InputType      RKLLMInputType
	PromptInput    *byte
}

type functionPointer uintptr

type RKLLMCallback struct {
	ResultCallback    functionPointer
	ResultUserdata    unsafe.Pointer
	TokenizerCallback functionPointer
	TokenizerUserdata unsafe.Pointer
	EmbedCallback     functionPointer
	EmbedUserdata     unsafe.Pointer
}

type RKLLMSamplingParam struct {
	TopK             int32
	TopP             float32
	Temperature      float32
	RepeatPenalty    float32
	FrequencyPenalty float32
	PresencePenalty  float32
	Mirostat         int32
	MirostatTau      float32
	MirostatEta      float32
}

type RKLLMInferParam struct {
	Mode              RKLLMInferMode
	LoraParams        unsafe.Pointer
	PromptCacheParams unsafe.Pointer
	SamplingParams    *RKLLMSamplingParam
	KeepHistory       int32
	MaxNewTokens      int32
}

type rkllmStreamState struct {
	ch     chan string
	reqCtx context.Context
}

type RkllmEngine struct {
	mu           sync.Mutex
	inferMu      sync.Mutex
	config       RkllmEngineConfig
	lib          uintptr
	handle       LLMHandle
	rkllmRun     func(handle LLMHandle, input *RKLLMInput, inferParams *RKLLMInferParam, userdata unsafe.Pointer) int32
	rkllmDestroy func(handle LLMHandle) int32
	rkllmAbort   func(handle LLMHandle) int32

	callbackPtr uintptr
}

func stringToBytePtr(s string) (*byte, []byte) {
	b := append([]byte(s), 0)
	return &b[0], b
}

func cStringToGo(ptr *byte) string {
	if ptr == nil {
		return ""
	}
	bytes := make([]byte, 0)
	p := uintptr(unsafe.Pointer(ptr))
	for {
		b := *(*byte)(unsafe.Pointer(p))
		if b == 0 {
			break
		}
		bytes = append(bytes, b)
		p++
	}
	return string(bytes)
}

func NewRkllmEngine(cfg RkllmEngineConfig) (*RkllmEngine, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("model_path cannot be empty for rkllm provider")
	}
	libPath := cfg.LibraryPath
	if libPath == "" {
		libPath = "/usr/lib/librkllmrt.so"
	}

	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW)
	if err != nil {
		return nil, fmt.Errorf("failed to open librkllm at %q: %w", libPath, err)
	}

	var rkllmCreateDefaultParam func() RKLLMParam
	var rkllmInit func(handle *LLMHandle, param *RKLLMParam, callback *RKLLMCallback) int32
	var rkllmRun func(handle LLMHandle, input *RKLLMInput, inferParams *RKLLMInferParam, userdata unsafe.Pointer) int32
	var rkllmDestroy func(handle LLMHandle) int32
	var rkllmAbort func(handle LLMHandle) int32

	purego.RegisterLibFunc(&rkllmCreateDefaultParam, lib, "rkllm_createDefaultParam")
	purego.RegisterLibFunc(&rkllmInit, lib, "rkllm_init")
	purego.RegisterLibFunc(&rkllmRun, lib, "rkllm_run")
	purego.RegisterLibFunc(&rkllmDestroy, lib, "rkllm_destroy")
	purego.RegisterLibFunc(&rkllmAbort, lib, "rkllm_abort")

	modelPathPtr, modelPathSlice := stringToBytePtr(cfg.ModelPath)
	defer runtime.KeepAlive(modelPathSlice)

	param := rkllmCreateDefaultParam()
	param.ModelPath = modelPathPtr
	if cfg.ContextSize > 0 {
		param.MaxContextLen = int32(cfg.ContextSize)
	} else {
		param.MaxContextLen = 4096
	}
	if cfg.MaxTokens > 0 {
		param.MaxNewTokens = int32(cfg.MaxTokens)
	} else {
		param.MaxNewTokens = 1024
	}
	if cfg.Temperature > 0 {
		param.Temperature = cfg.Temperature
	} else {
		param.Temperature = 0.7
	}
	if cfg.TopP > 0 {
		param.TopP = cfg.TopP
	} else {
		param.TopP = 0.9
	}
	// Enable NPU cores
	param.ExtendParam[0] = 7

	var handle LLMHandle
	engine := &RkllmEngine{
		config:       cfg,
		lib:          lib,
		handle:       handle,
		rkllmRun:     rkllmRun,
		rkllmDestroy: rkllmDestroy,
		rkllmAbort:   rkllmAbort,
	}

	callbackFn := func(result *RKLLMResult, userdata unsafe.Pointer, state LLMCallState) uintptr {
		if userdata == nil {
			return 0
		}
		streamState := (*rkllmStreamState)(userdata)
		if streamState == nil || streamState.ch == nil {
			return 0
		}

		reqCtx := streamState.reqCtx
		if reqCtx == nil {
			reqCtx = context.Background()
		}

		switch state {
		case RKLLM_RUN_NORMAL:
			if result != nil && result.Text != nil {
				text := cStringToGo(result.Text)
				if text != "" {
					select {
					case <-reqCtx.Done():
					case streamState.ch <- text:
					}
				}
			}
		case RKLLM_RUN_FINISH:
			close(streamState.ch)
		case RKLLM_RUN_ERROR:
			close(streamState.ch)
		}
		return 0
	}

	engine.callbackPtr = purego.NewCallback(callbackFn)
	cb := RKLLMCallback{
		ResultCallback: functionPointer(engine.callbackPtr),
	}

	if ret := rkllmInit(&engine.handle, &param, &cb); ret != 0 {
		return nil, fmt.Errorf("rkllm_init failed: %d", ret)
	}

	return engine, nil
}

func (e *RkllmEngine) Name() string {
	return "rkllm"
}

func (e *RkllmEngine) Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(token string) bool) error {
	e.inferMu.Lock()
	defer e.inferMu.Unlock()

	e.mu.Lock()
	if e.handle == nil {
		e.mu.Unlock()
		return fmt.Errorf("rkllm engine not initialized")
	}
	handle := e.handle
	abortFn := e.rkllmAbort
	e.mu.Unlock()

	ch := make(chan string, 100)
	streamState := &rkllmStreamState{
		ch:     ch,
		reqCtx: ctx,
	}

	promptPtr, promptSlice := stringToBytePtr(prompt)
	defer runtime.KeepAlive(promptSlice)

	input := RKLLMInput{
		InputType:   RKLLM_INPUT_PROMPT,
		PromptInput: promptPtr,
	}

	sampling := RKLLMSamplingParam{
		Temperature:   opts.Temperature,
		TopP:          opts.TopP,
		RepeatPenalty: 1.15,
		TopK:          40,
	}
	if sampling.Temperature == 0 {
		sampling.Temperature = e.config.Temperature
	}
	if sampling.TopP == 0 {
		sampling.TopP = e.config.TopP
	}

	inferParam := RKLLMInferParam{
		Mode:           RKLLM_INFER_MODE_NORMAL,
		SamplingParams: &sampling,
	}
	if opts.MaxTokens > 0 {
		inferParam.MaxNewTokens = int32(opts.MaxTokens)
	} else if e.config.MaxTokens > 0 {
		inferParam.MaxNewTokens = int32(e.config.MaxTokens)
	} else {
		inferParam.MaxNewTokens = 512
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		_ = e.rkllmRun(handle, &input, &inferParam, unsafe.Pointer(streamState))
		select {
		case <-streamState.ch:
		default:
			close(streamState.ch)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			if abortFn != nil && handle != nil {
				abortFn(handle)
			}
			wg.Wait()
			return ctx.Err()
		case token, ok := <-ch:
			if !ok {
				wg.Wait()
				return nil
			}
			if !onToken(token) {
				if abortFn != nil && handle != nil {
					abortFn(handle)
				}
				wg.Wait()
				return nil
			}
		}
	}
}

func (e *RkllmEngine) Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error) {
	var sb strings.Builder
	err := e.Stream(ctx, prompt, opts, func(token string) bool {
		sb.WriteString(token)
		return true
	})
	return sb.String(), err
}

func (e *RkllmEngine) Close() error {
	e.inferMu.Lock()
	defer e.inferMu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.handle != nil && e.rkllmDestroy != nil {
		e.rkllmDestroy(e.handle)
		e.handle = nil
	}
	return nil
}
