package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPRequestID(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()

	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/missing", http.StatusNotFound},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/user/profile", http.StatusUnauthorized},
	} {
		for _, id := range []string{"", "integration-request-123"} {
			req, err := http.NewRequestWithContext(ctx, tc.method, httpURL+tc.path, http.NoBody)
			require.NoError(t, err)
			req.Header.Set(echo.HeaderXRequestID, id)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, tc.status, resp.StatusCode)
			received := resp.Header.Get(echo.HeaderXRequestID)
			require.NotEmpty(t, received)

			if id != "" {
				assert.Equal(t, id, received)
			}
		}
	}
}
