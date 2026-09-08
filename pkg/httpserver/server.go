// Package httpserver implements HTTP server.
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/labstack/echo/v5"
	"golang.org/x/sync/errgroup"
)

const (
	_defaultAddr            = ":80"
	_defaultReadTimeout     = 5 * time.Second
	_defaultWriteTimeout    = 5 * time.Second
	_defaultShutdownTimeout = 3 * time.Second
	_defaultBodyLimit       = 4 << 20 // 4 MiB.
)

// Server owns the HTTP listener and its graceful shutdown.
type Server struct {
	ctx             context.Context
	stop            context.CancelFunc
	eg              *errgroup.Group
	start           sync.Once
	App             *echo.Echo
	notify          chan error
	address         string
	readTimeout     time.Duration
	writeTimeout    time.Duration
	shutdownTimeout time.Duration
	shutdownErr     error
	httpServer      *http.Server
	logger          logger.Interface
}

// New configures an Echo server without starting its listener.
func New(l logger.Interface, opts ...Option) *Server {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	group, ctx := errgroup.WithContext(ctx)

	s := &Server{
		ctx: ctx, stop: stop, eg: group,
		App:    echo.NewWithConfig(echo.Config{Router: bodyLimitRouter{echo.NewRouter(echo.RouterConfig{AutoHandleHEAD: true})}}),
		notify: make(chan error, 1), address: _defaultAddr,
		readTimeout: _defaultReadTimeout, writeTimeout: _defaultWriteTimeout,
		shutdownTimeout: _defaultShutdownTimeout, logger: l,
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Start starts the listener once; failures are available through Notify.
func (s *Server) Start() {
	s.start.Do(func() {
		s.eg.Go(func() error {
			defer close(s.notify)
			defer s.stop()

			cfg := echo.StartConfig{
				// Echo uses zero for its default and negative values to disable shutdown.
				Address: s.address, GracefulTimeout: max(s.shutdownTimeout, time.Nanosecond),
				HideBanner: true, HidePort: true,
				BeforeServeFunc: s.configureHTTP,
				OnShutdownError: func(err error) {
					s.shutdownErr = errors.Join(err, s.httpServer.Close())
				},
			}

			err := cfg.Start(s.ctx, s.App)
			if err != nil {
				s.notify <- err
			}

			return err
		})
	})
}

func (s *Server) configureHTTP(server *http.Server) error {
	s.httpServer = server
	server.ReadTimeout = s.readTimeout
	server.WriteTimeout = s.writeTimeout
	s.logger.Info("restapi server - Server - Started")

	return nil
}

// Notify reports listener failures and closes when serving ends.
func (s *Server) Notify() <-chan error { return s.notify }

// Shutdown waits for in-flight requests up to the configured timeout.
func (s *Server) Shutdown() error {
	s.stop()

	err := s.eg.Wait()
	if errors.Is(err, context.Canceled) {
		err = nil
	}

	err = errors.Join(err, s.shutdownErr)
	if err != nil {
		s.logger.Error(err, "restapi server - Server - Shutdown")
	}

	s.logger.Info("restapi server - Server - Shutdown")

	return err
}
