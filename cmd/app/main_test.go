package main

import (
	"io"
	"testing"

	"github.com/divilla/go-scream-template/config"
	"github.com/stretchr/testify/assert"
)

//nolint:paralleltest // Tests replace the process entrypoint dependencies.
func TestMain(t *testing.T) {
	original := entrypoint

	t.Cleanup(func() { entrypoint = original })

	cfg := &config.Config{}
	migrated := false
	entrypoint.migrate = func() { migrated = true }
	ran, fatal := false, false
	entrypoint.config = func() (*config.Config, error) {
		assert.True(t, migrated, "migrations must precede configuration")

		return cfg, nil
	}
	entrypoint.run = func(got *config.Config) { assert.Same(t, cfg, got); ran = true }
	entrypoint.fatal = func(message string, args ...any) {
		assert.Equal(t, "Config error: %s", message)
		assert.Equal(t, []any{io.ErrUnexpectedEOF}, args)

		fatal = true
	}

	main()
	assert.True(t, ran)
	assert.False(t, fatal)

	ran = false
	migrated = false
	entrypoint.config = func() (*config.Config, error) {
		assert.True(t, migrated, "migrations must also precede invalid configuration")

		return nil, io.ErrUnexpectedEOF
	}

	main()
	assert.True(t, fatal)
	assert.False(t, ran)
}
