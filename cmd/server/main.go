package main

import (
	"log"
	"os"
	"strings"

	"agentmud/internal/db"
	"agentmud/internal/server"
)

func main() {
	cfg, err := server.LoadConfig("")
	if err != nil {
		log.Fatalf("Failed to load server config: %v", err)
	}
	dbPath := cfg.Server.DBPath
	if dbPath == "" {
		dbPath = "agentmud.db"
	}

	useTUI := false
	if os.Getenv("AGENTMUD_TUI") == "true" || os.Getenv("AGENTMUD_TUI") == "1" {
		useTUI = true
	}
	doSeed := false
	if os.Getenv("AGENTMUD_SEED") == "true" || os.Getenv("AGENTMUD_SEED") == "1" {
		doSeed = true
	}
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--tui" {
			useTUI = true
		}
		if arg == "--seed" {
			doSeed = true
		}
		if arg == "--config" && i+1 < len(args) {
			i++
			cfg, err = server.LoadConfig(args[i])
			if err != nil {
				log.Fatalf("Failed to load server config %q: %v", args[i], err)
			}
			dbPath = cfg.Server.DBPath
			if dbPath == "" {
				dbPath = "agentmud.db"
			}
		} else if strings.HasPrefix(arg, "--config=") {
			configPath := strings.TrimPrefix(arg, "--config=")
			cfg, err = server.LoadConfig(configPath)
			if err != nil {
				log.Fatalf("Failed to load server config %q: %v", configPath, err)
			}
			dbPath = cfg.Server.DBPath
			if dbPath == "" {
				dbPath = "agentmud.db"
			}
		}
	}

	log.Printf("Initializing OurVillage database at %s...", dbPath)
	database, err := db.InitDB(dbPath, doSeed)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	srv, err := server.NewServerWithConfig(database, cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	if useTUI {
		log.Println("Starting OurVillage server with TUI...")
		if err := srv.StartTUI(); err != nil {
			log.Fatalf("Server stopped with error: %v", err)
		}
	} else {
		log.Println("OurVillage server initialized successfully. Starting engine...")
		if err := srv.Start(); err != nil {
			log.Fatalf("Server stopped with error: %v", err)
		}
	}
}
