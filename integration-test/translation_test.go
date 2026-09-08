package integration_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	protov1 "github.com/divilla/go-scream-template/docs/proto/v1"
	natsClient "github.com/divilla/go-scream-template/pkg/nats/nats_rpc/client"
	rmqClient "github.com/divilla/go-scream-template/pkg/rabbitmq/rmq_rpc/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// HTTP POST: /v1/translation/do-translate.
func TestHTTPDoTranslateV1(t *testing.T) {
	token := registerAndLogin(t)

	tests := []struct {
		description string
		body        string
		expected    int
	}{
		{
			description: "DoTranslate Success auto",
			body: `{
				"destination": "en",
				"original": "текст для перевода",
				"source": "auto"
			}`,
			expected: http.StatusOK,
		},
		{
			description: "DoTranslate Success ru",
			body: `{
				"destination": "en",
				"original": "Текст для перевода",
				"source": "ru"
			}`,
			expected: http.StatusOK,
		},
		{
			description: "DoTranslate Fail",
			body: `{
				"destination": "en",
				"original": "текст для перевода"
			}`,
			expected: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			url := basePathV1 + "/translation/do-translate"
			ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)

			defer cancel()

			resp, err := doAuthenticatedRequest(ctx, http.MethodPost, url, bytes.NewBufferString(tt.body), token)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}

			defer resp.Body.Close()

			if resp.StatusCode != tt.expected {
				t.Fatalf("Expected status %d, got %d", tt.expected, resp.StatusCode)
			}

			if tt.expected == http.StatusOK {
				body := parseJSON[struct {
					Translation string `json:"translation"`
				}](t, resp)
				if body.Translation != "text for translation" {
					t.Errorf("Unexpected translation: %q", body.Translation)
				}
			}
		})
	}
}

// HTTP GET: /v1/translation/history.
func TestHTTPHistoryV1(t *testing.T) {
	token := registerAndLogin(t)

	// First create a translation so history is non-empty.
	translateURL := basePathV1 + "/translation/do-translate"
	translateBody := `{
		"destination": "en",
		"original": "текст для перевода",
		"source": "auto"
	}`

	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()

	resp, err := doAuthenticatedRequest(ctx, http.MethodPost, translateURL, bytes.NewBufferString(translateBody), token)
	if err != nil {
		t.Fatalf("Failed to create translation: %v", err)
	}

	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status %d for translation, got %d", http.StatusOK, resp.StatusCode)
	}

	// Now fetch history.
	historyURL := basePathV1 + "/translation/history"

	ctx2, cancel2 := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel2()

	resp, err = doAuthenticatedRequest(ctx2, http.MethodGet, historyURL, http.NoBody, token)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	type historyBody struct {
		History []struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
			Original    string `json:"original"`
			Translation string `json:"translation"`
		} `json:"history"`
	}

	body := parseJSON[historyBody](t, resp)

	if len(body.History) != 1 {
		t.Fatalf("Expected one saved translation, got %d", len(body.History))
	}

	if body.History[0].Original != "текст для перевода" || body.History[0].Translation != "text for translation" {
		t.Errorf("Unexpected saved translation: %+v", body.History[0])
	}
}

// gRPC Client V1: GetHistory.
func TestGRPCTranslationV1(t *testing.T) {
	token := registerAndLoginGRPC(t)

	grpcConn, err := grpc.NewClient(grpcURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal("gRPC Client - init error - grpc.NewClient", err)
	}

	defer func() {
		if cerr := grpcConn.Close(); cerr != nil {
			t.Fatal("gRPC Client - shutdown error - grpcConn.Close", cerr)
		}
	}()

	translationClient := protov1.NewTranslationClient(grpcConn)

	for range requests {
		_, err = translationClient.GetHistory(grpcAuthCtx(t, token), &protov1.GetHistoryRequest{})
		if err != nil {
			t.Fatal("gRPC Client - remote call error - translationClient.GetHistory", err)
		}
	}
}

// RabbitMQ RPC Client V1: getHistory.
func TestRMQTranslationV1(t *testing.T) {
	token := registerAndLoginRMQ(t)

	client, err := rmqClient.New(rmqURL, rpcServerExchange, rpcClientExchange)
	if err != nil {
		t.Fatal("RabbitMQ RPC Client - init error - rmqClient.New", err)
	}

	defer func() {
		if serr := client.Shutdown(); serr != nil {
			t.Fatal("RabbitMQ RPC Client - shutdown error - client.Shutdown", serr)
		}
	}()

	type Translation struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Original    string `json:"original"`
		Translation string `json:"translation"`
	}

	type historyResponse struct {
		History []Translation `json:"history"`
	}

	for range requests {
		var history historyResponse

		err = client.RemoteCall("v1.translation.getHistory", authenticatedPayload(token, nil), &history)
		if err != nil {
			t.Fatal("RabbitMQ RPC Client - remote call error - client.RemoteCall", err)
		}
	}
}

// NATS RPC Client V1: getHistory.
func TestNATSTranslationV1(t *testing.T) {
	token := registerAndLoginNATS(t)

	client, err := natsClient.New(natsURL, rpcServerExchange)
	if err != nil {
		t.Fatal("NATS RPC Client - init error - natsClient.New", err)
	}

	defer func() {
		if serr := client.Shutdown(); serr != nil {
			t.Fatal("NATS RPC Client - shutdown error - client.Shutdown", serr)
		}
	}()

	type Translation struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Original    string `json:"original"`
		Translation string `json:"translation"`
	}

	type historyResponse struct {
		History []Translation `json:"history"`
	}

	for range requests {
		var history historyResponse

		err = client.RemoteCall("v1.translation.getHistory", authenticatedPayload(token, nil), &history)
		if err != nil {
			t.Fatal("NATS RPC Client - remote call error - client.RemoteCall", err)
		}
	}
}
