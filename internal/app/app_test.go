//nolint:errcheck // Server fixtures are constructed as the concrete types asserted below.
package app

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/config"
	"github.com/divilla/go-scream-template/pkg/grpcserver"
	"github.com/divilla/go-scream-template/pkg/httpserver"
	"github.com/divilla/go-scream-template/pkg/jwt"
	"github.com/divilla/go-scream-template/pkg/logger"
	natsserver "github.com/divilla/go-scream-template/pkg/nats/nats_rpc/server"
	"github.com/divilla/go-scream-template/pkg/postgres"
	rmqserver "github.com/divilla/go-scream-template/pkg/rabbitmq/rmq_rpc/server"
	"github.com/divilla/go-scream-template/pkg/tracing"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serverStub struct {
	notifications    chan error
	started, stopped bool
	err              error
}

func (s *serverStub) Start()               { s.started = true }
func (s *serverStub) Notify() <-chan error { return s.notifications }

func (s *serverStub) Shutdown() error {
	s.stopped = true

	return s.err
}

type logRecorder struct {
	logger.Interface
	errors []error
}

func (*logRecorder) Zerolog() *zerolog.Logger { return new(zerolog.Nop()) }

func (*logRecorder) Info(string, ...any) {}
func (l *logRecorder) Error(err any, _ ...any) {
	typed, ok := err.(error)
	if !ok {
		panic("expected logged error")
	}

	l.errors = append(l.errors, typed)
}
func (*logRecorder) Fatal(err any, _ ...any) { panic(err) }
func fakeServers() servers {
	return servers{http: &serverStub{notifications: make(chan error, 1)}, grpc: &serverStub{notifications: make(chan error, 1)}, rmq: &serverStub{notifications: make(chan error, 1)}, nats: &serverStub{notifications: make(chan error, 1)}}
}

func TestShutdownSources(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"http", "grpc", "rmq", "nats", "signal"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			stack := fakeServers()
			all := map[string]server{"http": stack.http, "grpc": stack.grpc, "rmq": stack.rmq, "nats": stack.nats}

			signals := make(chan os.Signal, 1)
			if source == "signal" {
				signals <- os.Interrupt
			} else {
				all[source].(*serverStub).notifications <- io.ErrClosedPipe
			}

			stack.startServers()

			log := &logRecorder{}
			stack.waitForShutdownSignal(log, signals)

			for _, server := range all {
				stub := server.(*serverStub)
				assert.True(t, stub.started)
				assert.True(t, stub.stopped)
			}

			if source != "signal" {
				require.Len(t, log.errors, 1)
				assert.ErrorIs(t, log.errors[0], io.ErrClosedPipe)
			}
		})
	}
}

func TestShutdownErrors(t *testing.T) {
	t.Parallel()

	stack := fakeServers()
	for _, server := range []server{stack.http, stack.grpc, stack.rmq, stack.nats} {
		server.(*serverStub).err = io.ErrUnexpectedEOF
	}

	log := &logRecorder{}
	stack.shutdownServers(log)
	require.Len(t, log.errors, 4)

	for _, err := range log.errors {
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}

func TestServerWiring(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"", "rmq", "nats"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{}
			cfg.HTTP.Port = "0"
			cfg.GRPC.Port = "0"
			manager := jwt.New("test", time.Hour)
			uc := initUseCases(&postgres.Postgres{}, manager)
			require.NotNil(t, uc.user)
			require.NotNil(t, uc.task)
			require.NotNil(t, uc.translation)

			constructors := serverConstructors{
				rmq: func(_, _ string, routes map[string]rmqserver.CallHandler, _ logger.Interface, _ ...rmqserver.Option) (*rmqserver.Server, error) {
					assert.Len(t, routes, 10)

					if failure == "rmq" {
						return nil, io.ErrClosedPipe
					}

					return &rmqserver.Server{}, nil
				},
				nats: func(_, _ string, routes map[string]natsserver.CallHandler, _ logger.Interface, _ ...natsserver.Option) (*natsserver.Server, error) {
					assert.Len(t, routes, 10)

					if failure == "nats" {
						return nil, io.ErrClosedPipe
					}

					return &natsserver.Server{}, nil
				},
			}

			if failure != "" {
				require.Panics(t, func() { initServersWith(cfg, uc, manager, &logRecorder{}, constructors) })

				return
			}

			stack := initServersWith(cfg, uc, manager, &logRecorder{}, constructors)
			http := stack.http.(*httpserver.Server)
			grpc := stack.grpc.(*grpcserver.Server)

			assert.NotNil(t, http.App)
			assert.Contains(t, grpc.App.GetServiceInfo(), "grpc.v1.AuthService")
			require.NoError(t, http.Shutdown())
			require.NoError(t, grpc.Shutdown())
		})
	}
}

//nolint:gocognit // Keep the scenario inputs and expected outcomes in one test table.
func TestRun(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"", "tracing", "postgres", "flush"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{}
			cfg.PG.URL = "postgres://localhost/db"
			cfg.PG.PoolMax = 1
			cfg.JWT.Secret = "test"
			flushed := false

			stack := fakeServers()
			stack.http.(*serverStub).notifications <- io.ErrClosedPipe

			deps := dependencies{
				tracing: func(_ context.Context, _ tracing.Config) (tracing.ShutdownFunc, error) {
					if failure == "tracing" {
						return nil, io.ErrUnexpectedEOF
					}

					return func(context.Context) error {
						flushed = true

						if failure == "flush" {
							return io.ErrUnexpectedEOF
						}

						return nil
					}, nil
				},
				postgres: func(url string, options ...postgres.Option) (*postgres.Postgres, error) {
					if failure == "postgres" {
						return nil, io.ErrUnexpectedEOF
					}

					return postgres.New(url, options...)
				},
				servers: func(_ *config.Config, uc useCases, manager *jwt.Manager, _ logger.Interface) servers {
					assert.NotNil(t, uc.user)
					assert.NotNil(t, manager)

					return stack
				},
			}
			log := &logRecorder{}

			if failure == "tracing" || failure == "postgres" {
				require.Panics(t, func() { run(cfg, log, deps) })
				assert.Equal(t, failure == "postgres", flushed)

				return
			}

			run(cfg, log, deps)
			assert.True(t, flushed)

			if failure == "flush" {
				require.Len(t, log.errors, 2)
			} else {
				require.Len(t, log.errors, 1)
			}
		})
	}
}
