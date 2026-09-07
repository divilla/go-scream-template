package v1

import (
	v1 "github.com/divilla/go-scream-template/docs/proto/v1"
	"github.com/divilla/go-scream-template/internal/usecase"
	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/go-playground/validator/v10"
)

// TranslationController -.
type TranslationController struct {
	v1.UnimplementedTranslationServer

	t usecase.Translation
	l logger.Interface
	v *validator.Validate
}
