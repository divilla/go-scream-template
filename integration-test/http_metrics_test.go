package integration_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPMetricsMethodCardinality(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()

	const methodCount = 100

	for i := range methodCount {
		resp, err := doWebRequestWithTimeout(ctx, "CUSTOM"+strconv.Itoa(i), healthPath, http.NoBody)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	}

	resp, err := doWebRequestWithTimeout(ctx, http.MethodGet, httpURL+"/metrics", http.NoBody)
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, string(data), `method="CUSTOM`)

	for _, metric := range []string{
		"echo_requests_total", "echo_request_duration_seconds_count",
		"echo_request_size_bytes_count", "echo_response_size_bytes_count",
	} {
		var series int

		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.HasPrefix(line, metric+"{") && strings.Contains(line, `method="UNKNOWN"`) {
				series++

				assert.Contains(t, line, `code="405"`)
				assert.True(t, strings.HasSuffix(line, " "+strconv.Itoa(methodCount)), line)
			}
		}

		assert.Equal(t, 1, series, metric)
	}
}
