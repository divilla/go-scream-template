// Package app configures and runs application.
package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/divilla/go-scream-template/config"
	amqprpc "github.com/divilla/go-scream-template/internal/controller/amqp_rpc"
	"github.com/divilla/go-scream-template/internal/controller/grpc"
	grpcmw "github.com/divilla/go-scream-template/internal/controller/grpc/middleware"
	natsrpc "github.com/divilla/go-scream-template/internal/controller/nats_rpc"
	"github.com/divilla/go-scream-template/internal/controller/restapi"
	persistTaskRepo "github.com/divilla/go-scream-template/internal/repo/persistent/task"
	persistTranslationRepo "github.com/divilla/go-scream-template/internal/repo/persistent/translation"
	persistUserRepo "github.com/divilla/go-scream-template/internal/repo/persistent/user"
	"github.com/divilla/go-scream-template/internal/repo/webapi"
	"github.com/divilla/go-scream-template/internal/usecase"
	"github.com/divilla/go-scream-template/internal/usecase/task"
	"github.com/divilla/go-scream-template/internal/usecase/translation"
	"github.com/divilla/go-scream-template/internal/usecase/user"
	"github.com/divilla/go-scream-template/pkg/grpcserver"
	"github.com/divilla/go-scream-template/pkg/httpserver"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	natsRPCServer "github.com/divilla/go-scream-template/pkg/nats/nats_rpc/server"
	"github.com/divilla/go-scream-template/pkg/postgres"
	rmqRPCServer "github.com/divilla/go-scream-template/pkg/rabbitmq/rmq_rpc/server"
	"github.com/divilla/go-scream-template/pkg/tracing"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	pbgrpc "google.golang.org/grpc"
)

type useCases struct {
	translation usecase.Translation
	user        usecase.User
	task        usecase.Task
}

type server interface {
	Start()
	Notify() <-chan error
	Shutdown() error
}

type servers struct {
	rmq, nats, grpc, http server
}

type serverConstructors struct {
	rmq  func(string, string, map[string]rmqRPCServer.CallHandler, logger.Interface, ...rmqRPCServer.Option) (*rmqRPCServer.Server, error)
	nats func(string, string, map[string]natsRPCServer.CallHandler, logger.Interface, ...natsRPCServer.Option) (*natsRPCServer.Server, error)
}

type dependencies struct {
	tracing  func(context.Context, tracing.Config) (tracing.ShutdownFunc, error)
	postgres func(string, ...postgres.Option) (*postgres.Postgres, error)
	servers  func(*config.Config, useCases, *jwt.Manager, logger.Interface) servers
}

func initUseCases(pg *postgres.Postgres, jwtManager *jwt.Manager) useCases {
	translationRepo := persistTranslationRepo.New(pg)
	taskRepo := persistTaskRepo.New(pg)
	userRepo := persistUserRepo.New(pg)

	return useCases{
		user:        user.New(userRepo, jwtManager),
		task:        task.New(taskRepo),
		translation: translation.New(translationRepo, webapi.New()),
	}
}

func initServers(cfg *config.Config, uc useCases, jwtManager *jwt.Manager, l logger.Interface) servers {
	return initServersWith(cfg, uc, jwtManager, l, serverConstructors{rmq: rmqRPCServer.New, nats: natsRPCServer.New})
}

func initServersWith(cfg *config.Config, uc useCases, jwtManager *jwt.Manager, l logger.Interface, constructors serverConstructors) servers {
	// RabbitMQ RPC Server
	rmqRouter := amqprpc.NewRouter(uc.translation, uc.user, uc.task, jwtManager, l)

	rmqServer, err := constructors.rmq(cfg.RMQ.URL, cfg.RMQ.ServerExchange, rmqRouter, l)
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - rmqServer - server.New: %w", err))
	}

	// NATS RPC Server
	natsRouter := natsrpc.NewRouter(uc.translation, uc.user, uc.task, jwtManager, l)

	natsServer, err := constructors.nats(cfg.NATS.URL, cfg.NATS.ServerExchange, natsRouter, l)
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - natsServer - server.New: %w", err))
	}

	// gRPC Server
	grpcServer := grpcserver.New(
		l,
		grpcserver.Port(cfg.GRPC.Port),
		grpcserver.ServerOptions(
			pbgrpc.UnaryInterceptor(grpcmw.AuthInterceptor(jwtManager)),
			pbgrpc.StatsHandler(otelgrpc.NewServerHandler()),
		),
	)
	grpc.NewRouter(grpcServer.App, uc.translation, uc.user, uc.task, l)

	// HTTP Server
	httpServer := httpserver.New(l, httpserver.Port(cfg.HTTP.Port))
	restapi.NewRouter(httpServer.App, cfg, uc.translation, uc.user, uc.task, jwtManager, l)

	return servers{
		rmq:  rmqServer,
		nats: natsServer,
		grpc: grpcServer,
		http: httpServer,
	}
}

func (s *servers) startServers() {
	s.rmq.Start()
	s.nats.Start()
	s.grpc.Start()
	s.http.Start()
}

func (s *servers) waitForShutdown(l logger.Interface) {
	interrupt := make(chan os.Signal, 1)

	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)

	s.waitForShutdownSignal(l, interrupt)
}

func (s *servers) waitForShutdownSignal(l logger.Interface, interrupt <-chan os.Signal) {
	var err error

	select {
	case sig := <-interrupt:
		l.Info("app - Run - signal: %s", sig.String())
	case err = <-s.http.Notify():
		l.Error(fmt.Errorf("app - Run - httpServer.Notify: %w", err))
	case err = <-s.grpc.Notify():
		l.Error(fmt.Errorf("app - Run - grpcServer.Notify: %w", err))
	case err = <-s.rmq.Notify():
		l.Error(fmt.Errorf("app - Run - rmqServer.Notify: %w", err))
	case err = <-s.nats.Notify():
		l.Error(fmt.Errorf("app - Run - natsServer.Notify: %w", err))
	}

	s.shutdownServers(l)
}

func (s *servers) shutdownServers(l logger.Interface) {
	if err := s.http.Shutdown(); err != nil {
		l.Error(fmt.Errorf("app - Run - httpServer.Shutdown: %w", err))
	}

	if err := s.grpc.Shutdown(); err != nil {
		l.Error(fmt.Errorf("app - Run - grpcServer.Shutdown: %w", err))
	}

	if err := s.rmq.Shutdown(); err != nil {
		l.Error(fmt.Errorf("app - Run - rmqServer.Shutdown: %w", err))
	}

	if err := s.nats.Shutdown(); err != nil {
		l.Error(fmt.Errorf("app - Run - natsServer.Shutdown: %w", err))
	}
}

// Run creates objects via constructors.
func Run(cfg *config.Config) {
	run(cfg, logger.New(cfg.Log.Level), dependencies{tracing: tracing.New, postgres: postgres.New, servers: initServers})
}

func run(cfg *config.Config, l logger.Interface, deps dependencies) {
	ctx := context.Background()

	// Tracing
	shutdownTracing, err := deps.tracing(ctx, tracing.Config{
		Enabled:     cfg.Tracing.Enabled,
		ServiceName: cfg.App.Name,
		Version:     cfg.App.Version,
		Endpoint:    cfg.Tracing.OTLPEndpoint,
		Insecure:    cfg.Tracing.OTLPInsecure,
		SampleRate:  cfg.Tracing.SampleRate,
	})
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - tracing.New: %w", err))
	}
	defer func() {
		if err := shutdownTracing(ctx); err != nil {
			l.Error(fmt.Errorf("app - Run - shutdownTracing: %w", err))
		}
	}()

	// Repository
	pg, err := deps.postgres(cfg.PG.URL, postgres.MaxPoolSize(cfg.PG.PoolMax))
	if err != nil {
		l.Fatal(fmt.Errorf("app - Run - postgres.New: %w", err))
	}
	defer pg.Close()

	// JWT
	jwtManager := jwt.New(cfg.JWT.Secret, cfg.JWT.TokenExpiry)

	uc := initUseCases(pg, jwtManager)
	s := deps.servers(cfg, uc, jwtManager, l)
	s.startServers()
	s.waitForShutdown(l)
}
