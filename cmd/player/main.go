package main

import (
	"context"
	"log"
	"os"

	"agentmud/internal/client"
)

func main() {
	app := client.NewPlayerApp()
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatalf("Fatal: %v", err)
	}
}
