package main

import (
	"log"
	"os"

	"agentmud/internal/db"
	"agentmud/internal/server"
)

func main() {
	dbPath := "agentmud.db"
	if envPath := os.Getenv("AGENTMUD_DB"); envPath != "" {
		dbPath = envPath
	}

	useTUI := false
	if os.Getenv("AGENTMUD_TUI") == "true" || os.Getenv("AGENTMUD_TUI") == "1" {
		useTUI = true
	}
	for _, arg := range os.Args {
		if arg == "--tui" {
			useTUI = true
		}
	}

	log.Printf("Initializing AgentMUD database at %s...", dbPath)
	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	srv, err := server.NewServer(database)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	if useTUI {
		log.Println("Starting AgentMUD server with TUI...")
		if err := srv.StartTUI(); err != nil {
			log.Fatalf("Server stopped with error: %v", err)
		}
	} else {
		log.Println("AgentMUD server initialized successfully. Starting engine...")
		if err := srv.Start(); err != nil {
			log.Fatalf("Server stopped with error: %v", err)
		}
	}
}
