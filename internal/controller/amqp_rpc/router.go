package v1

import (
	v1 "github.com/divilla/go-scream-template/internal/controller/amqp_rpc/v1"
	"github.com/divilla/go-scream-template/internal/usecase"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/divilla/go-scream-template/pkg/rabbitmq/rmq_rpc/server"
)

// NewRouter -.
func NewRouter(t usecase.Translation, u usecase.User, tk usecase.Task, j *jwt.Manager, l logger.Interface) map[string]server.CallHandler {
	routes := make(map[string]server.CallHandler)

	{
		v1.NewRoutes(routes, t, u, tk, j, l)
	}

	return routes
}
