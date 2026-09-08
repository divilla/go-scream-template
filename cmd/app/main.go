package main

import (
	"log"

	"github.com/divilla/go-scream-template/config"
	"github.com/divilla/go-scream-template/internal/app"
)

//nolint:gochecknoglobals // The process boundary is replaceable in unit tests without exiting or starting external services.
var entrypoint = struct {
	migrate func()
	config  func() (*config.Config, error)
	run     func(*config.Config)
	fatal   func(string, ...any)
}{app.Migrate, config.NewConfig, app.Run, log.Fatalf}

func main() {
	entrypoint.migrate()

	// Configuration
	cfg, err := entrypoint.config()
	if err != nil {
		entrypoint.fatal("Config error: %s", err)

		return
	}

	// Run
	entrypoint.run(cfg)
}
