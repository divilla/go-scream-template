//go:build migrate

package app

import (
	"errors"
	"log"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	// migrate tools
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const (
	_defaultAttempts = 20
	_defaultTimeout  = time.Second
)

type migration interface {
	Up() error
	Close() (error, error)
}

type migrationDependencies struct {
	open   func(string, string) (migration, error)
	sleep  func(time.Duration)
	printf func(string, ...any)
	fatalf func(string, ...any)
}

// Migrate applies database migrations before application configuration is loaded.
func Migrate() {
	runMigrations(os.Getenv("PG_URL"), migrationDependencies{
		open: openMigration, sleep: time.Sleep, printf: log.Printf, fatalf: log.Fatalf,
	})
}

func openMigration(sourceURL, databaseURL string) (migration, error) {
	return migrate.New(sourceURL, databaseURL)
}

func runMigrations(databaseURL string, deps migrationDependencies) {
	if databaseURL == "" {
		deps.fatalf("migrate: environment variable not declared: PG_URL")

		return
	}

	databaseURL += "?sslmode=disable"

	var (
		attempts = _defaultAttempts
		err      error
		m        migration
	)

	for attempts > 0 {
		m, err = deps.open("file://migrations", databaseURL)
		if err == nil {
			break
		}

		deps.printf("Migrate: postgres is trying to connect, attempts left: %d", attempts)
		deps.sleep(_defaultTimeout)

		attempts--
	}

	if err != nil {
		deps.fatalf("Migrate: postgres connect error: %s", err)

		return
	}

	err = m.Up()
	defer m.Close()

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		deps.fatalf("Migrate: up error: %s", err)

		return
	}

	if errors.Is(err, migrate.ErrNoChange) {
		deps.printf("Migrate: no change")

		return
	}

	deps.printf("Migrate: up success")
}
