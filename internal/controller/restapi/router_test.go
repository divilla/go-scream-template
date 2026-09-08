package restapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/config"
	"github.com/divilla/go-scream-template/internal/controller/restapi"
	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/divilla/go-scream-template/internal/usecase"
	"github.com/divilla/go-scream-template/pkg/httpserver"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type silentLogger struct{ logger.Interface }

func (silentLogger) Zerolog() *zerolog.Logger { return new(zerolog.Nop()) }

func (silentLogger) Info(string, ...any) {}
func (silentLogger) Error(any, ...any)   {}

func newRouter(cfg *config.Config, user usecase.User) *echo.Echo {
	app := echo.New()
	restapi.NewRouter(app, cfg, nil, user, nil, jwt.New("test-secret", time.Hour), silentLogger{})

	return app
}

func get(t *testing.T, app *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

	return rec
}

// TestOperationalEndpoints checks optional integrations and isolated registries (AC 4 and 5).
func TestOperationalEndpoints(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{}
			cfg.Metrics.Enabled, cfg.Swagger.Enabled = enabled, enabled
			app := newRouter(cfg, nil)

			for _, path := range []string{"/swagger", "/swagger/"} {
				root := get(t, app, path)
				if enabled {
					assert.Equal(t, http.StatusMovedPermanently, root.Code)
					assert.Equal(t, "/swagger/index.html", root.Header().Get("Location"))
				} else {
					assert.Equal(t, http.StatusNotFound, root.Code)
					assert.Empty(t, root.Header().Get("Location"))
				}
			}

			health := get(t, app, "/healthz")
			assert.Equal(t, http.StatusOK, health.Code)
			assert.Empty(t, health.Body.String())
			metrics := get(t, app, "/metrics")

			swagger := get(t, app, "/swagger/index.html")
			if !enabled {
				assert.Equal(t, http.StatusNotFound, metrics.Code)
				assert.Equal(t, http.StatusNotFound, swagger.Code)

				return
			}

			require.Equal(t, http.StatusOK, metrics.Code)
			assert.Contains(t, metrics.Body.String(), "echo_requests_total")
			assert.Contains(t, metrics.Body.String(), `url="/healthz"`)
			assert.Contains(t, metrics.Body.String(), "go_goroutines")
			assert.Contains(t, metrics.Body.String(), "process_cpu_seconds_total")
			require.Equal(t, http.StatusOK, swagger.Code)
			assert.Contains(t, swagger.Body.String(), "SwaggerUIBundle")
			doc := get(t, app, "/swagger/doc.json")
			assert.Equal(t, http.StatusOK, doc.Code)
			assert.Contains(t, doc.Body.String(), `"/auth/register"`)
			assert.Equal(t, http.StatusOK, get(t, newRouter(cfg, nil), "/metrics").Code)
		})
	}
}

// TestMetricsHostCardinality checks bounded labels across all HTTP metric families (AC 4).
func TestMetricsHostCardinality(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"test-service", ""} {
		t.Run("app-name="+name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{}
			cfg.Metrics.Enabled = true
			cfg.App.Name = name
			app := newRouter(cfg, nil)

			const requests = 100

			for i := range requests {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
				req.Host = "client-" + strconv.Itoa(i) + ".example:8080"
				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code)
			}

			metrics := get(t, app, "/metrics")
			require.Equal(t, http.StatusOK, metrics.Code)

			for line := range strings.SplitSeq(metrics.Body.String(), "\n") {
				if strings.HasPrefix(line, "echo_") {
					assert.Contains(t, line, `host="`+name+`"`, "every HTTP metric must use the configured host label")
				}
			}

			for _, metric := range []string{
				"echo_requests_total",
				"echo_request_duration_seconds_count",
				"echo_request_size_bytes_count",
				"echo_response_size_bytes_count",
			} {
				assert.Contains(t, metrics.Body.String(), metric+`{code="200",host="`+name+`",method="GET",url="/healthz"} `+strconv.Itoa(requests)+"\n")
			}
		})
	}
}

// TestMetricsMethodCardinality bounds unknown methods across all HTTP metric families (AC 4).
func TestMetricsMethodCardinality(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/healthz", "/missing"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{}
			cfg.Metrics.Enabled = true
			app := newRouter(cfg, nil)
			methods := []string{"get", "Get", "PROPFIND"}

			for i := range 100 {
				methods = append(methods, "CUSTOM"+strconv.Itoa(i))
			}

			for _, method := range methods {
				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))

				if path == "/healthz" {
					require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
					assert.JSONEq(t, `{"message":"Method Not Allowed"}`, rec.Body.String())
				} else {
					require.Equal(t, http.StatusNotFound, rec.Code)
				}
			}

			metrics := get(t, app, "/metrics")
			require.Equal(t, http.StatusOK, metrics.Code)
			assertUnknownMethodMetrics(t, metrics.Body.String(), len(methods))
		})
	}
}

func assertUnknownMethodMetrics(t *testing.T, body string, requests int) {
	t.Helper()

	for _, metric := range []string{
		"echo_requests_total", "echo_request_duration_seconds_count",
		"echo_request_size_bytes_count", "echo_response_size_bytes_count",
	} {
		var series int

		for line := range strings.SplitSeq(body, "\n") {
			if strings.HasPrefix(line, metric+"{") {
				series++

				assert.Contains(t, line, `method="UNKNOWN"`)
				assert.True(t, strings.HasSuffix(line, " "+strconv.Itoa(requests)), line)
			}
		}

		assert.Equal(t, 1, series, metric)
	}
}

func TestMetricsStandardMethods(t *testing.T) {
	t.Parallel()

	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPatch,
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{}
			cfg.Metrics.Enabled = true
			app := newRouter(cfg, nil)
			app.Add(method, "/method", func(ctx *echo.Context) error {
				assert.Equal(t, method, ctx.Request().Method)

				return ctx.NoContent(http.StatusOK)
			})

			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, "/method", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			metrics := get(t, app, "/metrics")
			require.Equal(t, http.StatusOK, metrics.Code)

			for _, metric := range []string{
				"echo_requests_total", "echo_request_duration_seconds_count",
				"echo_request_size_bytes_count", "echo_response_size_bytes_count",
			} {
				assert.Contains(t, metrics.Body.String(), metric+`{code="200",host="",method="`+method+`",url="/method"} 1`+"\n")
			}
		})
	}
}

func TestRequestBodyLimit(t *testing.T) {
	t.Parallel()

	server := httpserver.New(silentLogger{})

	t.Cleanup(func() { require.NoError(t, server.Shutdown()) })

	manager := jwt.New("test-secret", time.Hour)
	token, err := manager.GenerateToken("user-1")
	require.NoError(t, err)
	restapi.NewRouter(server.App, &config.Config{}, nil, nil, nil, manager, silentLogger{})

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/v1/auth/register"},
		{http.MethodPost, "/v1/auth/login"},
		{http.MethodPost, "/v1/tasks"},
		{http.MethodPut, "/v1/tasks/task-1"},
		{http.MethodPatch, "/v1/tasks/task-1/status"},
		{http.MethodPost, "/v1/translation/do-translate"},
	} {
		for _, chunked := range []bool{false, true} {
			req := httptest.NewRequestWithContext(t.Context(), route.method, route.path, strings.NewReader(`{"username":"alice","email":"alice@example.com","password":"secret123","padding":"`+strings.Repeat("x", 4<<20)+`"}`))
			req.Header.Set("Content-Type", "application/json")

			if !strings.HasPrefix(route.path, "/v1/auth/") {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			if chunked {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}

			rec := httptest.NewRecorder()
			server.App.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "%s %s chunked=%t", route.method, route.path, chunked)
			assert.JSONEq(t, `{"message":"Request Entity Too Large"}`, rec.Body.String())
		}
	}
}

func TestXMLRequestBodyLimit(t *testing.T) {
	t.Parallel()

	const document = `<user><Username>alice</Username><Email>alice@example.com</Email><Password>secret123</Password></user>`

	for _, size := range []int{4 << 20, (4 << 20) + 1} {
		for _, chunked := range []bool{false, true} {
			server := httpserver.New(silentLogger{})

			t.Cleanup(func() { require.NoError(t, server.Shutdown()) })

			user := &tracedUser{}
			restapi.NewRouter(server.App, &config.Config{}, nil, user, nil, jwt.New("test-secret", time.Hour), silentLogger{})

			body := document + "<!--" + strings.Repeat("x", size-len(document)-len("<!---->")) + "-->"
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/xml")

			if chunked {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}

			rec := httptest.NewRecorder()
			server.App.ServeHTTP(rec, req)

			if size > 4<<20 {
				assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
				assert.JSONEq(t, `{"message":"Request Entity Too Large"}`, rec.Body.String())
				assert.Nil(t, user.ctx, "use case must not run for oversized XML")
			} else {
				assert.Equal(t, http.StatusCreated, rec.Code)
				assert.NotNil(t, user.ctx)
			}
		}
	}
}

func TestHEADRoutes(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		server := httpserver.New(silentLogger{})

		t.Cleanup(func() { require.NoError(t, server.Shutdown()) })

		cfg := &config.Config{}
		cfg.Metrics.Enabled, cfg.Swagger.Enabled = enabled, enabled
		user := &headUser{}
		manager := jwt.New("test-secret", time.Hour)
		restapi.NewRouter(server.App, cfg, nil, user, nil, manager, silentLogger{})

		optionalStatus := http.StatusNotFound
		if enabled {
			optionalStatus = http.StatusOK
		}

		for _, tc := range []struct {
			path   string
			status int
		}{
			{"/healthz", http.StatusOK},
			{"/healthz/", http.StatusOK},
			{"/metrics", optionalStatus},
			{"/swagger/index.html", optionalStatus},
			{"/v1/user/profile", http.StatusUnauthorized},
			{"/v1/tasks", http.StatusUnauthorized},
			{"/v1/tasks/", http.StatusUnauthorized},
			{"/v1/tasks/task-1", http.StatusUnauthorized},
			{"/v1/translation/history", http.StatusUnauthorized},
			{"/missing", http.StatusNotFound},
		} {
			rec := httptest.NewRecorder()
			server.App.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodHead, tc.path, nil))
			assert.Equal(t, tc.status, rec.Code, tc.path)
			assert.Empty(t, rec.Body.String(), tc.path)
		}

		token, err := manager.GenerateToken("user-1")
		require.NoError(t, err)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/v1/user/profile", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		rec := httptest.NewRecorder()
		server.App.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Empty(t, rec.Body.String())
		assert.Equal(t, "user-1", user.id)
	}
}

type headUser struct {
	usecase.User
	id string
}

func (u *headUser) GetUser(_ context.Context, id string) (entity.User, error) {
	u.id = id

	return entity.User{ID: id}, nil
}

func TestTaskTrailingSlash(t *testing.T) {
	t.Parallel()

	app := newRouter(&config.Config{}, nil)

	for _, path := range []string{"/v1/tasks", "/v1/tasks/"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.JSONEq(t, `{"error":"missing authorization header"}`, rec.Body.String())
			assert.Empty(t, rec.Header().Get("Location"))
		}
	}
}

type tracedUser struct {
	usecase.User
	span trace.SpanContext
	ctx  context.Context
}

func (u *tracedUser) Register(ctx context.Context, _, _, _ string) (entity.User, error) {
	u.ctx, u.span = ctx, trace.SpanContextFromContext(ctx)

	return entity.User{ID: "user-1"}, nil
}

// TestTracing changes the process-global provider used by application startup.
func TestTracing(t *testing.T) { //nolint:paralleltest // OpenTelemetry globals must be restored before parallel tests run.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	oldProvider, oldPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(oldProvider)
		otel.SetTextMapPropagator(oldPropagator)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	for _, enabled := range []bool{false, true} {
		cfg := &config.Config{}
		cfg.Tracing.Enabled = enabled
		cfg.App.Name = "test-service"
		user := &tracedUser{}
		app := newRouter(cfg, user)
		ctx, cancel := context.WithCancel(t.Context())
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/auth/register", strings.NewReader(`{"username":"alice","email":"alice@example.com","password":"secret123"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")

		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		cancel()
		require.Equal(t, http.StatusCreated, rec.Code)
		assert.ErrorIs(t, user.ctx.Err(), context.Canceled)
		assert.Equal(t, enabled, user.span.IsValid())

		if enabled {
			assert.Equal(t, "0123456789abcdef0123456789abcdef", user.span.TraceID().String())

			spans := recorder.Ended()
			require.Len(t, spans, 1)
			assert.Equal(t, "POST /v1/auth/register", spans[0].Name())
			assert.Equal(t, "0123456789abcdef", spans[0].Parent().SpanID().String())
		} else {
			assert.Empty(t, recorder.Ended())
		}
	}
}

func TestRoutingErrors(t *testing.T) {
	t.Parallel()

	app := newRouter(&config.Config{}, nil)

	tests := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/missing", `{"message":"Not Found"}`, http.StatusNotFound},
		{http.MethodGet, "/HEALTHZ", `{"message":"Not Found"}`, http.StatusNotFound},
		{http.MethodPost, "/healthz", `{"message":"Method Not Allowed"}`, http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		assert.Equal(t, tc.status, rec.Code)
		assert.JSONEq(t, tc.body, rec.Body.String())
	}
}
