package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func loadExample(t *testing.T) {
	t.Helper()

	data, err := os.ReadFile("../.env.example")
	require.NoError(t, err)

	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			t.Setenv(key, value)
		}
	}
}

func TestNewConfig(t *testing.T) {
	loadExample(t)

	cfg, err := NewConfig()
	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.HTTP.Port)
	assert.Equal(t, "postgres://postgres:postgres@localhost:15432/db", cfg.PG.URL)
	assert.Equal(t, 24*time.Hour, cfg.JWT.TokenExpiry)
	t.Setenv("PG_POOL_MAX", "invalid")

	cfg, err = NewConfig()
	require.ErrorContains(t, err, "config error")
	assert.Nil(t, cfg)
}

// TestPostgresConfiguration checks host and container settings together (AC 6).
func TestPostgresConfiguration(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../docker-compose.yml")
	require.NoError(t, err)

	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
			Ports       []string          `yaml:"ports"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(data, &compose))
	db := compose.Services["db"]
	assert.Equal(t, []string{"15432:5432"}, db.Ports)
	assert.Equal(t, "postgres", db.Environment["POSTGRES_USER"])
	assert.Equal(t, "postgres", db.Environment["POSTGRES_PASSWORD"])
	assert.Equal(t, "5432", db.Environment["POSTGRES_PORT"])
	assert.Equal(t, "postgres://postgres:postgres@db:5432/db", compose.Services["app"].Environment["PG_URL"])

	for _, name := range []string{".env.example", "README.md"} {
		content, readErr := os.ReadFile(filepath.Join("..", name))
		require.NoError(t, readErr)
		assert.Contains(t, string(content), "postgres://postgres:postgres@localhost:15432/db")
	}
}

// TestFrameworkCleanup guards the dependency and configuration cleanup (AC 5).
func TestFrameworkCleanup(t *testing.T) {
	t.Parallel()

	root, err := os.OpenRoot("..")
	require.NoError(t, err)

	defer root.Close()

	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "agent" {
				return fs.SkipDir
			}

			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}

		checkProjectFile(t, root, path, entry.Name())

		return nil
	})
	require.NoError(t, err)
}

func checkProjectFile(t *testing.T, root *os.Root, path, name string) {
	t.Helper()

	content, err := root.ReadFile(path)
	require.NoError(t, err)

	for _, removed := range []string{"fi" + "ber", "pre" + "fork"} {
		assert.NotContains(t, strings.ToLower(name), removed, path)
		assert.NotContains(t, strings.ToLower(string(content)), removed, path)
	}
}
