package integration_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	protov1 "github.com/divilla/go-scream-template/docs/proto/v1"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Signed fixture identities exercise real JWT validation and PostgreSQL errors
// without changing shared tables or injecting mocks into the service.
func fixtureToken(t *testing.T, subject any) string {
	t.Helper()

	secret := os.Getenv("JWT_SECRET")
	require.NotEmpty(t, secret)
	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, jwtlib.MapClaims{
		"sub": subject, "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(secret))
	require.NoError(t, err)

	return token
}

func TestHTTPDependencyErrors(t *testing.T) {
	missing := fixtureToken(t, uuid.NewString())
	invalid := fixtureToken(t, "invalid-uuid")
	contractHTTP(t, http.MethodGet, "/user/profile", missing, "", http.StatusNotFound)
	contractHTTP(t, http.MethodGet, "/user/profile", invalid, "", http.StatusInternalServerError)
	contractHTTP(t, http.MethodGet, "/tasks", invalid, "", http.StatusInternalServerError)
	contractHTTP(t, http.MethodGet, "/translation/history", invalid, "", http.StatusInternalServerError)
	contractHTTP(t, http.MethodPost, "/tasks", missing, `{"title":"missing owner"}`, http.StatusInternalServerError)
	contractHTTP(t, http.MethodPost, "/translation/do-translate", missing, `{"source":"auto","destination":"en","original":"текст для перевода"}`, http.StatusInternalServerError)
	contractHTTP(t, http.MethodGet, "/user/profile", fixtureToken(t, 123), "", http.StatusUnauthorized)

	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodNone, jwtlib.MapClaims{"sub": "invalid"}).SignedString(jwtlib.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	contractHTTP(t, http.MethodGet, "/user/profile", token, "", http.StatusUnauthorized)
}

func TestRPCDependencyErrors(t *testing.T) {
	contractClients(t, func(t *testing.T, client contractRPC) {
		t.Helper()

		for _, test := range []struct {
			operation string
			token     string
			data      any
		}{
			{"task.create", fixtureToken(t, uuid.NewString()), map[string]string{"title": "missing owner"}},
			{"task.list", fixtureToken(t, "invalid-uuid"), map[string]string{}},
			{"translation.getHistory", fixtureToken(t, "invalid-uuid"), nil},
			{"translation.translate", fixtureToken(t, uuid.NewString()), map[string]string{"source": "auto", "destination": "en", "original": "текст для перевода"}},
		} {
			var response any
			require.ErrorContains(t, client.RemoteCall("v1."+test.operation, authenticatedPayload(test.token, test.data), &response), "internal server error")
		}
	})
}

func TestGRPCDependencyErrors(t *testing.T) {
	conn, err := grpc.NewClient(grpcURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	auth := protov1.NewAuthServiceClient(conn)
	tasks := protov1.NewTaskServiceClient(conn)
	translations := protov1.NewTranslationClient(conn)
	missing := grpcAuthCtx(t, fixtureToken(t, uuid.NewString()))
	invalid := grpcAuthCtx(t, fixtureToken(t, "invalid-uuid"))
	_, err = auth.GetProfile(missing, &protov1.GetProfileRequest{})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = auth.GetProfile(invalid, &protov1.GetProfileRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
	_, err = tasks.CreateTask(missing, &protov1.CreateTaskRequest{Title: "missing owner"})
	require.Equal(t, codes.Internal, status.Code(err))
	_, err = tasks.ListTasks(invalid, &protov1.ListTasksRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
	_, err = translations.GetHistory(invalid, &protov1.GetHistoryRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
}
