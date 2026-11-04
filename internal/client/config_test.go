package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Client.AgentID != "eldon_millwright" {
		t.Fatalf("expected default agent eldon_millwright, got %s", cfg.Client.AgentID)
	}
	if cfg.Client.BrokerURL != "tcp://localhost:1883" {
		t.Fatalf("expected default broker tcp://localhost:1883, got %s", cfg.Client.BrokerURL)
	}
	if cfg.Client.KeepAliveSeconds != 60 {
		t.Fatalf("expected keepalive 60, got %d", cfg.Client.KeepAliveSeconds)
	}
	if cfg.Model.Provider != "mock" {
		t.Fatalf("expected default model mock, got %s", cfg.Model.Provider)
	}
	if cfg.Model.LlamaPure.ContextSize != 2048 {
		t.Fatalf("expected context size 2048, got %d", cfg.Model.LlamaPure.ContextSize)
	}
	if cfg.Cognition.TickIntervalDebounceMS != 2000 {
		t.Fatalf("expected debounce 2000, got %d", cfg.Cognition.TickIntervalDebounceMS)
	}
}

func TestLoadConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	yamlData := `
client:
  agent_id: "martha_barkeep"
  password: "martha_secret_pass"
  broker_url: "tcp://192.168.1.100:1883"
  client_id: "martha_custom_id"
  keepalive_seconds: 120

model:
  provider: "cloud-openai"
  cloud:
    api_base: "https://custom.openai.api/v1"
    api_key: "sk-test-key-123"
    model_name: "gpt-4o"
    timeout_seconds: 30
    max_tokens: 512

cognition:
  tick_interval_debounce_ms: 3500
  chat_response_probability: 0.95
`

	if err := os.WriteFile(configPath, []byte(yamlData), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg := DefaultConfig()
	if err := LoadConfigFile(configPath, cfg); err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.Client.AgentID != "martha_barkeep" {
		t.Errorf("expected martha_barkeep, got %s", cfg.Client.AgentID)
	}
	if cfg.Client.Password != "martha_secret_pass" {
		t.Errorf("expected martha_secret_pass, got %s", cfg.Client.Password)
	}
	if cfg.Client.BrokerURL != "tcp://192.168.1.100:1883" {
		t.Errorf("expected tcp://192.168.1.100:1883, got %s", cfg.Client.BrokerURL)
	}
	if cfg.Client.ClientID != "martha_custom_id" {
		t.Errorf("expected martha_custom_id, got %s", cfg.Client.ClientID)
	}
	if cfg.Client.KeepAliveSeconds != 120 {
		t.Errorf("expected keepalive 120, got %d", cfg.Client.KeepAliveSeconds)
	}
	if cfg.Model.Provider != "cloud-openai" {
		t.Errorf("expected cloud-openai, got %s", cfg.Model.Provider)
	}
	if cfg.Model.Cloud.APIBase != "https://custom.openai.api/v1" {
		t.Errorf("expected https://custom.openai.api/v1, got %s", cfg.Model.Cloud.APIBase)
	}
	if cfg.Model.Cloud.APIKey != "sk-test-key-123" {
		t.Errorf("expected sk-test-key-123, got %s", cfg.Model.Cloud.APIKey)
	}
	if cfg.Model.Cloud.ModelName != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", cfg.Model.Cloud.ModelName)
	}
	if cfg.Cognition.TickIntervalDebounceMS != 3500 {
		t.Errorf("expected debounce 3500, got %d", cfg.Cognition.TickIntervalDebounceMS)
	}
	if cfg.Cognition.ChatResponseProbability != 0.95 {
		t.Errorf("expected chat response prob 0.95, got %f", cfg.Cognition.ChatResponseProbability)
	}
}

func TestHierarchy_EnvOverridesConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	yamlData := `
client:
  agent_id: "config_agent"
  broker_url: "tcp://config-broker:1883"
model:
  provider: "mock"
`
	if err := os.WriteFile(configPath, []byte(yamlData), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	t.Setenv("AGENTMUD_AGENT", "env_agent")
	t.Setenv("AGENTMUD_BROKER", "tcp://env-broker:1883")
	t.Setenv("AGENTMUD_CONFIG", configPath)

	app := NewApp()
	var resolved *Config
	app.Action = func(ctx context.Context, cmd *cli.Command) error {
		var err error
		resolved, err = ResolveConfig(cmd)
		return err
	}

	if err := app.Run(context.Background(), []string{"agentmud-client"}); err != nil {
		t.Fatalf("unexpected error running app: %v", err)
	}

	if resolved == nil {
		t.Fatal("expected resolved config, got nil")
	}

	// Environment variable must override config file value
	if resolved.Client.AgentID != "env_agent" {
		t.Errorf("expected env_agent, got %s", resolved.Client.AgentID)
	}
	if resolved.Client.BrokerURL != "tcp://env-broker:1883" {
		t.Errorf("expected tcp://env-broker:1883, got %s", resolved.Client.BrokerURL)
	}
	// Config file value should be retained for non-overridden fields
	if resolved.Model.Provider != "mock" {
		t.Errorf("expected model mock, got %s", resolved.Model.Provider)
	}
}

func TestHierarchy_CLIOverridesEnvAndConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "agent.yaml")

	yamlData := `
client:
  agent_id: "config_agent"
  broker_url: "tcp://config-broker:1883"
model:
  provider: "mock"
`
	if err := os.WriteFile(configPath, []byte(yamlData), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	t.Setenv("AGENTMUD_AGENT", "env_agent")
	t.Setenv("AGENTMUD_BROKER", "tcp://env-broker:1883")
	t.Setenv("AGENTMUD_ENGINE", "cloud-openai")

	app := NewApp()
	var resolved *Config
	app.Action = func(ctx context.Context, cmd *cli.Command) error {
		var err error
		resolved, err = ResolveConfig(cmd)
		return err
	}

	args := []string{
		"agentmud-client",
		"--config", configPath,
		"--agent", "cli_agent",
		"--broker", "tcp://cli-broker:1883",
		"--engine", "llama-pure",
	}

	if err := app.Run(context.Background(), args); err != nil {
		t.Fatalf("unexpected error running app: %v", err)
	}

	if resolved == nil {
		t.Fatal("expected resolved config, got nil")
	}

	// CLI flags MUST override both environment variables and config file
	if resolved.Client.AgentID != "cli_agent" {
		t.Errorf("expected cli_agent, got %s", resolved.Client.AgentID)
	}
	if resolved.Client.BrokerURL != "tcp://cli-broker:1883" {
		t.Errorf("expected tcp://cli-broker:1883, got %s", resolved.Client.BrokerURL)
	}
	if resolved.Model.Provider != "llama-pure" {
		t.Errorf("expected llama-pure, got %s", resolved.Model.Provider)
	}
}

func TestMissingExplicitConfigFails(t *testing.T) {
	app := NewApp()
	app.Action = func(ctx context.Context, cmd *cli.Command) error {
		_, err := ResolveConfig(cmd)
		return err
	}

	err := app.Run(context.Background(), []string{"agentmud-client", "--config", "/nonexistent/path/agent.yaml"})
	if err == nil {
		t.Fatal("expected error for nonexistent config file, got nil")
	}
}

func TestConfigToYAMLAndJSON(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Client.AgentID = "test_agent"

	yamlStr, err := cfg.ToYAML()
	if err != nil {
		t.Fatalf("failed to generate YAML: %v", err)
	}
	if yamlStr == "" {
		t.Fatal("YAML string is empty")
	}

	jsonStr, err := cfg.ToJSON()
	if err != nil {
		t.Fatalf("failed to generate JSON: %v", err)
	}
	if jsonStr == "" {
		t.Fatal("JSON string is empty")
	}
}
