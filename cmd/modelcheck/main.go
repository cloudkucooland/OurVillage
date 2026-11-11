package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentmud/internal/client/dispatcher"
	"agentmud/internal/client/model"
)

type BenchmarkResult struct {
	ModelName       string  `json:"model_name"`
	Backend         string  `json:"backend"`
	ContextSize     int     `json:"context_size"`
	PromptIndex     int     `json:"prompt_index"`
	Prompt          string  `json:"prompt"`
	OutputText      string  `json:"output_text"`
	DurationMs      int64   `json:"duration_ms"`
	TokensGen       int     `json:"tokens_gen"`
	TokensPerSec    float64 `json:"tokens_per_sec"`
	JSONValid       bool    `json:"json_valid"`
	IntentValid     bool    `json:"intent_valid"`
	ValidationError string  `json:"validation_error,omitempty"`
	Timestamp       string  `json:"timestamp"`
}

type ModelReport struct {
	ModelName   string            `json:"model_name"`
	Backend     string            `json:"backend"`
	ContextSize int               `json:"context_size"`
	Results     []BenchmarkResult `json:"results"`
}

func main() {
	backendFlag := flag.String("backend", "", "Model backend: llama-pure or rkllm (required)")
	modelsDirFlag := flag.String("models-dir", ".", "Directory containing model files (*.gguf or *.rkllm)")
	outputDirFlag := flag.String("output-dir", ".", "Directory to save JSON output reports")
	maxTokensFlag := flag.Int("max-tokens", 256, "Maximum completion tokens")
	tempFlag := flag.Float64("temperature", 0.2, "Sampling temperature")
	grammarFileFlag := flag.String("grammar-file", "config/grammars/agent_intent.gbnf", "Path to GBNF grammar file")
	flag.Parse()

	if *backendFlag == "" {
		log.Fatalf("Error: --backend flag is required (choices: llama-pure, rkllm)")
	}

	backend := strings.ToLower(strings.TrimSpace(*backendFlag))
	if backend != "llama-pure" && backend != "rkllm" {
		log.Fatalf("Error: invalid backend %q. Must be 'llama-pure' or 'rkllm'", backend)
	}

	if err := os.MkdirAll(*outputDirFlag, 0755); err != nil {
		log.Fatalf("Error creating output directory %q: %v", *outputDirFlag, err)
	}

	// Load grammar file if exists
	var grammarContent string
	if *grammarFileFlag != "" {
		if data, err := os.ReadFile(*grammarFileFlag); err == nil {
			grammarContent = string(data)
			log.Printf("[ModelCheck] Loaded GBNF grammar from %q", *grammarFileFlag)
		} else {
			log.Printf("[ModelCheck] Warning: could not load grammar file %q: %v", *grammarFileFlag, err)
		}
	}

	// Discover model files
	var ext string
	if backend == "llama-pure" {
		ext = ".gguf"
	} else {
		ext = ".rkllm"
	}

	var modelPaths []string
	err := filepath.WalkDir(*modelsDirFlag, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(strings.ToLower(name), ext) {
			modelPaths = append(modelPaths, path)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("Error walking models directory %q: %v", *modelsDirFlag, err)
	}

	if len(modelPaths) == 0 {
		log.Fatalf("No model files with extension %q found in directory %q", ext, *modelsDirFlag)
	}

	log.Printf("[ModelCheck] Found %d models for backend %s in %q", len(modelPaths), backend, *modelsDirFlag)

	systemPromptHeader := `You are an AI agent in OurVillage. Respond ONLY with valid JSON matching the intent schema.
Valid Action Enums: MOVE, TRAVEL, CHAT, TRANSFER, GIVE, FORAGE, COLLECT, PLANT, HARVEST, CANCEL_ACTION, DROP, PICKUP, EQUIP, UNEQUIP, CRAFT, LOOK, STATUS, BUY, SELL, RESTOCK, EAT, DRINK, REST.
Schema format:
{
  "thought": "<reasoning>",
  "action": "<ACTION_ENUM>",
  "payload": { ... }
}

Examples:
- Move: {"thought": "Walking to mill", "action": "MOVE", "payload": {"target_location": "grain_mill"}}
- Chat: {"thought": "Greeting John", "action": "CHAT", "payload": {"message": "Hello John!"}}
- Craft: {"thought": "Making workbench", "action": "CRAFT", "payload": {"recipe_id": "workbench"}}

Task: `

	// Test prompts suite based on OurVillage game data
	rawPrompts := []string{
		`You are Martha the Miller in OurVillage. You are currently at village_square. You need to walk to the grain_mill to start grinding flour.`,
		`You are Martha the Miller. You are at the forest_edge. You need to gather firewood to heat the mill.`,
		`You are Martha the Miller. You are at the woodworking_shop. You have timber and tools. You need to craft a workbench.`,
		`You are Martha the Miller. You encounter John the Blacksmith at the tavern. Send a friendly chat message asking about iron prices.`,
	}

	var prompts []string
	for _, p := range rawPrompts {
		prompts = append(prompts, systemPromptHeader+p)
	}

	contextSizes := []int{1024, 2048, 4096, 8192, 16384}

	ctx := context.Background()

	for _, modelPath := range modelPaths {
		modelBasename := filepath.Base(modelPath)
		modelName := strings.TrimSuffix(modelBasename, filepath.Ext(modelBasename))

		for _, ctxSize := range contextSizes {
			// Skip > 4k for rkllm if not supported
			if backend == "rkllm" && ctxSize > 4096 {
				continue
			}

			log.Printf("[ModelCheck] Testing model %q with context size %d...", modelName, ctxSize)

			var engine model.InferenceEngine
			var loadErr error

			if backend == "llama-pure" {
				engine, loadErr = model.NewLlamaPureEngine(model.LlamaPureEngineConfig{
					ModelPath:   modelPath,
					ContextSize: ctxSize,
					Temperature: float32(*tempFlag),
					MaxTokens:   *maxTokensFlag,
					GrammarFile: *grammarFileFlag,
				})
			} else {
				engine, loadErr = model.NewRkllmEngine(model.RkllmEngineConfig{
					ModelPath:   modelPath,
					ContextSize: ctxSize,
					Temperature: float32(*tempFlag),
					MaxTokens:   *maxTokensFlag,
				})
			}

			if loadErr != nil {
				log.Printf("[ModelCheck] Skipping model %q with context size %d (load failed/unsupported: %v)", modelName, ctxSize, loadErr)
				continue
			}

			var results []BenchmarkResult

			for i, prompt := range prompts {
				startTime := time.Now()

				genOpts := model.GenerationOptions{
					Temperature: float32(*tempFlag),
					MaxTokens:   *maxTokensFlag,
					Grammar:     grammarContent,
				}

				output, _ := engine.Generate(ctx, prompt, genOpts)
				duration := time.Since(startTime)
				durationMs := duration.Milliseconds()

				tokensGen := *maxTokensFlag // approximation or actual if returned
				if len(output) > 0 {
					// rough token estimation if token count not provided by engine
					tokensGen = len(strings.Fields(output))
					if tokensGen == 0 {
						tokensGen = 1
					}
				}
				tokensPerSec := 0.0
				if durationMs > 0 {
					tokensPerSec = float64(tokensGen) / (float64(durationMs) / 1000.0)
				}

				jsonValid := false
				intentValid := false
				var valErrStr string

				// Use production ParseAndRepairIntent and SanitizeAndExtractJSON
				if _, parseErr := model.SanitizeAndExtractJSON(output); parseErr == nil {
					jsonValid = true
				}

				if _, valErr := dispatcher.ParseAndRepairIntent(output); valErr == nil {
					jsonValid = true
					intentValid = true
				} else {
					valErrStr = valErr.Error()
				}

				results = append(results, BenchmarkResult{
					ModelName:       modelName,
					Backend:         backend,
					ContextSize:     ctxSize,
					PromptIndex:     i,
					Prompt:          prompt,
					OutputText:      output,
					DurationMs:      durationMs,
					TokensGen:       tokensGen,
					TokensPerSec:    tokensPerSec,
					JSONValid:       jsonValid,
					IntentValid:     intentValid,
					ValidationError: valErrStr,
					Timestamp:       time.Now().Format(time.RFC3339),
				})
			}

			engine.Close()

			report := ModelReport{
				ModelName:   modelName,
				Backend:     backend,
				ContextSize: ctxSize,
				Results:     results,
			}

			outFilename := fmt.Sprintf("%s-%dk.json", modelName, ctxSize/1024)
			outFilePath := filepath.Join(*outputDirFlag, outFilename)

			fileData, _ := json.MarshalIndent(report, "", "  ")
			if writeErr := os.WriteFile(outFilePath, fileData, 0644); writeErr != nil {
				log.Printf("[ModelCheck] Error writing report to %q: %v", outFilePath, writeErr)
			} else {
				log.Printf("[ModelCheck] Saved report to %q", outFilePath)
			}
		}
	}

	log.Printf("[ModelCheck] Benchmarking completed successfully.")
}
