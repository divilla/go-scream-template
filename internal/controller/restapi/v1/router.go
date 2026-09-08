package v1

import (
	"github.com/divilla/go-scream-template/internal/controller/restapi/middleware"
	"github.com/divilla/go-scream-template/internal/usecase"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v5"
)

// NewRoutes -.
func NewRoutes(apiV1Group *echo.Group, t usecase.Translation, u usecase.User, tk usecase.Task, jwtManager *jwt.Manager, l logger.Interface) {
	r := &V1{t: t, u: u, tk: tk, l: l, v: validator.New(validator.WithRequiredStructEnabled())}

	// Public routes
	authGroup := apiV1Group.Group("/auth")
	{
		authGroup.POST("/register", r.register)
		authGroup.POST("/login", r.login)
	}

	// Protected routes
	protected := apiV1Group.Group("", middleware.Auth(jwtManager))

	userGroup := protected.Group("/user")
	{
		userGroup.GET("/profile", r.profile)
	}

	taskGroup := protected.Group("/tasks")
	{
		taskGroup.POST("", r.createTask)
		taskGroup.GET("", r.listTasks)
		taskGroup.GET("/:id", r.getTask)
		taskGroup.PUT("/:id", r.updateTask)
		taskGroup.PATCH("/:id/status", r.transitionTask)
		taskGroup.DELETE("/:id", r.deleteTask)
	}

	translationGroup := protected.Group("/translation")
	{
		translationGroup.GET("/history", r.history)
		translationGroup.POST("/do-translate", r.doTranslate)
	}
}
