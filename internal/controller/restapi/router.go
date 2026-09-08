package restapi

import (
	"net/http"

	"github.com/divilla/go-scream-template/config"
	_ "github.com/divilla/go-scream-template/docs" // Swagger docs.
	"github.com/divilla/go-scream-template/internal/controller/restapi/middleware"
	v1 "github.com/divilla/go-scream-template/internal/controller/restapi/v1"
	"github.com/divilla/go-scream-template/internal/usecase"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	echootel "github.com/labstack/echo-opentelemetry"
	echoprometheus "github.com/labstack/echo-prometheus"
	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	echoSwagger "github.com/swaggo/echo-swagger/v2"
)

// NewRouter -.
// Swagger spec:
//
//	@title       Go Clean Template API
//	@description Multi-domain clean architecture template with translation, user, and task management
//	@version     1.0
//	@host        localhost:8080
//	@BasePath    /v1
//	@securityDefinitions.apikey BearerAuth
//	@in header
//	@name Authorization
func NewRouter(app *echo.Echo, cfg *config.Config, t usecase.Translation, u usecase.User, tk usecase.Task, jwtManager *jwt.Manager, l logger.Interface) {
	// Options
	app.Pre(echomiddleware.RemoveTrailingSlash())
	app.Use(echomiddleware.RequestID())
	app.Use(middleware.Logger(l.Zerolog()))
	app.Use(echomiddleware.RecoverWithConfig(echomiddleware.RecoverConfig{DisableStackAll: true}))

	// Prometheus metrics
	if cfg.Metrics.Enabled {
		registry := prometheus.NewRegistry()
		registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		app.Use(echoprometheus.NewMiddlewareWithConfig(echoprometheus.MiddlewareConfig{
			Subsystem:                 "echo",
			Registerer:                registry,
			DoNotUseRequestPathFor404: true,
			LabelFuncs: map[string]echoprometheus.LabelValueFunc{
				// Keep client-controlled Host headers from creating unbounded metric series.
				"host": func(_ *echo.Context, _ error) string { return cfg.App.Name },
				"method": func(ctx *echo.Context, _ error) string {
					// Bound client-controlled methods without changing routing semantics.
					switch method := ctx.Request().Method; method {
					case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
						http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPatch:
						return method
					default:
						return "UNKNOWN"
					}
				},
			},
		}))
		app.GET("/metrics", echoprometheus.NewHandlerWithConfig(echoprometheus.HandlerConfig{Gatherer: registry}))
	}

	// Swagger
	if cfg.Swagger.Enabled {
		app.GET("/swagger", func(ctx *echo.Context) error {
			return ctx.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
		})
		app.GET("/swagger/*", func(ctx *echo.Context) error {
			// Swagger accepts only GET; Echo's HEAD wrapper suppresses its body.
			if req := ctx.Request(); req.Method == http.MethodHead {
				head := req.Clone(req.Context())
				head.Method = http.MethodGet

				ctx.SetRequest(head)
				defer ctx.SetRequest(req)
			}

			return echoSwagger.WrapHandler(ctx)
		})
	}

	// K8s probe
	app.GET("/healthz", func(ctx *echo.Context) error { return ctx.NoContent(http.StatusOK) })

	// Routers
	apiV1Group := app.Group("/v1")
	{
		if cfg.Tracing.Enabled {
			apiV1Group.Use(echootel.NewMiddleware(cfg.App.Name))
		}

		v1.NewRoutes(apiV1Group, t, u, tk, jwtManager, l)
	}
}
