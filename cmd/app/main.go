package main

import (
	"log"

	"github.com/divilla/go-scream-template/config"
	"github.com/divilla/go-scream-template/internal/app"
)

func main() {
	// Configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Config error: %s", err)
	}

	// Run
	app.Run(cfg)
}
