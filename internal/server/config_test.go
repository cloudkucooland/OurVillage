package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDatabasePath(t *testing.T) {
	t.Setenv("OV_SERVER_CONFIG", "")
	t.Setenv("OV_DB", "")
	if got := DefaultConfig().Server.DBPath; got != "agentmud.db" {
		t.Fatalf("default DB path = %q, want agentmud.db", got)
	}

	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte("server:\n  db_path: live.db\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.DBPath != "live.db" {
		t.Fatalf("configured DB path = %q, want live.db", cfg.Server.DBPath)
	}
}

func TestLoadConfigDatabaseEnvironmentOverride(t *testing.T) {
	t.Setenv("OV_SERVER_CONFIG", "")
	t.Setenv("OV_DB", "override.db")
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte("server:\n  db_path: live.db\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.DBPath != "override.db" {
		t.Fatalf("environment DB path = %q, want override.db", cfg.Server.DBPath)
	}
}
