package v1

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/labstack/echo/v5"
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

func (silentLogger) Error(any, ...any)   {}
func (silentLogger) Info(string, ...any) {}

type routeCase struct {
	method   string
	path     string
	body     string
	status   int
	contains string
	args     []any
}

func routeCases() []routeCase {
	return []routeCase{
		{http.MethodPost, "/auth/register", `{"username":"alice","email":"alice@example.com","password":"secret123"}`, http.StatusCreated, `"username":"alice"`, []any{"alice", "alice@example.com", "secret123"}},
		{http.MethodPost, "/auth/login", `{"email":"alice@example.com","password":"secret123"}`, http.StatusOK, `"token":"signed-token"`, []any{"alice@example.com", "secret123"}},
		{http.MethodGet, "/user/profile", "", http.StatusOK, `"id":"user-1"`, []any{"user-1"}},
		{http.MethodPost, "/tasks", `{"title":"task","description":"details","userID":"attacker"}`, http.StatusCreated, `"title":"task"`, []any{"user-1", "task", "details"}},
		{http.MethodGet, "/tasks", "", http.StatusOK, `"total":1`, []any{"user-1", (*entity.TaskStatus)(nil), 10, 0}},
		{http.MethodGet, "/tasks/task-1", "", http.StatusOK, `"id":"task-1"`, []any{"user-1", "task-1"}},
		{http.MethodPut, "/tasks/task-1", `{"title":"edited","description":"new"}`, http.StatusOK, `"title":"edited"`, []any{"user-1", "task-1", "edited", "new"}},
		{http.MethodPatch, "/tasks/task-1/status", `{"status":"in_progress"}`, http.StatusOK, `"status":"in_progress"`, []any{"user-1", "task-1", entity.TaskStatusInProgress}},
		{http.MethodDelete, "/tasks/task-1", "", http.StatusNoContent, "", []any{"user-1", "task-1"}},
		{http.MethodGet, "/translation/history", "", http.StatusOK, `"history":`, []any{"user-1"}},
		{http.MethodPost, "/translation/do-translate", `{"source":"en","destination":"de","original":"hello"}`, http.StatusOK, `"original":"hello"`, []any{"user-1", entity.Translation{Source: "en", Destination: "de", Original: "hello"}}},
	}
}

func testRouter(t *testing.T, stub *usecaseStub) (app *echo.Echo, token string) {
	t.Helper()

	app = echo.New()
	manager := jwt.New("test-secret", time.Hour)
	token, err := manager.GenerateToken("user-1")
	require.NoError(t, err)
	NewRoutes(app.Group("/v1"), stub, stub, stub, manager, silentLogger{})

	return app, token
}

func sendRequest(t *testing.T, app *echo.Echo, route *routeCase, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), route.method, "/v1"+route.path, strings.NewReader(route.body))
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	return rec
}

// TestRoutes preserves the request/response and use-case contracts (AC 1 and 2).
func TestRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range routeCases() {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{}
			app, token := testRouter(t, stub)
			rec := sendRequest(t, app, &tc, token)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.contains)
			assert.Equal(t, tc.args, stub.args)
			assert.Equal(t, t.Context(), stub.ctx)

			if tc.status == http.StatusNoContent {
				assert.Empty(t, rec.Body.String())
			}
		})
	}
}

func TestHandlerFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range routeCases() {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{err: fmt.Errorf("private database failure: %w", io.ErrUnexpectedEOF)}
			app, token := testRouter(t, stub)
			rec := sendRequest(t, app, &tc, token)
			require.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.Contains(t, rec.Body.String(), `"error":`)
			assert.NotContains(t, rec.Body.String(), "private database failure")
		})
	}
}

func TestInvalidBodies(t *testing.T) {
	t.Parallel()

	for _, route := range routeCases() {
		if route.body == "" {
			continue
		}

		for _, body := range []string{"{", "{}"} {
			t.Run(route.method+route.path+body, func(t *testing.T) {
				t.Parallel()

				stub := &usecaseStub{}
				app, token := testRouter(t, stub)
				requestRoute := route
				requestRoute.body = body
				rec := sendRequest(t, app, &requestRoute, token)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				assert.JSONEq(t, `{"error":"invalid request body"}`, rec.Body.String())
				assert.Nil(t, stub.ctx)
			})
		}
	}
}

func TestProtectedRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range routeCases() {
		if strings.HasPrefix(tc.path, "/auth/") {
			continue
		}

		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{}
			app, _ := testRouter(t, stub)
			rec := sendRequest(t, app, &tc, "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Nil(t, stub.ctx)
		})
	}
}

func TestDomainErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path    string
		method  string
		err     error
		status  int
		message string
	}{
		{"/auth/register", http.MethodPost, entity.ErrUserAlreadyExists, http.StatusConflict, "user already exists"},
		{"/auth/login", http.MethodPost, entity.ErrInvalidCredentials, http.StatusUnauthorized, "invalid credentials"},
		{"/user/profile", http.MethodGet, entity.ErrUserNotFound, http.StatusNotFound, "user not found"},
		{"/tasks/task-1", http.MethodGet, entity.ErrTaskNotFound, http.StatusNotFound, "task not found"},
		{"/tasks/task-1", http.MethodGet, entity.ErrTaskForbidden, http.StatusForbidden, "forbidden"},
		{"/tasks/task-1", http.MethodPut, entity.ErrTaskNotFound, http.StatusNotFound, "task not found"},
		{"/tasks/task-1", http.MethodPut, entity.ErrTaskForbidden, http.StatusForbidden, "forbidden"},
		{"/tasks/task-1/status", http.MethodPatch, entity.ErrTaskNotFound, http.StatusNotFound, "task not found"},
		{"/tasks/task-1/status", http.MethodPatch, entity.ErrTaskForbidden, http.StatusForbidden, "forbidden"},
		{"/tasks/task-1/status", http.MethodPatch, entity.ErrInvalidTransition, http.StatusBadRequest, "invalid status transition"},
		{"/tasks/task-1", http.MethodDelete, entity.ErrTaskNotFound, http.StatusNotFound, "task not found"},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+tc.message, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{err: tc.err}
			app, token := testRouter(t, stub)

			for _, route := range routeCases() {
				if route.path != tc.path || route.method != tc.method {
					continue
				}

				rec := sendRequest(t, app, &route, token)
				assert.Equal(t, tc.status, rec.Code)
				assert.JSONEq(t, `{"error":"`+tc.message+`"}`, rec.Body.String())
			}
		})
	}
}

func TestMissingHandlerIdentity(t *testing.T) {
	t.Parallel()

	r := &V1{}

	handlers := []echo.HandlerFunc{r.profile, r.createTask, r.listTasks, r.getTask, r.updateTask, r.transitionTask, r.deleteTask, r.history, r.doTranslate}
	for _, handler := range handlers {
		rec := httptest.NewRecorder()
		ctx := echo.New().NewContext(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), rec)
		require.NoError(t, handler(ctx))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.JSONEq(t, `{"error":"unauthorized"}`, rec.Body.String())
	}
}

func TestTaskListParameters(t *testing.T) {
	t.Parallel()

	for _, query := range []string{"?status=done&limit=3&offset=2", "?status=invalid", "?limit=bad&offset=bad"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{}
			app, token := testRouter(t, stub)

			rec := sendRequest(t, app, &routeCase{method: http.MethodGet, path: "/tasks" + query}, token)
			if strings.Contains(query, "invalid") {
				assert.Equal(t, http.StatusBadRequest, rec.Code)
				assert.Nil(t, stub.ctx)

				return
			}

			require.Equal(t, http.StatusOK, rec.Code)

			if strings.Contains(query, "done") {
				status := entity.TaskStatusDone
				assert.Equal(t, []any{"user-1", &status, 3, 2}, stub.args)
			} else {
				assert.Equal(t, []any{"user-1", (*entity.TaskStatus)(nil), 10, 0}, stub.args)
			}
		})
	}
}
