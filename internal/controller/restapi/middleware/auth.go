package middleware

import (
	"net/http"
	"strings"

	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/labstack/echo/v5"
)

const _bearerParts = 2

type errorResponse struct {
	Error string `json:"error"`
}

// Auth returns a JWT authentication middleware for Echo.
func Auth(jwtManager *jwt.Manager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx *echo.Context) error {
			header := ctx.Request().Header.Get("Authorization")
			if header == "" {
				return ctx.JSON(http.StatusUnauthorized, errorResponse{Error: "missing authorization header"})
			}

			parts := strings.SplitN(header, " ", _bearerParts)
			if len(parts) != _bearerParts || parts[0] != "Bearer" {
				return ctx.JSON(http.StatusUnauthorized, errorResponse{Error: "invalid authorization header format"})
			}

			userID, err := jwtManager.ParseToken(parts[1])
			if err != nil {
				return ctx.JSON(http.StatusUnauthorized, errorResponse{Error: "invalid or expired token"})
			}

			ctx.Set("userID", userID)

			return next(ctx)
		}
	}
}
