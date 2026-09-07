package v1

import (
	"github.com/divilla/go-scream-template/internal/usecase"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/go-playground/validator/v10"
)

// V1 -.
type V1 struct {
	t  usecase.Translation
	u  usecase.User
	tk usecase.Task
	l  logger.Interface
	v  *validator.Validate
}
