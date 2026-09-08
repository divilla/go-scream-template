package v1

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/goccy/go-json"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type usecaseStub struct {
	err  error
	ctx  context.Context
	args []any
}

func (s *usecaseStub) record(ctx context.Context, args ...any) { s.ctx, s.args = ctx, args }
func (s *usecaseStub) Register(ctx context.Context, username, email, password string) (entity.User, error) {
	s.record(ctx, username, email, password)

	return entity.User{ID: "user-1", Username: username, Email: email}, s.err
}

func (s *usecaseStub) Login(ctx context.Context, email, password string) (string, error) {
	s.record(ctx, email, password)

	return "signed-token", s.err
}

func (s *usecaseStub) GetUser(ctx context.Context, userID string) (entity.User, error) {
	s.record(ctx, userID)

	return entity.User{ID: userID}, s.err
}

func (s *usecaseStub) Create(ctx context.Context, userID, title, description string) (entity.Task, error) {
	s.record(ctx, userID, title, description)

	return entity.Task{ID: "task-1", Title: title, Description: description}, s.err
}

func (s *usecaseStub) Get(ctx context.Context, userID, taskID string) (entity.Task, error) {
	s.record(ctx, userID, taskID)

	return entity.Task{ID: taskID}, s.err
}

func (s *usecaseStub) List(ctx context.Context, userID string, status *entity.TaskStatus, limit, offset int) ([]entity.Task, int, error) {
	s.record(ctx, userID, status, limit, offset)

	return []entity.Task{{ID: "task-1"}}, 1, s.err
}

func (s *usecaseStub) Update(ctx context.Context, userID, taskID, title, description string) (entity.Task, error) {
	s.record(ctx, userID, taskID, title, description)

	return entity.Task{ID: taskID, Title: title, Description: description}, s.err
}

func (s *usecaseStub) Transition(ctx context.Context, userID, taskID string, status entity.TaskStatus) (entity.Task, error) {
	s.record(ctx, userID, taskID, status)

	return entity.Task{ID: taskID, Status: status}, s.err
}

func (s *usecaseStub) Delete(ctx context.Context, userID, taskID string) error {
	s.record(ctx, userID, taskID)

	return s.err
}

func (s *usecaseStub) History(ctx context.Context, userID string) (entity.TranslationHistory, error) {
	s.record(ctx, userID)

	return entity.TranslationHistory{}, s.err
}

func (s *usecaseStub) Translate(ctx context.Context, userID string, translation entity.Translation) (entity.Translation, error) {
	s.record(ctx, userID, translation)

	return translation, s.err
}

type silentLogger struct{ logger.Interface }

func (silentLogger) Error(any, ...any) {}

type routeCase struct {
	name, body, result string
	args               []any
}

func routeCases() []routeCase {
	status := entity.TaskStatusDone

	return []routeCase{
		{"auth.register", `{"username":"alice","email":"alice@example.com","password":"secret123"}`, `"username":"alice"`, []any{"alice", "alice@example.com", "secret123"}},
		{"auth.login", `{"email":"alice@example.com","password":"secret123"}`, `"token":"signed-token"`, []any{"alice@example.com", "secret123"}},
		{"task.create", `{"title":"task","description":"details","user_id":"attacker"}`, `"title":"task"`, []any{"user-1", "task", "details"}},
		{"task.get", `{"id":"task-1"}`, `"id":"task-1"`, []any{"user-1", "task-1"}},
		{"task.list", `{"status":"done","limit":3,"offset":2}`, `"total":1`, []any{"user-1", &status, 3, 2}},
		{"task.update", `{"id":"task-1","title":"edited","description":"new"}`, `"title":"edited"`, []any{"user-1", "task-1", "edited", "new"}},
		{"task.transition", `{"id":"task-1","status":"in_progress"}`, `"status":"in_progress"`, []any{"user-1", "task-1", entity.TaskStatusInProgress}},
		{"task.delete", `{"id":"task-1"}`, `"status":"deleted"`, []any{"user-1", "task-1"}},
		{"translation.getHistory", `{}`, `"history":`, []any{"user-1"}},
		{"translation.translate", `{"source":"en","destination":"de","original":"hello"}`, `"original":"hello"`, []any{"user-1", entity.Translation{Source: "en", Destination: "de", Original: "hello"}}},
	}
}

func envelope(body, token string) []byte {
	return []byte(`{"token":"` + token + `","data":` + body + `}`)
}

//nolint:gocognit // Keep the scenario inputs and expected outcomes in one test table.
func TestRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range routeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, failure := range []bool{false, true} {
				stub := &usecaseStub{}
				if failure {
					stub.err = io.ErrUnexpectedEOF
				}

				manager := jwt.New("test-secret", time.Hour)
				token, err := manager.GenerateToken("user-1")
				require.NoError(t, err)

				routes := NewRouter(stub, stub, stub, manager, silentLogger{})
				require.Len(t, routes, 10)

				body := []byte(tc.body)
				if !strings.HasPrefix(tc.name, "auth.") {
					body = envelope(tc.body, token)
				}

				got, err := routes["v1."+tc.name](t.Context(), &nats.Msg{Data: body})
				assert.Equal(t, tc.args, stub.args)
				assert.Equal(t, t.Context(), stub.ctx)

				if failure {
					require.ErrorIs(t, err, io.ErrUnexpectedEOF)
					assert.Nil(t, got)

					continue
				}

				require.NoError(t, err)
				encoded, err := json.Marshal(got)
				require.NoError(t, err)
				assert.Contains(t, string(encoded), tc.result)
			}
		})
	}
}

//nolint:gocognit // Keep the scenario inputs and expected outcomes in one test table.
func TestInvalidRequests(t *testing.T) {
	t.Parallel()

	for _, tc := range routeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manager := jwt.New("test-secret", time.Hour)
			token, err := manager.GenerateToken("user-1")
			require.NoError(t, err)

			bodies := [][]byte{[]byte("{"), []byte(`{}`)}
			if !strings.HasPrefix(tc.name, "auth.") {
				bodies = append(bodies, envelope(tc.body, "invalid"))
				if tc.name != "translation.getHistory" {
					invalid := `{}`
					if tc.name == "task.list" {
						invalid = `{"status":"unknown"}`
					}

					bodies = append(bodies, envelope(`"wrong-type"`, token), envelope(invalid, token))
				}
			}

			for _, body := range bodies {
				stub := &usecaseStub{}
				routes := NewRouter(stub, stub, stub, manager, silentLogger{})
				got, err := routes["v1."+tc.name](t.Context(), &nats.Msg{Data: body})
				require.Error(t, err, string(body))
				assert.Nil(t, got)
				assert.Nil(t, stub.ctx, "invalid input must not reach use cases")
			}
		})
	}
}
