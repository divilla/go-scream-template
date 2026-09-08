package integration_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	protov1 "github.com/divilla/go-scream-template/docs/proto/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func contractHTTP(t *testing.T, method, path, token, body string, want int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()

	response, err := doAuthenticatedRequest(ctx, method, basePathV1+path, strings.NewReader(body), token)
	require.NoError(t, err)

	defer response.Body.Close()

	require.Equal(t, want, response.StatusCode)

	if want >= 400 {
		payload := parseJSON[map[string]any](t, response)
		require.NotEmpty(t, payload)
		require.NotContains(t, payload, "password_hash")
	}
}

func TestHTTPTaskFailureContracts(t *testing.T) {
	token := registerAndLogin(t)
	owned := httpCreateTask(t, token, "owned", "")

	other := registerAndLogin(t)
	for _, operation := range []struct{ method, suffix, body string }{
		{http.MethodGet, "", ""},
		{http.MethodPut, "", `{"title":"changed"}`},
		{http.MethodPatch, "/status", `{"status":"in_progress"}`},
		{http.MethodDelete, "", ""},
	} {
		t.Run(operation.method, func(t *testing.T) {
			forbidden := http.StatusForbidden
			if operation.method == http.MethodDelete {
				forbidden = http.StatusNotFound
			}

			for _, test := range []struct {
				token, id string
				want      int
			}{
				{token, uuid.NewString(), http.StatusNotFound},
				{other, owned.ID, forbidden},
				{token, "invalid-uuid", http.StatusInternalServerError},
			} {
				contractHTTP(t, operation.method, "/tasks/"+test.id+operation.suffix, test.token, operation.body, test.want)
			}
		})
	}

	for _, operation := range []struct{ method, path string }{
		{http.MethodPost, "/tasks"},
		{http.MethodPut, "/tasks/" + owned.ID},
		{http.MethodPatch, "/tasks/" + owned.ID + "/status"},
		{http.MethodPost, "/auth/register"},
		{http.MethodPost, "/auth/login"},
		{http.MethodPost, "/translation/do-translate"},
	} {
		for _, body := range []string{"{", `{}`} {
			contractHTTP(t, operation.method, operation.path, token, body, http.StatusBadRequest)
		}
	}

	contractHTTP(t, http.MethodGet, "/tasks?status=unknown", token, "", http.StatusBadRequest)
	contractHTTP(t, http.MethodGet, "/tasks?status=todo&limit=0&offset=-1", token, "", http.StatusOK)
	contractHTTP(t, http.MethodGet, "/user/profile", "invalid", "", http.StatusUnauthorized)
	contractHTTP(t, http.MethodPost, "/translation/do-translate", token, `{"source":"auto","destination":"en","original":"fixture failure"}`, http.StatusInternalServerError)
}

func TestGRPCTaskFailureContracts(t *testing.T) {
	conn, err := grpc.NewClient(grpcURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	client := protov1.NewTaskServiceClient(conn)
	token := registerAndLogin(t)
	owned := httpCreateTask(t, token, "owned", "")

	other := registerAndLogin(t)
	for _, operation := range []struct {
		name string
		call func(context.Context, string) error
	}{
		{"get", func(ctx context.Context, id string) error {
			_, err := client.GetTask(ctx, &protov1.GetTaskRequest{Id: id})

			return err
		}},
		{"update", func(ctx context.Context, id string) error {
			_, err := client.UpdateTask(ctx, &protov1.UpdateTaskRequest{Id: id, Title: "changed"})

			return err
		}},
		{"transition", func(ctx context.Context, id string) error {
			_, err := client.TransitionTask(ctx, &protov1.TransitionTaskRequest{Id: id, Status: "in_progress"})

			return err
		}},
		{"delete", func(ctx context.Context, id string) error {
			_, err := client.DeleteTask(ctx, &protov1.DeleteTaskRequest{Id: id})

			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			forbidden := codes.PermissionDenied
			if operation.name == "delete" {
				forbidden = codes.NotFound
			}

			for _, test := range []struct {
				token, id string
				want      codes.Code
			}{
				{token, uuid.NewString(), codes.NotFound}, {other, owned.ID, forbidden}, {token, "invalid-uuid", codes.Internal},
			} {
				require.Equal(t, test.want, status.Code(operation.call(grpcAuthCtx(t, test.token), test.id)))
			}
		})
	}

	_, err = client.TransitionTask(grpcAuthCtx(t, token), &protov1.TransitionTaskRequest{Id: owned.ID, Status: "done"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = client.ListTasks(grpcAuthCtx(t, token), &protov1.ListTasksRequest{Status: "invalid"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	list, err := client.ListTasks(grpcAuthCtx(t, token), &protov1.ListTasksRequest{Status: "todo", Offset: -1})
	require.NoError(t, err)
	require.Len(t, list.GetTasks(), 1)

	for _, ctx := range []context.Context{metadata.NewOutgoingContext(t.Context(), metadata.MD{}), grpcAuthCtx(t, ""), grpcAuthCtx(t, "invalid")} {
		_, err = client.ListTasks(ctx, &protov1.ListTasksRequest{})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	}
}

func TestGRPCTranslationAndAuthErrors(t *testing.T) {
	conn, err := grpc.NewClient(grpcURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	token := registerAndLogin(t)
	client := protov1.NewTranslationClient(conn)
	translated, err := client.Translate(grpcAuthCtx(t, token), &protov1.TranslateRequest{Source: "auto", Destination: "en", Original: "текст для перевода"})
	require.NoError(t, err)
	require.Equal(t, "text for translation", translated.GetTranslation())
	history, err := client.GetHistory(grpcAuthCtx(t, token), &protov1.GetHistoryRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, history.GetHistory())
	_, err = client.Translate(grpcAuthCtx(t, token), &protov1.TranslateRequest{Source: "auto", Destination: "en", Original: "fixture failure"})
	require.Equal(t, codes.Internal, status.Code(err))

	auth := protov1.NewAuthServiceClient(conn)
	name := uniqueUsername(t)
	registration := &protov1.RegisterRequest{Username: name, Email: name + "@test.com", Password: testPassword}
	_, err = auth.Register(t.Context(), registration)
	require.NoError(t, err)
	_, err = auth.Register(t.Context(), registration)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	_, err = auth.Login(t.Context(), &protov1.LoginRequest{Email: registration.Email, Password: "wrongpassword"})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	_, err = auth.Register(t.Context(), &protov1.RegisterRequest{Username: uniqueUsername(t), Password: strings.Repeat("x", 73)})
	require.Equal(t, codes.Internal, status.Code(err))
}
