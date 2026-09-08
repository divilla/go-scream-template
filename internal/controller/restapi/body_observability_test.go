package restapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/divilla/go-scream-template/config"
	"github.com/divilla/go-scream-template/internal/controller/restapi"
	"github.com/divilla/go-scream-template/pkg/httpserver"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestLogger struct {
	silentLogger
	requests bytes.Buffer
}

func (l *requestLogger) Zerolog() *zerolog.Logger {
	return new(zerolog.New(&l.requests).With().Timestamp().Logger())
}

// Rejected bodies must remain observable before reaching a use case (AC 4).
func TestRejectedBodyObservability(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, encoding string
		body           func() io.Reader
		status         int
	}{
		{"malformed gzip", "gzip", func() io.Reader { return strings.NewReader("invalid gzip") }, http.StatusBadRequest},
		{"oversized body", "", func() io.Reader { return strings.NewReader(strings.Repeat("x", (4<<20)+1)) }, http.StatusRequestEntityTooLarge},
		{"read failure", "", func() io.Reader { return iotest.ErrReader(io.ErrUnexpectedEOF) }, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			log := &requestLogger{}
			server := httpserver.New(log)

			t.Cleanup(func() { require.NoError(t, server.Shutdown()) })

			cfg := &config.Config{}
			cfg.Metrics.Enabled = true
			cfg.App.Name = "test-service"
			restapi.NewRouter(server.App, cfg, nil, nil, nil, jwt.New("test-secret", time.Hour), log)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/login", tc.body())
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", tc.encoding)

			rec := httptest.NewRecorder()
			server.App.ServeHTTP(rec, req)

			require.Equal(t, tc.status, rec.Code)

			var entry map[string]any
			require.NoError(t, json.Unmarshal(log.requests.Bytes(), &entry))
			assert.Equal(t, "POST", entry["method"])
			assert.Equal(t, "/v1/auth/login", entry["uri"])
			assert.Equal(t, float64(tc.status), entry["status"])
			assert.Equal(t, float64(rec.Body.Len()), entry["response_size"])
			require.NotEmpty(t, rec.Header().Get(echo.HeaderXRequestID))
			assert.Equal(t, rec.Header().Get(echo.HeaderXRequestID), entry["request_id"])
			metrics := get(t, server.App, "/metrics")
			require.Equal(t, http.StatusOK, metrics.Code)

			for _, metric := range []string{
				"echo_requests_total", "echo_request_duration_seconds_count",
				"echo_request_size_bytes_count", "echo_response_size_bytes_count",
			} {
				assert.Contains(t, metrics.Body.String(), fmt.Sprintf(
					`%s{code="%d",host="test-service",method="POST",url="/v1/auth/login"} 1`+"\n", metric, tc.status,
				))
			}
		})
	}
}
