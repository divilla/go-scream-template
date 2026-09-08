package integration_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPBodyLimit(t *testing.T) {
	for _, path := range []string{"/auth/register", "/auth/login"} {
		for _, payload := range []struct{ contentType, body string }{
			{"application/json", `{"username":"alice","email":"alice@example.com","password":"secret123","padding":"` + strings.Repeat("x", 5<<20) + `"}`},
			{"application/xml", `<user><Username>alice</Username><Email>alice@example.com</Email><Password>secret123</Password></user><!--` + strings.Repeat("x", 5<<20) + `-->`},
		} {
			for _, chunked := range []bool{false, true} {
				ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
				t.Cleanup(cancel)

				req, err := http.NewRequestWithContext(ctx, http.MethodPost, basePathV1+path, strings.NewReader(payload.body))
				require.NoError(t, err)
				req.Header.Set("Content-Type", payload.contentType)

				if chunked {
					req.ContentLength = -1
				}

				resp, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				data, err := io.ReadAll(resp.Body)
				require.NoError(t, resp.Body.Close())
				require.NoError(t, err)
				assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode, "%s %s chunked=%t", path, payload.contentType, chunked)
				assert.JSONEq(t, `{"message":"Request Entity Too Large"}`, string(data))
			}
		}
	}
}

func TestHTTPSwaggerRoots(t *testing.T) {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, path := range []string{"/swagger", "/swagger/"} {
		ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
		t.Cleanup(cancel)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL+path, http.NoBody)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
		assert.Equal(t, "/swagger/index.html", resp.Header.Get("Location"))
	}
}

func TestHTTPHEAD(t *testing.T) {
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/healthz", http.StatusOK},
		{"/metrics", http.StatusOK},
		{"/swagger/index.html", http.StatusOK},
		{"/v1/user/profile", http.StatusUnauthorized},
		{"/v1/tasks/", http.StatusUnauthorized},
		{"/v1/tasks/task-1", http.StatusUnauthorized},
		{"/v1/translation/history", http.StatusUnauthorized},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
		t.Cleanup(cancel)

		req, err := http.NewRequestWithContext(ctx, http.MethodHead, httpURL+tc.path, http.NoBody)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, resp.Body.Close())
		require.NoError(t, err)
		assert.Equal(t, tc.status, resp.StatusCode, tc.path)
		assert.Empty(t, data, tc.path)
	}
}
