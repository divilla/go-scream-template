//go:build migrate

package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type migrationStub struct {
	err        error
	up, closed bool
}

func (m *migrationStub) Up() error {
	m.up = true

	return m.err
}

func (m *migrationStub) Close() (sourceErr, databaseErr error) {
	m.closed = true

	return nil, nil
}

func TestRunMigrations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, url        string
		failures         int
		upError          error
		attempts, sleeps int
		fatal, result    string
	}{
		{name: "missing URL", fatal: "migrate: environment variable not declared: PG_URL"},
		{name: "success", url: "postgres://db", attempts: 1, result: "Migrate: up success"},
		{name: "retry", url: "postgres://db", failures: 2, attempts: 3, sleeps: 2, result: "Migrate: up success"},
		{name: "last attempt", url: "postgres://db", failures: 19, attempts: 20, sleeps: 19, result: "Migrate: up success"},
		{name: "exhausted", url: "postgres://db", failures: 20, attempts: 20, sleeps: 20, fatal: "Migrate: postgres connect error: unexpected EOF"},
		{name: "up failure", url: "postgres://db", attempts: 1, upError: io.ErrUnexpectedEOF, fatal: "Migrate: up error: unexpected EOF"},
		{name: "no change", url: "postgres://db", attempts: 1, upError: migrate.ErrNoChange, result: "Migrate: no change"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := &migrationStub{err: tc.upError}
			attempts, sleeps := 0, 0

			var fatal string

			var messages []string

			runMigrations(tc.url, migrationDependencies{
				open: func(source, database string) (migration, error) {
					assert.Equal(t, "file://migrations", source)
					assert.Equal(t, tc.url+"?sslmode=disable", database)

					attempts++
					if attempts <= tc.failures {
						return nil, io.ErrUnexpectedEOF
					}

					return m, nil
				},
				sleep:  func(delay time.Duration) { assert.Equal(t, time.Second, delay); sleeps++ },
				printf: func(format string, args ...any) { messages = append(messages, fmt.Sprintf(format, args...)) },
				fatalf: func(format string, args ...any) { fatal = fmt.Sprintf(format, args...) },
			})

			assert.Equal(t, tc.attempts, attempts)
			assert.Equal(t, tc.sleeps, sleeps)
			assert.Equal(t, tc.fatal, fatal)
			assert.Equal(t, attempts > tc.failures, m.up)
			assert.Equal(t, m.up, m.closed)

			for i := range sleeps {
				assert.Equal(t, fmt.Sprintf("Migrate: postgres is trying to connect, attempts left: %d", 20-i), messages[i])
			}

			if tc.result != "" {
				require.Len(t, messages, sleeps+1)
				assert.Equal(t, tc.result, messages[sleeps])
			}
		})
	}
}

func TestOpenMigrationRejectsInvalidSource(t *testing.T) {
	t.Parallel()

	_, err := openMigration("invalid://source", "postgres://db")
	require.ErrorContains(t, err, "unknown driver")
}

func TestMigrateMissingURL(t *testing.T) {
	t.Parallel()

	if os.Getenv("TEST_MIGRATE_MISSING_URL") == "1" {
		Migrate()

		return
	}

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMigrateMissingURL$") //nolint:gosec // Re-execute this test binary to observe log.Fatalf's exit status.

	command.Env = append(os.Environ(), "TEST_MIGRATE_MISSING_URL=1", "PG_URL=")
	output, err := command.CombinedOutput()

	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 1, exit.ExitCode())
	assert.Contains(t, string(output), "migrate: environment variable not declared: PG_URL")
}
