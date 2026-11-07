package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"agentmud/internal/client/spatial"

	"github.com/urfave/cli/v3"
)

// CommonFlags returns the comprehensive flag set for agentmud-client.
func CommonFlags() []cli.Flag {
	return []cli.Flag{
		// Config file
		&cli.StringFlag{
			Name:      "config",
			Aliases:   []string{"c"},
			Usage:     "Path to agent configuration YAML/JSON file",
			TakesFile: true,
			Sources:   cli.EnvVars("AGENTMUD_CONFIG"),
		},

		// Client / Network flags
		&cli.StringFlag{
			Name:    "broker",
			Usage:   "MQTT broker URL",
			Value:   "tcp://localhost:1883",
			Sources: cli.EnvVars("AGENTMUD_BROKER", "MQTT_BROKER"),
		},
		&cli.StringFlag{
			Name:    "agent",
			Usage:   "Agent ID (MQTT username)",
			Value:   "eldon_millwright",
			Sources: cli.EnvVars("AGENTMUD_AGENT", "AGENTMUD_AGENT_ID"),
		},
		&cli.StringFlag{
			Name:    "pass",
			Usage:   "Agent MQTT password",
			Value:   "secret_eldon_123",
			Sources: cli.EnvVars("AGENTMUD_PASS", "AGENTMUD_PASSWORD"),
		},
		&cli.StringFlag{
			Name:    "client-id",
			Usage:   "MQTT client ID (auto-generated if empty)",
			Sources: cli.EnvVars("AGENTMUD_CLIENT_ID"),
		},
		&cli.IntFlag{
			Name:    "keepalive",
			Usage:   "MQTT keepalive timeout in seconds",
			Value:   60,
			Sources: cli.EnvVars("AGENTMUD_KEEPALIVE"),
		},
		&cli.BoolFlag{
			Name:    "tui",
			Usage:   "Launch interactive Bubble Tea / Lipgloss terminal user interface",
			Value:   false,
			Sources: cli.EnvVars("AGENTMUD_TUI"),
		},
		&cli.StringFlag{
			Name:      "log-file",
			Usage:     "Path to client log file",
			TakesFile: true,
			Sources:   cli.EnvVars("AGENTMUD_LOG_FILE", "AGENTMUD_LOG"),
		},

		// Model Provider flags
		&cli.StringFlag{
			Name:    "engine",
			Usage:   "Model engine provider (mock, llama-pure, rkllm, cloud-openai, cloud-gemini, cloud-anthropic)",
			Value:   "mock",
			Sources: cli.EnvVars("AGENTMUD_ENGINE", "AGENTMUD_MODEL_PROVIDER"),
		},
		&cli.StringFlag{
			Name:      "model-path",
			Usage:     "GGUF model file path for llama-pure",
			TakesFile: true,
			Sources:   cli.EnvVars("AGENTMUD_MODEL_PATH"),
		},
		&cli.StringFlag{
			Name:      "llama-lib",
			Usage:     "Path to dynamic libllama.so library",
			Value:     "/usr/local/lib/libllama.so",
			TakesFile: true,
			Sources:   cli.EnvVars("AGENTMUD_LLAMA_LIB"),
		},
		&cli.IntFlag{
			Name:    "threads",
			Usage:   "CPU inference thread count for local model",
			Value:   4,
			Sources: cli.EnvVars("AGENTMUD_THREADS"),
		},
		&cli.IntFlag{
			Name:    "context-size",
			Usage:   "Context window size in tokens",
			Value:   2048,
			Sources: cli.EnvVars("AGENTMUD_CONTEXT_SIZE"),
		},
		&cli.IntFlag{
			Name:    "gpu-layers",
			Usage:   "Number of layers to offload to GPU/NPU",
			Value:   0,
			Sources: cli.EnvVars("AGENTMUD_GPU_LAYERS"),
		},
		&cli.FloatFlag{
			Name:    "temperature",
			Usage:   "Sampling temperature for model generation",
			Value:   0.7,
			Sources: cli.EnvVars("AGENTMUD_TEMPERATURE"),
		},
		&cli.FloatFlag{
			Name:    "top-p",
			Usage:   "Top-P sampling parameter",
			Value:   0.9,
			Sources: cli.EnvVars("AGENTMUD_TOP_P"),
		},
		&cli.StringFlag{
			Name:      "grammar-file",
			Usage:     "Path to GBNF grammar file for constrained JSON decoding",
			TakesFile: true,
			Sources:   cli.EnvVars("AGENTMUD_GRAMMAR_FILE"),
		},

		// Cloud Provider flags
		&cli.StringFlag{
			Name:    "api-key",
			Usage:   "API key for remote cloud model provider",
			Sources: cli.EnvVars("AGENTMUD_API_KEY", "OPENAI_API_KEY"),
		},
		&cli.StringFlag{
			Name:    "api-base",
			Usage:   "API base URL for remote cloud provider",
			Value:   "https://api.openai.com/v1",
			Sources: cli.EnvVars("AGENTMUD_API_BASE"),
		},
		&cli.StringFlag{
			Name:    "model-name",
			Usage:   "Remote model identifier (e.g. gpt-4o-mini)",
			Value:   "gpt-4o-mini",
			Sources: cli.EnvVars("AGENTMUD_MODEL_NAME"),
		},
		&cli.IntFlag{
			Name:    "timeout",
			Usage:   "Timeout in seconds for remote API requests",
			Value:   15,
			Sources: cli.EnvVars("AGENTMUD_TIMEOUT"),
		},
		&cli.IntFlag{
			Name:    "max-tokens",
			Usage:   "Maximum completion tokens to generate",
			Value:   256,
			Sources: cli.EnvVars("AGENTMUD_MAX_TOKENS"),
		},

		// Cognition flags
		&cli.IntFlag{
			Name:    "debounce",
			Usage:   "Tick interval debounce in milliseconds",
			Value:   2000,
			Sources: cli.EnvVars("AGENTMUD_DEBOUNCE_MS"),
		},
		&cli.FloatFlag{
			Name:    "chat-prob",
			Usage:   "Probability (0.0 - 1.0) of responding to ambient room chat",
			Value:   0.85,
			Sources: cli.EnvVars("AGENTMUD_CHAT_PROB"),
		},
		&cli.IntFlag{
			Name:    "idle-threshold",
			Usage:   "Number of consecutive quiet time ticks before triggering intrinsic motivation",
			Value:   10,
			Sources: cli.EnvVars("AGENTMUD_IDLE_THRESHOLD"),
		},

		// Action flags
		&cli.StringFlag{
			Name:    "action",
			Usage:   "Intent action to submit (MOVE, CHAT, TRANSFER, FORAGE, CANCEL_ACTION, DROP, PICKUP, EQUIP, UNEQUIP, CRAFT)",
			Sources: cli.EnvVars("AGENTMUD_ACTION"),
		},
		&cli.StringFlag{
			Name:  "target",
			Usage: "Target location ID for MOVE intent",
			Value: "tavern",
		},
		&cli.StringFlag{
			Name:  "msg",
			Usage: "Chat message for CHAT intent",
			Value: "Hello village!",
		},
		&cli.StringFlag{
			Name:  "recipient",
			Usage: "Recipient agent ID for TRANSFER intent",
			Value: "martha_barkeep",
		},
		&cli.IntFlag{
			Name:  "amount",
			Usage: "Gold amount for TRANSFER intent",
			Value: 5,
		},
		&cli.StringFlag{
			Name:  "resource",
			Usage: "Target resource for FORAGE intent",
			Value: "firewood",
		},
		&cli.IntFlag{
			Name:  "duration",
			Usage: "Duration ticks for FORAGE intent",
			Value: 60,
		},
		&cli.StringFlag{
			Name:  "item",
			Usage: "Item ID for DROP, PICKUP, EQUIP, or TRANSFER",
			Value: "firewood",
		},
		&cli.IntFlag{
			Name:  "qty",
			Usage: "Quantity for DROP, PICKUP, or TRANSFER",
			Value: 1,
		},
		&cli.StringFlag{
			Name:  "recipe",
			Usage: "Recipe ID for CRAFT intent",
			Value: "mill_flour",
		},
	}
}

// NewApp creates the root urfave/cli v3 Command for agentmud-client.
func NewApp() *cli.Command {
	flags := CommonFlags()

	return &cli.Command{
		Name:        "agentmud-client",
		Usage:       "Autonomous agent daemon and client harness for OurVillage",
		Version:     "0.2.0",
		Description: "Powers cognitive lifecycles, sensory processing, and intent dispatch for OurVillage residents.",
		Flags:       flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := ResolveConfig(cmd)
			if err != nil {
				return fmt.Errorf("configuration error: %w", err)
			}
			return RunClient(ctx, cfg)
		},
		Commands: []*cli.Command{
			{
				Name:  "prompt",
				Usage: "Display assembled RFC 004 context window prompt",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := ResolveConfig(cmd)
					if err != nil {
						return fmt.Errorf("configuration error: %w", err)
					}
					initLoc := cfg.Action.TargetLoc
					if initLoc == "" {
						initLoc = "town_square"
					}
					ctxMgr := spatial.NewContextManager(spatial.ContextManagerConfig{
						AgentID:         cfg.Client.AgentID,
						InitialLocation: initLoc,
						MaxSensoryLines: cfg.Cognition.MaxSensoryLogLines,
						SmallSystem:     cfg.Cognition.SmallSystem,
					})
					fmt.Print(ctxMgr.BuildPrompt([]spatial.PeerMemoryContext{}, nil))
					return nil
				},
			},
			{
				Name:  "config",
				Usage: "Inspect and validate configuration",
				Commands: []*cli.Command{
					{
						Name:  "show",
						Usage: "Display the effective resolved configuration",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "format",
								Aliases: []string{"f"},
								Usage:   "Output format (yaml or json)",
								Value:   "yaml",
							},
						},
						Action: func(ctx context.Context, cmd *cli.Command) error {
							cfg, err := ResolveConfig(cmd)
							if err != nil {
								return fmt.Errorf("configuration error: %w", err)
							}

							format := strings.ToLower(cmd.String("format"))
							if format == "json" {
								out, err := cfg.ToJSON()
								if err != nil {
									return err
								}
								fmt.Println(out)
							} else {
								out, err := cfg.ToYAML()
								if err != nil {
									return err
								}
								fmt.Println(out)
							}
							return nil
						},
					},
					{
						Name:  "validate",
						Usage: "Validate syntax and basic sanity of the configuration",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							cfg, err := ResolveConfig(cmd)
							if err != nil {
								return fmt.Errorf("configuration invalid: %w", err)
							}

							if cfg.Client.AgentID == "" {
								return fmt.Errorf("client.agent_id cannot be empty")
							}
							if cfg.Client.BrokerURL == "" {
								return fmt.Errorf("client.broker_url cannot be empty")
							}
							if _, err := url.Parse(cfg.Client.BrokerURL); err != nil {
								return fmt.Errorf("invalid broker URL %q: %w", cfg.Client.BrokerURL, err)
							}

							fmt.Println("Configuration is valid.")
							if cfg.ConfigFile != "" {
								fmt.Printf("Loaded from: %s\n", cfg.ConfigFile)
							} else {
								fmt.Println("Source: Built-in defaults and environment variables")
							}
							fmt.Printf("Agent ID:   %s\n", cfg.Client.AgentID)
							fmt.Printf("Broker:     %s\n", cfg.Client.BrokerURL)
							fmt.Printf("Engine:     %s\n", cfg.Model.Provider)
							return nil
						},
					},
				},
			},
		},
	}
}
