//nolint:errcheck // Test dispatch uses the request type fixed by each case; transport stubs mirror REST.
package v1

import (
	"context"
	"io"
	"testing"
	"time"

	pb "github.com/divilla/go-scream-template/docs/proto/v1"
	grpcmw "github.com/divilla/go-scream-template/internal/controller/grpc/middleware"
	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
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

type grpcCase struct {
	service, method string
	request         any
	args            []any
}

func grpcCases() []grpcCase {
	status := entity.TaskStatusDone

	return []grpcCase{
		{"v1.AuthService", "Register", &pb.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret123"}, []any{"alice", "alice@example.com", "secret123"}},
		{"v1.AuthService", "Login", &pb.LoginRequest{Email: "alice@example.com", Password: "secret123"}, []any{"alice@example.com", "secret123"}},
		{"v1.AuthService", "GetProfile", &pb.GetProfileRequest{}, []any{"user-1"}},
		{"v1.TaskService", "CreateTask", &pb.CreateTaskRequest{Title: "task", Description: "details"}, []any{"user-1", "task", "details"}},
		{"v1.TaskService", "GetTask", &pb.GetTaskRequest{Id: "task-1"}, []any{"user-1", "task-1"}},
		{"v1.TaskService", "ListTasks", &pb.ListTasksRequest{Status: "done", Limit: 3, Offset: 2}, []any{"user-1", &status, 3, 2}},
		{"v1.TaskService", "UpdateTask", &pb.UpdateTaskRequest{Id: "task-1", Title: "edited", Description: "new"}, []any{"user-1", "task-1", "edited", "new"}},
		{"v1.TaskService", "TransitionTask", &pb.TransitionTaskRequest{Id: "task-1", Status: "in_progress"}, []any{"user-1", "task-1", entity.TaskStatusInProgress}},
		{"v1.TaskService", "DeleteTask", &pb.DeleteTaskRequest{Id: "task-1"}, []any{"user-1", "task-1"}},
		{"v1.Translation", "GetHistory", &pb.GetHistoryRequest{}, []any{"user-1"}},
		{"v1.Translation", "Translate", &pb.TranslateRequest{Source: "en", Destination: "de", Original: "hello"}, []any{"user-1", entity.Translation{Source: "en", Destination: "de", Original: "hello"}}},
	}
}

//nolint:gocyclo,cyclop // Keep the scenario inputs and expected outcomes in one test table.
func invokeController(t *testing.T, tc grpcCase, stub *usecaseStub, authenticated bool) (any, error) {
	t.Helper()
	ctx := t.Context()

	if authenticated {
		manager := jwt.New("test-secret", time.Hour)
		token, err := manager.GenerateToken("user-1")
		require.NoError(t, err)

		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", token))

		_, err = grpcmw.AuthInterceptor(manager)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/protected"}, func(inner context.Context, _ any) (any, error) {
			ctx = inner

			return nil, nil
		})
		require.NoError(t, err)
	}

	validate := validator.New()
	auth := &AuthController{u: stub, l: silentLogger{}, v: validate}
	task := &TaskController{tk: stub, l: silentLogger{}, v: validate}
	translation := &TranslationController{t: stub, l: silentLogger{}, v: validate}

	switch tc.method {
	case "Register":
		return auth.Register(ctx, tc.request.(*pb.RegisterRequest))
	case "Login":
		return auth.Login(ctx, tc.request.(*pb.LoginRequest))
	case "GetProfile":
		return auth.GetProfile(ctx, tc.request.(*pb.GetProfileRequest))
	case "CreateTask":
		return task.CreateTask(ctx, tc.request.(*pb.CreateTaskRequest))
	case "GetTask":
		return task.GetTask(ctx, tc.request.(*pb.GetTaskRequest))
	case "ListTasks":
		return task.ListTasks(ctx, tc.request.(*pb.ListTasksRequest))
	case "UpdateTask":
		return task.UpdateTask(ctx, tc.request.(*pb.UpdateTaskRequest))
	case "TransitionTask":
		return task.TransitionTask(ctx, tc.request.(*pb.TransitionTaskRequest))
	case "DeleteTask":
		return task.DeleteTask(ctx, tc.request.(*pb.DeleteTaskRequest))
	case "GetHistory":
		return translation.GetHistory(ctx, tc.request.(*pb.GetHistoryRequest))
	case "Translate":
		return translation.Translate(ctx, tc.request.(*pb.TranslateRequest))
	default:
		t.Fatalf("unknown method %s", tc.method)

		return nil, nil
	}
}

func TestHandlers(t *testing.T) {
	t.Parallel()

	for _, tc := range grpcCases() {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{}
			got, err := invokeController(t, tc, stub, true)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.args, stub.args)
			assert.Equal(t, t.Context().Done(), stub.ctx.Done())
			stub.err = io.ErrUnexpectedEOF
			_, err = invokeController(t, tc, stub, true)
			require.Equal(t, codes.Internal, grpcstatus.Code(err))
			assert.NotContains(t, err.Error(), io.ErrUnexpectedEOF.Error())

			if tc.method != "Login" && tc.method != "Register" {
				stub.ctx = nil
				_, err = invokeController(t, tc, stub, false)
				assert.Equal(t, codes.Unauthenticated, grpcstatus.Code(err))
				assert.Nil(t, stub.ctx)
			}
		})
	}
}

func TestDomainErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method string
		err    error
		code   codes.Code
	}{
		{"Register", entity.ErrUserAlreadyExists, codes.AlreadyExists},
		{"Login", entity.ErrInvalidCredentials, codes.Unauthenticated},
		{"GetProfile", entity.ErrUserNotFound, codes.NotFound},
		{"GetTask", entity.ErrTaskNotFound, codes.NotFound},
		{"GetTask", entity.ErrTaskForbidden, codes.PermissionDenied},
		{"UpdateTask", entity.ErrTaskNotFound, codes.NotFound},
		{"UpdateTask", entity.ErrTaskForbidden, codes.PermissionDenied},
		{"TransitionTask", entity.ErrTaskNotFound, codes.NotFound},
		{"TransitionTask", entity.ErrTaskForbidden, codes.PermissionDenied},
		{"TransitionTask", entity.ErrInvalidTransition, codes.InvalidArgument},
		{"DeleteTask", entity.ErrTaskNotFound, codes.NotFound},
	}
	for _, failure := range cases {
		t.Run(failure.method+failure.code.String(), func(t *testing.T) {
			t.Parallel()

			for _, tc := range grpcCases() {
				if tc.method == failure.method {
					_, err := invokeController(t, tc, &usecaseStub{err: failure.err}, true)
					assert.Equal(t, failure.code, grpcstatus.Code(err))
				}
			}
		})
	}
}

func TestListFilters(t *testing.T) {
	t.Parallel()

	for _, filter := range []string{"", "unknown"} {
		t.Run(filter, func(t *testing.T) {
			t.Parallel()

			stub := &usecaseStub{}

			_, err := invokeController(t, grpcCase{method: "ListTasks", request: &pb.ListTasksRequest{Status: filter}}, stub, true)
			if filter == "unknown" {
				require.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))
				assert.Nil(t, stub.ctx)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, []any{"user-1", (*entity.TaskStatus)(nil), 0, 0}, stub.args)
		})
	}
}
