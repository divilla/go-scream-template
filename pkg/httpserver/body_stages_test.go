package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each intermediate decoding stage must enforce the documented 4 MiB limit.
func TestStackedContentEncodingIntermediateLimit(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{"gzip", "deflate", "br"} {
		t.Run(encoding, func(t *testing.T) {
			t.Parallel()

			// A valid, small inner document with trailing padding creates an
			// oversized intermediate body while keeping the wire body small.
			inner := compressBody(t, []byte(`{"data":"value"}`), "deflate")
			intermediate := make([]byte, _defaultBodyLimit+1)
			copy(intermediate, inner)
			payload := compressBody(t, intermediate, encoding)
			require.Less(t, len(payload), _defaultBodyLimit)

			s := New(silentLogger{})

			t.Cleanup(func() { require.NoError(t, s.Shutdown()) })

			s.App.POST("/", func(*echo.Context) error {
				t.Error("handler called for an oversized intermediate body")

				return nil
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(payload))
			req.Header.Set(echo.HeaderContentEncoding, encoding+", deflate")

			rec := httptest.NewRecorder()

			s.App.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
			assert.JSONEq(t, `{"message":"Request Entity Too Large"}`, rec.Body.String())
		})
	}
}
