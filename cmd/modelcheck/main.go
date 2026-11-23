package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ourvillage/internal/client/dispatcher"
	"ourvillage/internal/client/memory"
	"ourvillage/internal/client/model"
)

type BenchmarkResult struct {
	ModelName       string  `json:"model_name"`
	Backend         string  `json:"backend"`
	ContextSize     int     `json:"context_size"`
	PromptIndex     int     `json:"prompt_index"`
	PromptCategory  string  `json:"prompt_category"`
	Prompt          string  `json:"prompt"`
	OutputText      string  `json:"output_text"`
	DurationMs      int64   `json:"duration_ms"`
	TokensGen       int     `json:"tokens_gen"`
	TokensPerSec    float64 `json:"tokens_per_sec"`
	RawJSONValid    bool    `json:"raw_json_valid"`
	JSONValid       bool    `json:"json_valid"`
	IntentValid     bool    `json:"intent_valid"`
	ValidationError string  `json:"validation_error,omitempty"`
	JudgeEvaluation string  `json:"judge_evaluation,omitempty"`
	Timestamp       string  `json:"timestamp"`
}

type ModelReport struct {
	ModelName        string            `json:"model_name"`
	Backend          string            `json:"backend"`
	ContextSize      int               `json:"context_size"`
	Temperature      float64           `json:"temperature"`
	TopP             float64           `json:"top_p"`
	TopK             int               `json:"top_k"`
	RepeatPenalty    float64           `json:"repeat_penalty"`
	FrequencyPenalty float64           `json:"frequency_penalty"`
	PresencePenalty  float64           `json:"presence_penalty"`
	NKeep            int               `json:"n_keep"`
	SkipSpecialToken bool              `json:"skip_special_token"`
	IgnoreEosToken   bool              `json:"ignore_eos_token"`
	MaxTokens        int               `json:"max_tokens"`
	EnableThinking   bool              `json:"enable_thinking"`
	Results          []BenchmarkResult `json:"results"`
}

type ModelSummary struct {
	ModelName        string  `json:"model_name"`
	Backend          string  `json:"backend"`
	ContextSize      int     `json:"context_size"`
	TotalPrompts     int     `json:"total_prompts"`
	RawJSONValidCnt  int     `json:"raw_json_valid_count"`
	JSONValidCount   int     `json:"json_valid_count"`
	IntentValidCount int     `json:"intent_valid_count"`
	RawJSONValidPct  float64 `json:"raw_json_valid_pct"`
	JSONValidPct     float64 `json:"json_valid_pct"`
	IntentValidPct   float64 `json:"intent_valid_pct"`
	AvgTokensPerSec  float64 `json:"avg_tokens_per_sec"`
	AvgDurationMs    float64 `json:"avg_duration_ms"`
}

type testCase struct {
	Category     string
	Prompt       string
	UseGrammar   bool
	ValidateFunc func(output string) (jsonValid bool, intentValid bool, errStr string)
}

func main() {
	backendFlag := flag.String("backend", "", "Model backend: llama-pure or rkllm (required unless --rejudge is set)")
	modelsDirFlag := flag.String("models-dir", ".", "Directory containing model files (*.gguf or *.rkllm)")
	outputDirFlag := flag.String("output-dir", ".", "Directory to save JSON output reports")
	maxTokensFlag := flag.Int("max-tokens", 512, "Maximum completion tokens")
	tempFlag := flag.Float64("temperature", 0.2, "Sampling temperature")
	topPFlag := flag.Float64("top-p", 0.9, "Top-P sampling")
	topKFlag := flag.Int("top-k", 40, "Top-K sampling")
	repeatPenaltyFlag := flag.Float64("repeat-penalty", 1.15, "Repeat penalty")
	freqPenaltyFlag := flag.Float64("frequency-penalty", 0.0, "Frequency penalty")
	presPenaltyFlag := flag.Float64("presence-penalty", 0.0, "Presence penalty")
	nKeepFlag := flag.Int("n-keep", 0, "Number of tokens to keep in context")
	skipSpecialTokenFlag := flag.Bool("skip-special-token", false, "Skip special tokens")
	ignoreEosFlag := flag.Bool("ignore-eos", false, "Ignore EOS token")
	enableThinkingFlag := flag.Bool("enable-thinking", false, "Enable thinking/reasoning token generation")
	grammarFileFlag := flag.String("grammar-file", "config/grammars/agent_intent.gbnf", "Path to GBNF grammar file for gameplay intents")
	quickFlag := flag.Bool("quick", false, "Quick smoke test mode (tests 4k context only with a single prompt)")
	judgeProviderFlag := flag.String("judge-provider", "", "Online LLM judge provider (e.g., openai, gemini)")
	judgeModelFlag := flag.String("judge-model", "", "Online LLM judge model name (defaults: gemini-2.5-flash for gemini, gpt-4o for openai)")
	judgeApiKeyFlag := flag.String("judge-api-key", "", "Online LLM judge API key")
	rejudgeFlag := flag.Bool("rejudge", false, "Re-run LLM judge only on existing JSON reports in --output-dir where judge_evaluation is missing or errored")
	flag.Parse()

	judgeProvider := strings.ToLower(strings.TrimSpace(*judgeProviderFlag))
	judgeModel := strings.TrimSpace(*judgeModelFlag)
	if judgeProvider != "" {
		if judgeModel == "" {
			switch judgeProvider {
			case "gemini":
				judgeModel = "gemini-2.5-flash"
			case "openai":
				judgeModel = "gpt-4o"
			}
		}
		if *judgeApiKeyFlag == "" {
			log.Printf("[ModelCheck] Warning: --judge-provider specified but --judge-api-key is empty")
		} else {
			log.Printf("[ModelCheck] Online LLM judge enabled: provider=%s, model=%s", judgeProvider, judgeModel)
		}
	}

	if *rejudgeFlag {
		if judgeProvider == "" || *judgeApiKeyFlag == "" {
			log.Fatalf("Error: --rejudge requires --judge-provider and --judge-api-key")
		}
		rejudgeReports(*outputDirFlag, judgeProvider, judgeModel, *judgeApiKeyFlag)
		return
	}

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

	// Load grammar file if exists (used strictly for gameplay intents, NOT memory synthesis)
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
		if strings.HasSuffix(strings.ToLower(d.Name()), ext) {
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

	testCases, err := buildTestCases()
	if err != nil {
		log.Fatalf("Error building test cases: %v", err)
	}

	var summaries []ModelSummary

	var contextSizes []int
	if *quickFlag {
		contextSizes = []int{4096}
	} else {
		contextSizes = []int{4096, 8192, 16384}
	}

	ctx := context.Background()

	for _, modelPath := range modelPaths {
		modelBasename := filepath.Base(modelPath)
		modelName := strings.TrimSuffix(modelBasename, filepath.Ext(modelBasename))

		for _, ctxSize := range contextSizes {
			if backend == "rkllm" && ctxSize > 4096 {
				continue
			}

			log.Printf("[ModelCheck] Testing model %q with context size %d...", modelName, ctxSize)

			activeCases := testCases
			if *quickFlag {
				activeCases = testCases[:1]
			}

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
					ModelPath:        modelPath,
					ContextSize:      ctxSize,
					Temperature:      float32(*tempFlag),
					TopP:             float32(*topPFlag),
					TopK:             *topKFlag,
					RepeatPenalty:    float32(*repeatPenaltyFlag),
					FrequencyPenalty: float32(*freqPenaltyFlag),
					PresencePenalty:  float32(*presPenaltyFlag),
					NKeep:            *nKeepFlag,
					SkipSpecialToken: *skipSpecialTokenFlag,
					IgnoreEosToken:   *ignoreEosFlag,
					MaxTokens:        *maxTokensFlag,
					EnableThinking:   *enableThinkingFlag,
				})
			}

			if loadErr != nil {
				log.Printf("[ModelCheck] Skipping model %q with context size %d (load failed/unsupported: %v)", modelName, ctxSize, loadErr)
				continue
			}

			var results []BenchmarkResult

			for i, tc := range activeCases {
				startTime := time.Now()

				genOpts := model.GenerationOptions{
					Temperature: float32(*tempFlag),
					MaxTokens:   *maxTokensFlag,
				}
				if tc.UseGrammar {
					genOpts.Grammar = grammarContent
				}

				output, _ := engine.Generate(ctx, tc.Prompt, genOpts)
				duration := time.Since(startTime)
				durationMs := duration.Milliseconds()

				tokensGen := 0
				if len(output) > 0 {
					tokensGen = (len(output) + 3) / 4
					if tokensGen == 0 {
						tokensGen = 1
					}
				}
				tokensPerSec := 0.0
				if durationMs > 0 {
					tokensPerSec = float64(tokensGen) / (float64(durationMs) / 1000.0)
				}

				trimmed := strings.TrimSpace(output)
				rawJSONValid := len(trimmed) > 0 && json.Valid([]byte(trimmed))
				jsonValid, intentValid, valErrStr := tc.ValidateFunc(output)
				if rawJSONValid {
					jsonValid = true
				}
				if intentValid && !rawJSONValid && valErrStr == "" {
					valErrStr = "warning: output required sanitization (non-raw JSON)"
				}

				var judgeEval string
				if !tc.UseGrammar && judgeProvider != "" && *judgeApiKeyFlag != "" {
					log.Printf("[ModelCheck] Consulting online LLM judge (%s/%s) for prompt index %d (%s)...", judgeProvider, judgeModel, i, tc.Category)
					judgeEval = evaluateWithJudge(judgeProvider, judgeModel, *judgeApiKeyFlag, tc.Prompt, output)
				}

				results = append(results, BenchmarkResult{
					ModelName:       modelName,
					Backend:         backend,
					ContextSize:     ctxSize,
					PromptIndex:     i,
					PromptCategory:  tc.Category,
					Prompt:          tc.Prompt,
					OutputText:      output,
					DurationMs:      durationMs,
					TokensGen:       tokensGen,
					TokensPerSec:    tokensPerSec,
					RawJSONValid:    rawJSONValid,
					JSONValid:       jsonValid,
					IntentValid:     intentValid,
					ValidationError: valErrStr,
					JudgeEvaluation: judgeEval,
					Timestamp:       time.Now().Format(time.RFC3339),
				})
			}

			engine.Close()

			report := ModelReport{
				ModelName:        modelName,
				Backend:          backend,
				ContextSize:      ctxSize,
				Temperature:      *tempFlag,
				TopP:             *topPFlag,
				TopK:             *topKFlag,
				RepeatPenalty:    *repeatPenaltyFlag,
				FrequencyPenalty: *freqPenaltyFlag,
				PresencePenalty:  *presPenaltyFlag,
				NKeep:            *nKeepFlag,
				SkipSpecialToken: *skipSpecialTokenFlag,
				IgnoreEosToken:   *ignoreEosFlag,
				MaxTokens:        *maxTokensFlag,
				EnableThinking:   *enableThinkingFlag,
				Results:          results,
			}

			outFilename := fmt.Sprintf("%s-%dk.json", modelName, ctxSize/1024)
			outFilePath := filepath.Join(*outputDirFlag, outFilename)

			fileData, _ := json.MarshalIndent(report, "", "  ")
			if writeErr := os.WriteFile(outFilePath, fileData, 0644); writeErr != nil {
				log.Printf("[ModelCheck] Error writing report to %q: %v", outFilePath, writeErr)
			} else {
				log.Printf("[ModelCheck] Saved report to %q", outFilePath)
			}

			summaries = append(summaries, computeSummary(report))
		}
	}

	if len(summaries) > 0 {
		printAndSaveSummary(summaries, *outputDirFlag)
	}

	log.Printf("[ModelCheck] Benchmarking completed successfully.")
}

func buildTestCases() ([]testCase, error) {
	systemPromptHeader := `You are an AI agent in OurVillage. Respond ONLY with valid JSON matching the intent schema. Do not wrap in markdown or add extra text. Keep "thought" concise (under 15 words).
Valid Action Enums: MOVE, TRAVEL, CHAT, TRANSFER, GIVE, FORAGE, COLLECT, PLANT, HARVEST, CANCEL_ACTION, DROP, PICKUP, EQUIP, UNEQUIP, CRAFT, LOOK, STATUS, BUY, SELL, RESTOCK, EAT, DRINK, REST.
Schema format:
{
  "thought": "<reasoning>",
  "action": "<ACTION_ENUM>",
  "payload": { ... }
}

Examples:
- Move: {"thought": "Walking to mill", "action": "MOVE", "payload": {"target_location": "grain_mill"}}
- Forage: {"thought": "Gathering wood", "action": "FORAGE", "payload": {"target_resource": "firewood"}}
- Chat: {"thought": "Greeting John", "action": "CHAT", "payload": {"message": "Hello John!"}}
- Craft: {"thought": "Making workbench", "action": "CRAFT", "payload": {"recipe_id": "workbench"}}

Task: `

	actionPrompts := []struct {
		category string
		task     string
	}{
		{"action_move", `You are Martha the Miller in OurVillage. You are currently at village_square. You need to walk to the grain_mill to start grinding flour.`},
		{"action_forage", `You are Martha the Miller. You are at the forest_edge. You need to gather firewood to heat the mill.`},
		{"action_craft", `You are Martha the Miller. You are at the woodworking_shop. You have timber and tools. You need to craft a workbench.`},
		{"action_chat", `You are Martha the Miller. You encounter John the Blacksmith at the tavern. Send a friendly chat message asking about iron prices.`},
	}

	validateGameIntent := func(output string) (bool, bool, string) {
		jsonValid := false
		if _, parseErr := model.SanitizeAndExtractJSON(output); parseErr == nil {
			jsonValid = true
		}
		if _, valErr := dispatcher.ParseAndRepairIntent(output); valErr == nil {
			return true, true, ""
		} else {
			return jsonValid, false, valErr.Error()
		}
	}

	var cases []testCase
	for _, ap := range actionPrompts {
		cases = append(cases, testCase{
			Category:     ap.category,
			Prompt:       systemPromptHeader + ap.task,
			UseGrammar:   true,
			ValidateFunc: validateGameIntent,
		})
	}

	// Production Memory Prompt 1: Relationship Reflection (Martha -> Eleanor)
	relEvents := []memory.EventRecord{
		{ID: 101, Tick: 412, PeerID: "eleanor_clerk", EventType: "CHAT", RawPayload: `{"speaker":"eleanor_clerk","message":"Martha, you look exhausted from hauling sacks all morning. Have some warm tea before you head back to the mill."}`},
		{ID: 102, Tick: 413, PeerID: "eleanor_clerk", EventType: "CHAT", RawPayload: `{"speaker":"martha_miller","message":"Thank you, Eleanor. Once the afternoon wheat batch is ground, I will bring a fresh sack of fine flour by the town hall."}`},
		{ID: 103, Tick: 415, PeerID: "eleanor_clerk", EventType: "TRANSFER", RawPayload: `{"from":"eleanor_clerk","to":"martha_miller","item":"herbal_tea","quantity":1}`},
	}
	prevRel := memory.RelationshipMemory{
		PeerID:      "eleanor_clerk",
		Summary:     "Eleanor manages town hall ledgers and coordinates village grain requisitions.",
		Commitments: []string{"Confirm weekly flour quota for the town pantry"},
	}
	relPrompt := memory.BuildRelationshipPrompt("eleanor_clerk", relEvents, prevRel)
	cases = append(cases, testCase{
		Category:   "memory_relationship",
		Prompt:     relPrompt,
		UseGrammar: false,
		ValidateFunc: func(output string) (bool, bool, string) {
			cleaned, validJSON := extractJSONObject(output)
			if !validJSON {
				return false, false, "response is not a valid JSON object"
			}
			_, reflection, _, noEntry, err := memory.ParseRelationshipResponse(cleaned, prevRel)
			if err != nil {
				return true, false, err.Error()
			}
			if !noEntry && strings.Contains(reflection, "One-sentence takeaway from this batch") {
				return true, false, "model copied placeholder text from prompt"
			}
			return true, true, ""
		},
	})

	// Production Memory Prompt 2: Self-Identity Reflection (Eldon the Millwright)
	eldonTraits := []memory.IdentityTrait{
		{Key: "craft_philosophy", Value: "Preventative maintenance keeps the waterwheel turning smoothly.", SalienceScore: 8},
	}
	eldonRecentRefs := []memory.SelfReflection{
		{Tick: 390, ReflectionText: "Replaced a worn wooden cog on the secondary drive shaft before dawn."},
	}
	eldonEvents := []memory.EventRecord{
		{ID: 201, Tick: 420, PeerID: "eldon_millwright", EventType: "LOOK", RawPayload: `{"location":"town_mill","observation":"Main drive axle grease levels are low and sediment is building up in the millrace intake channel."}`},
		{ID: 202, Tick: 422, PeerID: "eldon_millwright", EventType: "REST", RawPayload: `{"location":"eldons_room","energy":44,"hunger":77,"reason":"Pausing before noon shift to recover energy and inspect millrace clearance."}`},
		{ID: 203, Tick: 425, PeerID: "eldon_millwright", EventType: "STATUS", RawPayload: `{"inventory":["iron_wrench","wheat","flour"],"energy":68,"hunger":79}`},
	}
	selfPrompt := memory.BuildSelfIdentityPrompt("eldon_millwright", 425, eldonTraits, eldonRecentRefs, eldonEvents)
	cases = append(cases, testCase{
		Category:   "memory_self_identity",
		Prompt:     selfPrompt,
		UseGrammar: false,
		ValidateFunc: func(output string) (bool, bool, string) {
			cleaned, validJSON := extractJSONObject(output)
			if !validJSON {
				return false, false, "response is not a valid JSON object"
			}
			noEntry, traitKey, traitVal, selfRef, lesson, _ := memory.ParseSelfIdentityResponse(cleaned)
			if !noEntry && traitKey == "" && traitVal == "" && selfRef == "" && lesson == "" {
				return true, false, "empty self-identity fields without no_entry=true"
			}
			if strings.Contains(selfRef, "Today's forge work yielded three solid iron ingots") {
				return true, false, "model copied forge example text verbatim from prompt"
			}
			return true, true, ""
		},
	})

	// Production Memory Prompt 3: Personal Projects Synthesis (Eldon)
	existingProjects := []memory.PersonalProject{
		{
			ID:          "millrace-sediment-guard",
			Title:       "Install removable grate on millrace intake",
			Motivation:  "Stop river silt from fouling the waterwheel paddles before evening grain deliveries",
			Status:      "active",
			Progress:    "Inspected intake channel and measured width",
			NextStep:    "Source iron bars from Gideon at the forge",
			People:      []string{"gideon_forge"},
			Locations:   []string{"town_mill", "blacksmith"},
			UpdatedTick: 400,
		},
	}
	projectsPrompt, err := memory.BuildProjectsPrompt(eldonEvents, existingProjects)
	if err != nil {
		return nil, err
	}
	cases = append(cases, testCase{
		Category:   "memory_projects",
		Prompt:     projectsPrompt,
		UseGrammar: false,
		ValidateFunc: func(output string) (bool, bool, string) {
			cleaned, validJSON := extractJSONObject(output)
			if !validJSON {
				return false, false, "response is not a valid JSON object"
			}
			_, parseErr := memory.ParseProjectUpdates(cleaned, 425)
			if parseErr != nil {
				return true, false, parseErr.Error()
			}
			return true, true, ""
		},
	})

	return cases, nil
}

func extractJSONObject(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return "", false
	}
	candidate := trimmed[start : end+1]
	return candidate, json.Valid([]byte(candidate))
}

func computeSummary(report ModelReport) ModelSummary {
	totalPrompts := len(report.Results)
	rawJSONValidCount := 0
	jsonValidCount := 0
	intentValidCount := 0
	var totalTokensPerSec float64
	var totalDurationMs int64

	for _, r := range report.Results {
		if r.RawJSONValid {
			rawJSONValidCount++
		}
		if r.JSONValid {
			jsonValidCount++
		}
		if r.IntentValid {
			intentValidCount++
		}
		totalTokensPerSec += r.TokensPerSec
		totalDurationMs += r.DurationMs
	}

	var rawJSONValidPct, jsonValidPct, intentValidPct, avgTokensPerSec, avgDurationMs float64
	if totalPrompts > 0 {
		rawJSONValidPct = float64(rawJSONValidCount) / float64(totalPrompts) * 100.0
		jsonValidPct = float64(jsonValidCount) / float64(totalPrompts) * 100.0
		intentValidPct = float64(intentValidCount) / float64(totalPrompts) * 100.0
		avgTokensPerSec = totalTokensPerSec / float64(totalPrompts)
		avgDurationMs = float64(totalDurationMs) / float64(totalPrompts)
	}

	return ModelSummary{
		ModelName:        report.ModelName,
		Backend:          report.Backend,
		ContextSize:      report.ContextSize,
		TotalPrompts:     totalPrompts,
		RawJSONValidCnt:  rawJSONValidCount,
		JSONValidCount:   jsonValidCount,
		IntentValidCount: intentValidCount,
		RawJSONValidPct:  rawJSONValidPct,
		JSONValidPct:     jsonValidPct,
		IntentValidPct:   intentValidPct,
		AvgTokensPerSec:  avgTokensPerSec,
		AvgDurationMs:    avgDurationMs,
	}
}

func printAndSaveSummary(summaries []ModelSummary, outputDir string) {
	log.Print("\n=== BENCHMARK SUMMARY REPORT ===")
	log.Printf("%-35s | %-7s | %-5s | %-9s | %-9s | %-9s | %-8s", "Model", "Context", "Total", "RawJSON %", "JSON %", "Intent %", "Tok/s")
	log.Printf("%s", strings.Repeat("-", 98))
	for _, s := range summaries {
		log.Printf("%-35s | %-7d | %-5d | %8.1f%% | %8.1f%% | %8.1f%% | %8.2f",
			s.ModelName, s.ContextSize, s.TotalPrompts, s.RawJSONValidPct, s.JSONValidPct, s.IntentValidPct, s.AvgTokensPerSec)
	}

	summaryFilePath := filepath.Join(outputDir, "summary.json")
	summaryData, _ := json.MarshalIndent(summaries, "", "  ")
	if writeErr := os.WriteFile(summaryFilePath, summaryData, 0644); writeErr != nil {
		log.Printf("[ModelCheck] Error writing summary report to %q: %v", summaryFilePath, writeErr)
	} else {
		log.Printf("[ModelCheck] Saved summary report to %q", summaryFilePath)
	}
}

func rejudgeReports(outputDir, provider, modelName, apiKey string) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		log.Fatalf("Error reading output directory %q: %v", outputDir, err)
	}

	var summaries []ModelSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == "summary.json" {
			continue
		}
		filePath := filepath.Join(outputDir, entry.Name())
		rawBytes, readErr := os.ReadFile(filePath)
		if readErr != nil {
			log.Printf("[ModelCheck] Warning: could not read %q: %v", filePath, readErr)
			continue
		}

		var report ModelReport
		if unmarshalErr := json.Unmarshal(rawBytes, &report); unmarshalErr != nil {
			log.Printf("[ModelCheck] Warning: could not parse %q: %v", filePath, unmarshalErr)
			continue
		}

		updated := false
		for i := range report.Results {
			res := &report.Results[i]
			if !strings.HasPrefix(res.PromptCategory, "memory_") && res.PromptIndex < 4 {
				continue
			}
			needsJudge := strings.TrimSpace(res.JudgeEvaluation) == "" ||
				strings.Contains(res.JudgeEvaluation, "RESOURCE_EXHAUSTED") ||
				strings.Contains(res.JudgeEvaluation, "UNAVAILABLE") ||
				strings.HasPrefix(res.JudgeEvaluation, "judge error:")

			if needsJudge {
				log.Printf("[ModelCheck] Re-judging %s prompt %d (%s)...", report.ModelName, res.PromptIndex, res.PromptCategory)
				res.JudgeEvaluation = evaluateWithJudge(provider, modelName, apiKey, res.Prompt, res.OutputText)
				updated = true
				time.Sleep(1500 * time.Millisecond)
			}
		}

		if updated {
			fileData, _ := json.MarshalIndent(report, "", "  ")
			if writeErr := os.WriteFile(filePath, fileData, 0644); writeErr != nil {
				log.Printf("[ModelCheck] Error saving updated report %q: %v", filePath, writeErr)
			} else {
				log.Printf("[ModelCheck] Updated report %q", filePath)
			}
		}
		summaries = append(summaries, computeSummary(report))
	}

	if len(summaries) > 0 {
		printAndSaveSummary(summaries, outputDir)
	}
}

func evaluateWithJudge(provider, modelName, apiKey, promptContext, outputText string) string {
	if provider == "" || apiKey == "" {
		return ""
	}

	prompt := fmt.Sprintf(`You are an expert AI evaluation judge for OurVillage agent memory consolidation.
Evaluate whether the agent's synthesized memory output accurately reflects the prompt's events and instructions without hallucination or copying example placeholders verbatim.

--- PROMPT & EVENT CONTEXT ---
%s

--- AGENT MEMORY OUTPUT ---
%s

--- TASK ---
Provide your evaluation in valid JSON format:
{
  "score": <1 to 10>,
  "factual_retention": "high/medium/low",
  "hallucination_detected": true/false,
  "rationale": "<brief explanation>"
}`, promptContext, outputText)

	client := &http.Client{Timeout: 30 * time.Second}

	if provider == "gemini" {
		url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", modelName, apiKey)
		bodyMap := map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"parts": []map[string]string{
						{"text": prompt},
					},
				},
			},
			"generationConfig": map[string]interface{}{
				"responseMimeType": "application/json",
			},
		}
		jsonBody, _ := json.Marshal(bodyMap)
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
		if err != nil {
			return fmt.Sprintf("judge error: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Sprintf("judge error: %v", err)
		}
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		var res map[string]interface{}
		if err := json.Unmarshal(respBytes, &res); err == nil {
			if candidates, ok := res["candidates"].([]interface{}); ok && len(candidates) > 0 {
				if cand, ok := candidates[0].(map[string]interface{}); ok {
					if content, ok := cand["content"].(map[string]interface{}); ok {
						if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
							if part, ok := parts[0].(map[string]interface{}); ok {
								if text, ok := part["text"].(string); ok {
									return text
								}
							}
						}
					}
				}
			}
		}
		return string(respBytes)
	} else if provider == "openai" {
		url := "https://api.openai.com/v1/chat/completions"
		bodyMap := map[string]interface{}{
			"model": modelName,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
			"response_format": map[string]string{
				"type": "json_object",
			},
		}
		jsonBody, _ := json.Marshal(bodyMap)
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
		if err != nil {
			return fmt.Sprintf("judge error: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Sprintf("judge error: %v", err)
		}
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		var res map[string]interface{}
		if err := json.Unmarshal(respBytes, &res); err == nil {
			if choices, ok := res["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if msg, ok := choice["message"].(map[string]interface{}); ok {
						if content, ok := msg["content"].(string); ok {
							return content
						}
					}
				}
			}
		}
		return string(respBytes)
	}
	return "unsupported judge provider"
}
