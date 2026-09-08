package middleware

import (
	"errors"

	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/rs/zerolog"
)

// Logger records completed requests through the application logger.
func Logger(l *zerolog.Logger) echo.MiddlewareFunc {
	return echomiddleware.RequestLoggerWithConfig(echomiddleware.RequestLoggerConfig{
		LogRemoteIP: true, LogMethod: true, LogURI: true,
		LogStatus: true, LogResponseSize: true, HandleError: true,
		LogRequestID: true, LogLatency: true,
		LogValuesFunc: func(_ *echo.Context, v echomiddleware.RequestLoggerValues) error {
			level := zerolog.InfoLevel
			if v.Error != nil {
				level = zerolog.ErrorLevel
			}

			event := l.WithLevel(level)

			var panicErr *echomiddleware.PanicStackError
			if errors.As(v.Error, &panicErr) {
				event.Err(panicErr.Err).Bytes("stack", panicErr.Stack)
			} else {
				event.Err(v.Error)
			}

			event.Str("request_id", v.RequestID).
				Str("remote_ip", v.RemoteIP).
				Str("method", v.Method).
				Str("uri", v.URI).
				Int("status", v.Status).
				Int64("response_size", v.ResponseSize).
				Dur("latency", v.Latency).
				Msg("restapi request")

			return nil
		},
	})
}
