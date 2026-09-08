package middleware_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/internal/controller/restapi/middleware"
	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObservability verifies structured request logging and panic stacks (AC 4).
func TestObservability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		handler        echo.HandlerFunc
		code           int
		body, errorLog string
	}{
		{"success", func(c *echo.Context) error { return c.String(http.StatusCreated, "created") }, http.StatusCreated, "created", ""},
		{"error", func(*echo.Context) error { return io.ErrUnexpectedEOF }, http.StatusInternalServerError, "Internal Server Error", "unexpected EOF"},
		{"panic", func(*echo.Context) error { panic("private panic") }, http.StatusInternalServerError, "Internal Server Error", "private panic"},
		{"http error", func(*echo.Context) error { return echo.ErrNotFound }, http.StatusNotFound, "Not Found", "Not Found"},
		{"method not allowed", func(*echo.Context) error { return echo.ErrMethodNotAllowed }, http.StatusMethodNotAllowed, "Method Not Allowed", "Method Not Allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, requestID := range []string{"", "client-request-123"} {
				var buf bytes.Buffer

				log := zerolog.New(&buf).With().Timestamp().Logger()
				app := echo.New()
				app.Use(echomiddleware.RequestID(), middleware.Logger(&log), echomiddleware.RecoverWithConfig(echomiddleware.RecoverConfig{DisableStackAll: true}))
				app.GET("/test", tc.handler)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test?query=value", nil)
				req.Header.Set(echo.HeaderXRequestID, requestID)

				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, req)
				assert.Equal(t, tc.code, rec.Code)
				assert.Contains(t, rec.Body.String(), tc.body)
				assert.NotContains(t, rec.Body.String(), "private")
				require.NotEmpty(t, rec.Header().Get(echo.HeaderXRequestID))

				if requestID != "" {
					assert.Equal(t, requestID, rec.Header().Get(echo.HeaderXRequestID))
				}

				entry := assertRequestLog(t, buf.Bytes(), rec)
				assertRequestError(t, entry, tc.errorLog, tc.name == "panic")
			}
		})
	}
}

func assertRequestLog(t *testing.T, data []byte, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var entry map[string]any
	// Unmarshal also rejects duplicate log entries for the same request.
	require.NoError(t, json.Unmarshal(data, &entry))
	assert.Equal(t, "restapi request", entry["message"])
	assert.Equal(t, "192.0.2.1", entry["remote_ip"])
	assert.Equal(t, "GET", entry["method"])
	assert.Equal(t, "/test?query=value", entry["uri"])
	assert.Equal(t, float64(rec.Code), entry["status"])
	assert.Equal(t, float64(rec.Body.Len()), entry["response_size"])
	assert.Equal(t, rec.Header().Get(echo.HeaderXRequestID), entry["request_id"])
	assert.Contains(t, entry, "latency")
	timestamp, ok := entry["time"].(string)
	require.True(t, ok)

	_, err := time.Parse(time.RFC3339, timestamp)
	require.NoError(t, err)
	assert.NotContains(t, entry, "caller")

	return entry
}

func assertRequestError(t *testing.T, entry map[string]any, errorLog string, panicked bool) {
	t.Helper()

	if errorLog == "" {
		assert.Equal(t, "info", entry["level"])
		assert.NotContains(t, entry, "error")
	} else {
		assert.Equal(t, "error", entry["level"])
		assert.Equal(t, errorLog, entry["error"])
	}

	if panicked {
		assert.Contains(t, entry["stack"], "goroutine")
		assert.Contains(t, entry["stack"], "TestObservability")
	} else {
		assert.NotContains(t, entry, "stack")
	}
}
