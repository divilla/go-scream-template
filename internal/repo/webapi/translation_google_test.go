package webapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	translator "github.com/Conight/go-googletrans"
	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslation(t *testing.T) {
	t.Parallel()

	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "upstream-error"}[failure], func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					_, writeErr := w.Write([]byte("tkk:'1.2'"))
					assert.NoError(t, writeErr)

					return
				}

				assert.Equal(t, "/translate_a/single", r.URL.Path)
				assert.Equal(t, "hello", r.URL.Query().Get("q"))
				assert.Equal(t, "en", r.URL.Query().Get("sl"))
				assert.Equal(t, "de", r.URL.Query().Get("tl"))

				if failure {
					w.WriteHeader(http.StatusServiceUnavailable)

					return
				}

				_, writeErr := w.Write([]byte(`{"sentences":[{"trans":"hallo"}],"src":"en"}`))
				assert.NoError(t, writeErr)
			}))
			defer server.Close()

			client := newTraced(&TranslationWebAPI{conf: translator.Config{ServiceUrls: []string{strings.TrimPrefix(server.URL, "https://")}}})
			input := entity.Translation{Source: "en", Destination: "de", Original: "hello"}

			got, err := client.Translate(t.Context(), input)
			if failure {
				require.ErrorContains(t, err, "trans.Translate")
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)

			input.Translation = "hallo"
			assert.Equal(t, input, got)
		})
	}
}

func TestCanceledTranslation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := New().Translate(ctx, entity.Translation{})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got)
}
