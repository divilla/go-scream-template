package integration_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// The integration network resolves translate.google.com to this container.
// The real translation client still performs its token and translation requests.
func startTranslationFixture() (*httptest.Server, error) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", ":443")
	if err != nil {
		return nil, fmt.Errorf("translation fixture listener: %w", err)
	}

	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(translationFixture), ReadHeaderTimeout: time.Second}}
	server.StartTLS()

	return server, nil
}

func translationFixture(w http.ResponseWriter, r *http.Request) {
	body := "tkk:'1.2'"

	if r.URL.Path != "/" {
		query := r.URL.Query()
		if r.URL.Path != "/translate_a/single" || query.Get("tl") != "en" || (query.Get("sl") != "auto" && query.Get("sl") != "ru") || strings.ToLower(query.Get("q")) != "текст для перевода" {
			http.Error(w, "unexpected translation fixture request", http.StatusBadRequest)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		body = `{"sentences":[{"trans":"text for translation"}],"src":"ru"}`
	}

	if _, err := io.WriteString(w, body); err != nil {
		log.Printf("translation fixture response: %v", err)
	}
}
