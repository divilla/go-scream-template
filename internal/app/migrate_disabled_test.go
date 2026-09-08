//go:build !migrate

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMigrateDisabled(t *testing.T) {
	t.Setenv("PG_URL", "")
	assert.NotPanics(t, Migrate)
}
