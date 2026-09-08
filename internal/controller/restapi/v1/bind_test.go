package v1

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/pkg/httpserver"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBodyCompatibility(t *testing.T) {
	t.Parallel()

	for _, route := range routeCases() {
		if route.body == "" {
			continue
		}

		for _, mediaType := range []string{
			"application/x-www-form-urlencoded",
			"Application/X-Www-Form-Urlencoded; charset=UTF-8",
			"multipart/form-data",
			"Multipart/Form-Data",
			"application/xml",
			"Application/XML; charset=UTF-8",
			"text/xml",
			"Text/XML; charset=UTF-8",
			"application/vnd.example+json",
			"application/problem+json; charset=utf-8",
			"Application/JSON; charset=UTF-8",
		} {
			t.Run(route.method+route.path+mediaType, func(t *testing.T) {
				t.Parallel()

				stub := &usecaseStub{}
				app, token := testRouter(t, stub)
				body, contentType := compatibilityBody(t, route.body, mediaType)
				req := httptest.NewRequestWithContext(t.Context(), route.method, "/v1"+route.path+"?userID=attacker&email=query@example.com&title=query&bad=%zz", bytes.NewReader(body))
				req.Header.Set("Content-Type", contentType)
				req.Header.Set("Authorization", "Bearer "+token)

				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, req)
				require.Equal(t, route.status, rec.Code, rec.Body.String())
				assert.Equal(t, route.args, stub.args)
				assert.Contains(t, rec.Body.String(), route.contains)
				assert.Equal(t, t.Context(), stub.ctx)
				assert.Equal(t, contentType, req.Header.Get("Content-Type"))
			})
		}
	}
}

func compatibilityBody(t *testing.T, body, mediaType string) (payload []byte, contentType string) {
	t.Helper()

	var fields map[string]string
	require.NoError(t, json.Unmarshal([]byte(body), &fields))

	var buffer bytes.Buffer

	base, _, _ := strings.Cut(mediaType, ";")

	switch strings.ToLower(base) {
	case "application/xml", "text/xml":
		encoder := xml.NewEncoder(&buffer)
		root := xml.StartElement{Name: xml.Name{Local: "request"}}
		require.NoError(t, encoder.EncodeToken(root))

		for key, value := range fields {
			field := xml.StartElement{Name: xml.Name{Local: strings.ToUpper(key[:1]) + key[1:]}}
			require.NoError(t, encoder.EncodeElement(value, field))
		}

		require.NoError(t, encoder.EncodeToken(root.End()))
		require.NoError(t, encoder.Flush())

		return buffer.Bytes(), mediaType
	case "application/x-www-form-urlencoded":
		values := url.Values{}
		for key, value := range fields {
			values.Set(key, value)
		}

		return []byte(values.Encode()), mediaType
	case "multipart/form-data":
		writer := multipart.NewWriter(&buffer)
		require.NoError(t, writer.SetBoundary("CaseSensitiveBoundary"))

		for key, value := range fields {
			require.NoError(t, writer.WriteField(key, value))
		}

		require.NoError(t, writer.Close())

		return buffer.Bytes(), mediaType + `; boundary="` + writer.Boundary() + `"`
	default:
		return []byte(body), mediaType
	}
}

func TestInvalidCompatibleBodies(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ mediaType, body string }{
		{"application/x-www-form-urlencoded", "email=%zz"},
		{"Application/X-Www-Form-Urlencoded; charset=UTF-8", "email=%zz"},
		{"application/x-www-form-urlencoded", "password=secret123"},
		{"multipart/form-data; boundary=missing", "broken"},
		{"Multipart/Form-Data; boundary=MissingBoundary", "broken"},
		{"Application/XML; charset=UTF-8", "<request>"},
		{"Text/XML; charset=UTF-8", "<request>"},
		{"application/vnd.example+json", "{"},
		{"application/vnd.example+json", "{}"},
		{"text/plain", `{"email":"alice@example.com","password":"secret123"}`},
	} {
		t.Run(tc.mediaType+tc.body, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{}
			app, _ := testRouter(t, stub)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/login?email=query@example.com", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.mediaType)

			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Nil(t, stub.args)
			assert.Equal(t, tc.mediaType, req.Header.Get("Content-Type"))
		})
	}
}

func TestCompressedRoutes(t *testing.T) {
	t.Parallel()

	for _, route := range routeCases() {
		if route.body == "" {
			continue
		}

		t.Run(route.method+route.path, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{}
			server := httpserver.New(silentLogger{})

			t.Cleanup(func() { require.NoError(t, server.Shutdown()) })

			manager := jwt.New("test-secret", time.Hour)
			token, err := manager.GenerateToken("user-1")
			require.NoError(t, err)
			NewRoutes(server.App.Group("/v1"), stub, stub, stub, manager, silentLogger{})

			var body bytes.Buffer

			writer := gzip.NewWriter(&body)
			_, err = writer.Write([]byte(route.body))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			req := httptest.NewRequestWithContext(t.Context(), route.method, "/v1"+route.path, &body)
			req.Header.Set("Content-Type", "application/vnd.example+json")
			req.Header.Set("Content-Encoding", "gzip")
			req.Header.Set("Authorization", "Bearer "+token)

			rec := httptest.NewRecorder()
			server.App.ServeHTTP(rec, req)
			require.Equal(t, route.status, rec.Code, rec.Body.String())
			assert.Equal(t, route.args, stub.args)
		})
	}
}
