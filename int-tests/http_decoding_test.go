package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodedRequest(t *testing.T, method, path, format, token string, fields map[string]string, status int) map[string]any {
	t.Helper()
	body, contentType := encodedBody(t, format, fields)

	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, basePathV1+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)

	if format == "gzip" {
		req.Header.Set("Content-Encoding", "gzip")
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, err)
	require.Equal(t, status, resp.StatusCode, string(data))

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))

	return result
}

func encodedBody(t *testing.T, format string, fields map[string]string) (payload []byte, contentType string) {
	t.Helper()

	var buffer bytes.Buffer

	switch format {
	case "xml", "text-xml":
		return xmlBody(t, format, fields)
	case "form", "mixed-form":
		values := url.Values{}
		for key, value := range fields {
			values.Set(key, value)
		}

		contentType := "application/x-www-form-urlencoded"
		if format == "mixed-form" {
			contentType = "Application/X-Www-Form-Urlencoded; charset=UTF-8"
		}

		return []byte(values.Encode()), contentType
	case "multipart", "mixed-multipart":
		writer := multipart.NewWriter(&buffer)
		require.NoError(t, writer.SetBoundary("CaseSensitiveBoundary"))

		for key, value := range fields {
			require.NoError(t, writer.WriteField(key, value))
		}

		require.NoError(t, writer.Close())

		contentType := writer.FormDataContentType()
		if format == "mixed-multipart" {
			contentType = `Multipart/Form-Data; boundary="` + writer.Boundary() + `"`
		}

		return buffer.Bytes(), contentType
	default:
		body, err := json.Marshal(fields)
		require.NoError(t, err)

		if format == "gzip" {
			writer := gzip.NewWriter(&buffer)
			_, err = writer.Write(body)
			require.NoError(t, err)
			require.NoError(t, writer.Close())

			body = buffer.Bytes()
		}

		return body, "application/vnd.example+json; charset=utf-8"
	}
}

func xmlBody(t *testing.T, format string, fields map[string]string) (payload []byte, contentType string) {
	t.Helper()

	var buffer bytes.Buffer

	encoder := xml.NewEncoder(&buffer)
	root := xml.StartElement{Name: xml.Name{Local: "request"}}
	require.NoError(t, encoder.EncodeToken(root))

	for key, value := range fields {
		field := xml.StartElement{Name: xml.Name{Local: strings.ToUpper(key[:1]) + key[1:]}}
		require.NoError(t, encoder.EncodeElement(value, field))
	}

	require.NoError(t, encoder.EncodeToken(root.End()))
	require.NoError(t, encoder.Flush())

	contentType = "Application/XML; charset=UTF-8"
	if format == "text-xml" {
		contentType = "Text/XML; charset=UTF-8"
	}

	return buffer.Bytes(), contentType
}

func TestHTTPRequestDecoding(t *testing.T) {
	for _, format := range []string{"form", "mixed-form", "multipart", "mixed-multipart", "xml", "text-xml", "vendor", "gzip"} {
		t.Run(format, func(t *testing.T) {
			name := uniqueUsername(t)
			fields := map[string]string{"username": name, "email": name + "@test.com", "password": testPassword}
			user := encodedRequest(t, http.MethodPost, "/auth/register", format, "", fields, http.StatusCreated)
			assert.Equal(t, name, user["username"])
			login := encodedRequest(t, http.MethodPost, "/auth/login", format, "", fields, http.StatusOK)
			token, ok := login["token"].(string)
			require.True(t, ok)
			require.NotEmpty(t, token)
			task := encodedRequest(t, http.MethodPost, "/tasks", format, token, map[string]string{"title": "task", "description": "details", "userID": "attacker"}, http.StatusCreated)
			assert.Equal(t, user["id"], task["user_id"])
			path := fmt.Sprintf("/tasks/%v", task["id"])
			updated := encodedRequest(t, http.MethodPut, path, format, token, map[string]string{"title": "edited", "description": "new"}, http.StatusOK)
			assert.Equal(t, "edited", updated["title"])
			transitioned := encodedRequest(t, http.MethodPatch, path+"/status", format, token, map[string]string{"status": "in_progress"}, http.StatusOK)
			assert.Equal(t, "in_progress", transitioned["status"])
			translated := encodedRequest(t, http.MethodPost, "/translation/do-translate", format, token, map[string]string{"source": "auto", "destination": "en", "original": "текст для перевода"}, http.StatusOK)
			assert.Equal(t, "текст для перевода", translated["original"])
		})
	}
}

func TestHTTPCompressedBodyLimit(t *testing.T) {
	for _, path := range []string{"/auth/register", "/auth/login"} {
		for _, chunked := range []bool{false, true} {
			body, contentType := encodedBody(t, "gzip", map[string]string{"padding": strings.Repeat("x", 4<<20)})
			ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
			t.Cleanup(cancel)

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, basePathV1+path, bytes.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Content-Encoding", "gzip")

			if chunked {
				req.ContentLength = -1
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
		}
	}
}
