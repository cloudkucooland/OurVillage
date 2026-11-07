package client

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// Config represents the complete runtime configuration for agentmud-client,
// matching the layered hierarchy: CLI Flags > Environment Variables > Config File > Built-in Defaults.
type Config struct {
	ConfigFile string          `yaml:"-" json:"-"`
	Client     ClientConfig    `yaml:"client" json:"client"`
	Model      ModelConfig     `yaml:"model" json:"model"`
	Memory     MemoryConfig    `yaml:"memory" json:"memory"` // Added
	Cognition  CognitionConfig `yaml:"cognition" json:"cognition"`
	Action     ActionConfig    `yaml:"action" json:"action"`
}

// ClientConfig holds network and identity parameters for MQTT communication.
type ClientConfig struct {
	AgentID          string `yaml:"agent_id" json:"agent_id"`
	Password         string `yaml:"password" json:"password"`
	BrokerURL        string `yaml:"broker_url" json:"broker_url"`
	ClientID         string `yaml:"client_id" json:"client_id"`
	KeepAliveSeconds int    `yaml:"keepalive_seconds" json:"keepalive_seconds"`
	TUI              bool   `yaml:"tui" json:"tui"`
	LogFile          string `yaml:"log_file" json:"log_file"`
}

// ModelConfig specifies the inference engine backend and provider-specific configurations.
type ModelConfig struct {
	// Provider options: "mock", "llama-pure", "rkllm", "cloud-openai", "cloud-gemini", "cloud-anthropic"
	Provider  string           `yaml:"provider" json:"provider"`
	LlamaPure LlamaPureConfig  `yaml:"llama_pure" json:"llama_pure"`
	Rkllm     RkllmConfig      `yaml:"rkllm" json:"rkllm"`
	Cloud     CloudModelConfig `yaml:"cloud" json:"cloud"`
}

type MemoryConfig struct {
	DBPath                  string `yaml:"db_path" json:"db_path"`
	MaxReflectionsPerPeer   int    `yaml:"max_reflections_per_peer" json:"max_reflections_per_peer"`
	SynthesisTimeoutSeconds int    `yaml:"synthesis_timeout_seconds" json:"synthesis_timeout_seconds"`
}

// RkllmConfig parameterizes NPU-accelerated inference via librkllm.
type RkllmConfig struct {
	LibraryPath string  `yaml:"library_path" json:"library_path"`
	ModelPath   string  `yaml:"model_path" json:"model_path"`
	ContextSize int     `yaml:"context_size" json:"context_size"`
	Temperature float64 `yaml:"temperature" json:"temperature"`
	TopP        float64 `yaml:"top_p" json:"top_p"`
}

// LlamaPureConfig parameterizes embedded local inference via go-llama-pure.
type LlamaPureConfig struct {
	LibraryPath   string  `yaml:"library_path" json:"library_path"`
	ModelPath     string  `yaml:"model_path" json:"model_path"`
	ContextSize   int     `yaml:"context_size" json:"context_size"`
	Threads       int     `yaml:"threads" json:"threads"`
	GPULayers     int     `yaml:"gpu_layers" json:"gpu_layers"`
	MaxTokens     int     `yaml:"max_tokens" json:"max_tokens"`
	Temperature   float64 `yaml:"temperature" json:"temperature"`
	TopP          float64 `yaml:"top_p" json:"top_p"`
	RepeatPenalty float64 `yaml:"repeat_penalty" json:"repeat_penalty"`
	GrammarFile   string  `yaml:"grammar_file" json:"grammar_file"`
}

// CloudModelConfig parameterizes remote REST/OpenAI-compatible inference.
type CloudModelConfig struct {
	APIBase        string `yaml:"api_base" json:"api_base"`
	APIKey         string `yaml:"api_key" json:"api_key"`
	ModelName      string `yaml:"model_name" json:"model_name"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	MaxTokens      int    `yaml:"max_tokens" json:"max_tokens"`
}

// CognitionConfig parameterizes sensory queue debouncing and autonomous triggers.
type CognitionConfig struct {
	TickIntervalDebounceMS  int     `yaml:"tick_interval_debounce_ms" json:"tick_interval_debounce_ms"`
	ChatResponseProbability float64 `yaml:"chat_response_probability" json:"chat_response_probability"`
	MaxSensoryLogLines      int     `yaml:"max_sensory_log_lines" json:"max_sensory_log_lines"`
	ReflectionIntervalTicks int     `yaml:"reflection_interval_ticks" json:"reflection_interval_ticks"`
	IdleThresholdTicks      int     `yaml:"idle_threshold_ticks" json:"idle_threshold_ticks"`
	SmallSystem             bool    `yaml:"small_system" json:"small_system"`
}

// ActionConfig allows specifying an intent to fire upon connecting (useful for CLI test actions).
type ActionConfig struct {
	Action    string `yaml:"action" json:"action"`
	TargetLoc string `yaml:"target_location" json:"target_location"`
	ChatMsg   string `yaml:"chat_message" json:"chat_message"`
	Recipient string `yaml:"recipient_id" json:"recipient_id"`
	Amount    int    `yaml:"amount" json:"amount"`
	Resource  string `yaml:"resource" json:"resource"`
	Duration  int    `yaml:"duration" json:"duration"`
	Item      string `yaml:"item" json:"item"`
	Qty       int    `yaml:"qty" json:"qty"`
	Recipe    string `yaml:"recipe" json:"recipe"`
}

// DefaultConfig returns a Config populated with standard defaults.
func DefaultConfig() *Config {
	return &Config{
		Client: ClientConfig{
			AgentID:          "eldon_millwright",
			Password:         "secret_eldon_123",
			BrokerURL:        "tcp://localhost:1883",
			ClientID:         "",
			KeepAliveSeconds: 60,
		},
		Model: ModelConfig{
			Provider: "mock",
			LlamaPure: LlamaPureConfig{
				LibraryPath:   "/usr/local/lib/libllama.so",
				ModelPath:     "",
				ContextSize:   2048,
				Threads:       4,
				GPULayers:     0,
				MaxTokens:     256,
				Temperature:   0.7,
				TopP:          0.9,
				RepeatPenalty: 1.15,
				GrammarFile:   "",
			},
			Cloud: CloudModelConfig{
				APIBase:        "https://api.openai.com/v1",
				APIKey:         "",
				ModelName:      "gpt-4o-mini",
				TimeoutSeconds: 30,
				MaxTokens:      256,
			},
		},
		Memory: MemoryConfig{
			DBPath:                  "~/.agentmud/memory.db",
			MaxReflectionsPerPeer:   5,
			SynthesisTimeoutSeconds: 60,
		},
		Cognition: CognitionConfig{
			TickIntervalDebounceMS:  2000,
			ChatResponseProbability: 0.85,
			MaxSensoryLogLines:      12,
			ReflectionIntervalTicks: 30,
			IdleThresholdTicks:      10,
		},
		Action: ActionConfig{
			TargetLoc: "tavern",
			ChatMsg:   "Hello village!",
			Recipient: "martha_barkeep",
			Amount:    5,
			Resource:  "firewood",
			Duration:  60,
			Item:      "firewood",
			Qty:       1,
			Recipe:    "mill_flour",
		},
	}
}

// LoadConfigFile loads YAML or JSON configuration from the specified path into cfg.
func LoadConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse config file %q: %w", path, err)
	}

	cfg.ConfigFile = path
	return nil
}

// ResolveConfig resolves configuration adhering to the strict hierarchy:
// CLI Flags > Environment Variables > Config File (agent.yaml or custom) > Built-in Defaults.
func ResolveConfig(cmd *cli.Command) (*Config, error) {
	cfg := DefaultConfig()

	// 1. Determine config file path
	configPath := ""
	explicitConfig := false
	if cmd != nil && cmd.IsSet("config") {
		configPath = cmd.String("config")
		explicitConfig = true
	} else if envPath := os.Getenv("AGENTMUD_CONFIG"); envPath != "" {
		configPath = envPath
		explicitConfig = true
	} else {
		// Auto-discover default agent.yaml or agent.yml in current directory
		for _, candidate := range []string{"agent.yaml", "agent.yml"} {
			if _, err := os.Stat(candidate); err == nil {
				configPath = candidate
				break
			}
		}
	}

	// 2. Load config file if found or explicitly required
	if configPath != "" {
		if err := LoadConfigFile(configPath, cfg); err != nil {
			if explicitConfig {
				return nil, err
			}
			// If auto-discovered and unparseable, return error
			return nil, fmt.Errorf("error loading discovered config %q: %w", configPath, err)
		}
	}

	// 3. Environment Variable Overrides
	applyEnvOverrides(cfg)

	// 4. CLI Flag Overrides (highest precedence)
	if cmd != nil {
		applyCLIOverrides(cmd, cfg)
	}

	return cfg, nil
}

// applyEnvOverrides updates cfg with any non-empty environment variables.
func applyEnvOverrides(cfg *Config) {
	if val := getFirstEnv("AGENTMUD_BROKER", "MQTT_BROKER"); val != "" {
		cfg.Client.BrokerURL = val
	}
	if val := getFirstEnv("AGENTMUD_AGENT", "AGENTMUD_AGENT_ID"); val != "" {
		cfg.Client.AgentID = val
	}
	if val := getFirstEnv("AGENTMUD_PASS", "AGENTMUD_PASSWORD"); val != "" {
		cfg.Client.Password = val
	}
	if val := os.Getenv("AGENTMUD_CLIENT_ID"); val != "" {
		cfg.Client.ClientID = val
	}
	if val := os.Getenv("AGENTMUD_KEEPALIVE"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Client.KeepAliveSeconds = n
		}
	}
	if val := os.Getenv("AGENTMUD_TUI"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.Client.TUI = b
		}
	}
	if val := getFirstEnv("AGENTMUD_LOG_FILE", "AGENTMUD_LOG"); val != "" {
		cfg.Client.LogFile = val
	}

	if val := getFirstEnv("AGENTMUD_ENGINE", "AGENTMUD_MODEL_PROVIDER"); val != "" {
		cfg.Model.Provider = val
	}
	if val := os.Getenv("AGENTMUD_MODEL_PATH"); val != "" {
		cfg.Model.LlamaPure.ModelPath = val
	}
	if val := os.Getenv("AGENTMUD_LLAMA_LIB"); val != "" {
		cfg.Model.LlamaPure.LibraryPath = val
	}
	if val := os.Getenv("AGENTMUD_THREADS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Model.LlamaPure.Threads = n
		}
	}
	if val := os.Getenv("AGENTMUD_CONTEXT_SIZE"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Model.LlamaPure.ContextSize = n
		}
	}
	if val := os.Getenv("AGENTMUD_GPU_LAYERS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Model.LlamaPure.GPULayers = n
		}
	}
	if val := os.Getenv("AGENTMUD_LLAMA_MAX_TOKENS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Model.LlamaPure.MaxTokens = n
		}
	}
	if val := os.Getenv("AGENTMUD_TEMPERATURE"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			cfg.Model.LlamaPure.Temperature = f
		}
	}
	if val := os.Getenv("AGENTMUD_TOP_P"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			cfg.Model.LlamaPure.TopP = f
		}
	}
	if val := os.Getenv("AGENTMUD_GRAMMAR_FILE"); val != "" {
		cfg.Model.LlamaPure.GrammarFile = val
	}

	if val := getFirstEnv("AGENTMUD_API_KEY", "OPENAI_API_KEY"); val != "" {
		cfg.Model.Cloud.APIKey = val
	}
	if val := os.Getenv("AGENTMUD_API_BASE"); val != "" {
		cfg.Model.Cloud.APIBase = val
	}
	if val := os.Getenv("AGENTMUD_MODEL_NAME"); val != "" {
		cfg.Model.Cloud.ModelName = val
	}
	if val := os.Getenv("AGENTMUD_TIMEOUT"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Model.Cloud.TimeoutSeconds = n
		}
	}
	if val := os.Getenv("AGENTMUD_MAX_TOKENS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Model.Cloud.MaxTokens = n
		}
	}

	if val := os.Getenv("AGENTMUD_DEBOUNCE_MS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Cognition.TickIntervalDebounceMS = n
		}
	}
	if val := os.Getenv("AGENTMUD_CHAT_PROB"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			cfg.Cognition.ChatResponseProbability = f
		}
	}
	if val := os.Getenv("AGENTMUD_IDLE_THRESHOLD"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Cognition.IdleThresholdTicks = n
		}
	}

	if val := os.Getenv("AGENTMUD_ACTION"); val != "" {
		cfg.Action.Action = val
	}
}

// applyCLIOverrides updates cfg only with flags that were explicitly provided on the CLI.
// Note: In urfave/cli v3, cmd.IsSet(name) returns true if the flag was provided via command line.
func applyCLIOverrides(cmd *cli.Command, cfg *Config) {
	// Client flags
	if cmd.IsSet("broker") {
		cfg.Client.BrokerURL = cmd.String("broker")
	}
	if cmd.IsSet("agent") {
		cfg.Client.AgentID = cmd.String("agent")
	}
	if cmd.IsSet("pass") {
		cfg.Client.Password = cmd.String("pass")
	}
	if cmd.IsSet("client-id") {
		cfg.Client.ClientID = cmd.String("client-id")
	}
	if cmd.IsSet("keepalive") {
		cfg.Client.KeepAliveSeconds = int(cmd.Int("keepalive"))
	}
	if cmd.IsSet("tui") {
		cfg.Client.TUI = cmd.Bool("tui")
	}
	if cmd.IsSet("log-file") {
		cfg.Client.LogFile = cmd.String("log-file")
	}

	// Model flags
	if cmd.IsSet("engine") {
		cfg.Model.Provider = cmd.String("engine")
	}
	if cmd.IsSet("model-path") {
		cfg.Model.LlamaPure.ModelPath = cmd.String("model-path")
	}
	if cmd.IsSet("llama-lib") {
		cfg.Model.LlamaPure.LibraryPath = cmd.String("llama-lib")
	}
	if cmd.IsSet("threads") {
		cfg.Model.LlamaPure.Threads = int(cmd.Int("threads"))
	}
	if cmd.IsSet("context-size") {
		cfg.Model.LlamaPure.ContextSize = int(cmd.Int("context-size"))
	}
	if cmd.IsSet("gpu-layers") {
		cfg.Model.LlamaPure.GPULayers = int(cmd.Int("gpu-layers"))
	}
	if cmd.IsSet("temperature") {
		cfg.Model.LlamaPure.Temperature = cmd.Float("temperature")
	}
	if cmd.IsSet("top-p") {
		cfg.Model.LlamaPure.TopP = cmd.Float("top-p")
	}
	if cmd.IsSet("grammar-file") {
		cfg.Model.LlamaPure.GrammarFile = cmd.String("grammar-file")
	}

	// Cloud flags
	if cmd.IsSet("api-key") {
		cfg.Model.Cloud.APIKey = cmd.String("api-key")
	}
	if cmd.IsSet("api-base") {
		cfg.Model.Cloud.APIBase = cmd.String("api-base")
	}
	if cmd.IsSet("model-name") {
		cfg.Model.Cloud.ModelName = cmd.String("model-name")
	}
	if cmd.IsSet("timeout") {
		cfg.Model.Cloud.TimeoutSeconds = int(cmd.Int("timeout"))
	}
	if cmd.IsSet("max-tokens") {
		cfg.Model.Cloud.MaxTokens = int(cmd.Int("max-tokens"))
	}

	// Cognition flags
	if cmd.IsSet("debounce") {
		cfg.Cognition.TickIntervalDebounceMS = int(cmd.Int("debounce"))
	}
	if cmd.IsSet("chat-prob") {
		cfg.Cognition.ChatResponseProbability = cmd.Float("chat-prob")
	}
	if cmd.IsSet("idle-threshold") {
		cfg.Cognition.IdleThresholdTicks = int(cmd.Int("idle-threshold"))
	}

	// Action flags
	if cmd.IsSet("action") {
		cfg.Action.Action = cmd.String("action")
	}
	if cmd.IsSet("target") {
		cfg.Action.TargetLoc = cmd.String("target")
	}
	if cmd.IsSet("msg") {
		cfg.Action.ChatMsg = cmd.String("msg")
	}
	if cmd.IsSet("recipient") {
		cfg.Action.Recipient = cmd.String("recipient")
	}
	if cmd.IsSet("amount") {
		cfg.Action.Amount = int(cmd.Int("amount"))
	}
	if cmd.IsSet("resource") {
		cfg.Action.Resource = cmd.String("resource")
	}
	if cmd.IsSet("duration") {
		cfg.Action.Duration = int(cmd.Int("duration"))
	}
	if cmd.IsSet("item") {
		cfg.Action.Item = cmd.String("item")
	}
	if cmd.IsSet("qty") {
		cfg.Action.Qty = int(cmd.Int("qty"))
	}
	if cmd.IsSet("recipe") {
		cfg.Action.Recipe = cmd.String("recipe")
	}
}

func getFirstEnv(keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return ""
}

// ToJSON formats the configuration as indented JSON for debugging or display.
func (c *Config) ToJSON() (string, error) {
	bytes, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ToYAML formats the configuration as YAML.
func (c *Config) ToYAML() (string, error) {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
