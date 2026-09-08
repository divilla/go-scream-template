package v1

import (
	"errors"
	"net/http"

	"github.com/divilla/go-scream-template/internal/controller/restapi/v1/response"
	"github.com/labstack/echo/v5"
)

func bodyErrorResponse(ctx *echo.Context, err error) error {
	if errors.Is(err, echo.ErrStatusRequestEntityTooLarge) {
		return echo.ErrStatusRequestEntityTooLarge
	}

	return errorResponse(ctx, http.StatusBadRequest, "invalid request body")
}

func errorResponse(ctx *echo.Context, code int, msg string) error {
	return ctx.JSON(code, response.Error{Error: msg})
}
