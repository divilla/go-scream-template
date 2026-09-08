package httpserver

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compressBody(t *testing.T, body []byte, encoding string) []byte {
	t.Helper()

	var buffer bytes.Buffer

	var writer io.WriteCloser

	switch encoding {
	case "gzip":
		writer = gzip.NewWriter(&buffer)
	case "deflate":
		writer = zlib.NewWriter(&buffer)
	case "br", "brotli":
		writer = brotli.NewWriter(&buffer)
	default:
		t.Fatalf("unsupported test encoding %q", encoding)
	}

	_, err := writer.Write(body)
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	return buffer.Bytes()
}

func TestCompressedBodyLimit(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{"gzip", "deflate", "br", "brotli"} {
		for _, size := range []int{0, _defaultBodyLimit, _defaultBodyLimit + 1} {
			t.Run(fmt.Sprintf("%s/%d", encoding, size), func(t *testing.T) {
				t.Parallel()

				body := bytes.Repeat([]byte("x"), size)
				compressed := compressBody(t, body, encoding)

				for _, chunked := range []bool{false, true} {
					checkCompressedBody(t, body, compressed, encoding, chunked)
				}
			})
		}
	}
}

func TestCompressedBodyErrors(t *testing.T) {
	t.Parallel()

	for _, encoding := range []string{"gzip", "deflate", "br"} {
		valid := compressBody(t, []byte(`{"data":"value"}`), encoding)
		for _, body := range [][]byte{[]byte("invalid"), valid[:len(valid)-1], bytes.Repeat([]byte("x"), _defaultBodyLimit+1)} {
			for _, chunked := range []bool{false, true} {
				s := New(silentLogger{})

				t.Cleanup(func() { require.NoError(t, s.Shutdown()) })
				s.App.POST("/", func(*echo.Context) error {
					t.Error("handler called for invalid compressed body")

					return nil
				})

				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(body))
				req.Header.Set("Content-Encoding", encoding)

				if chunked {
					req.ContentLength = -1
				}

				rec := httptest.NewRecorder()
				s.App.ServeHTTP(rec, req)

				status := http.StatusBadRequest
				if len(body) > _defaultBodyLimit {
					status = http.StatusRequestEntityTooLarge
				}

				assert.Equal(t, status, rec.Code, "%s chunked=%t", encoding, chunked)
			}
		}
	}
}

func TestStackedContentEncodings(t *testing.T) {
	t.Parallel()

	body := []byte(`{"data":"value"}`)
	s := New(silentLogger{})

	t.Cleanup(func() { require.NoError(t, s.Shutdown()) })

	s.App.POST("/", func(c *echo.Context) error {
		decoded, err := io.ReadAll(c.Request().Body)
		require.NoError(t, err)
		assert.Equal(t, body, decoded)

		return c.NoContent(http.StatusNoContent)
	})

	for _, encoding := range []string{"gzip, deflate", "identity", "unknown"} {
		payload := body
		if strings.Contains(encoding, "gzip") {
			payload = compressBody(t, compressBody(t, body, "deflate"), "gzip")
		}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(payload))
		req.Header.Set("Content-Encoding", encoding)

		rec := httptest.NewRecorder()
		s.App.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	}
}

func checkCompressedBody(t *testing.T, body, compressed []byte, encoding string, chunked bool) {
	t.Helper()

	s := New(silentLogger{})

	t.Cleanup(func() { require.NoError(t, s.Shutdown()) })

	called := false

	s.App.POST("/", func(c *echo.Context) error {
		called = true
		decoded, err := io.ReadAll(c.Request().Body)
		require.NoError(t, err)
		assert.Equal(t, body, decoded)
		assert.EqualValues(t, len(body), c.Request().ContentLength)

		return c.NoContent(http.StatusNoContent)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", encoding)

	if chunked {
		req.ContentLength = -1
	}

	rec := httptest.NewRecorder()
	s.App.ServeHTTP(rec, req)

	if len(body) > _defaultBodyLimit {
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		assert.False(t, called)
	} else {
		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.True(t, called)
	}
}
