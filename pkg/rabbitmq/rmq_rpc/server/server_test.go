package server

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/pkg/logger"
	rmqrpc "github.com/divilla/go-scream-template/pkg/rabbitmq/rmq_rpc"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type silentLogger struct{ logger.Interface }

func (silentLogger) Info(string, ...any) {}
func (silentLogger) Error(any, ...any)   {}

type acknowledger struct {
	calls int
	err   error
}

func (a *acknowledger) Ack(uint64, bool) error {
	a.calls++

	return a.err
}
func (*acknowledger) Nack(uint64, bool, bool) error { return nil }
func (*acknowledger) Reject(uint64, bool) error     { return nil }
func testServer(t *testing.T) *Server {
	t.Helper()

	server, err := New("url", "rpc", nil, silentLogger{}, ConnAttempts(0), ConnWaitTime(0), Timeout(time.Second), func(s *Server) {
		s.closeConnection = func() error { return nil }
		s.publishMessage = func(string, amqp.Publishing) error { return nil }
	})
	require.NoError(t, err)
	assert.Equal(t, time.Second, server.timeout)

	return server
}

func TestConstructionFailure(t *testing.T) {
	t.Parallel()

	server, err := New("://invalid", "rpc", nil, silentLogger{}, ConnAttempts(1), ConnWaitTime(0))
	require.ErrorContains(t, err, "AttemptConnect")
	assert.Nil(t, server)
}

func TestLifecycle(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	server.Start()
	require.NoError(t, server.Shutdown())
}

func TestWorkerFailure(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	deliveries := make(chan amqp.Delivery)
	close(deliveries)
	server.conn.Delivery = deliveries
	server.conn.URL = "://invalid"
	server.conn.Attempts = 1
	server.Start()

	select {
	case err := <-server.Notify():
		require.ErrorContains(t, err, "AttemptConnect")
	case <-time.After(time.Second):
		t.Fatal("missing failure notification")
	}

	server.closeConnection = func() error { return io.ErrClosedPipe }
	err := server.Shutdown()
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.ErrorContains(t, err, "AttemptConnect")
}

func TestCanceledWorker(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	server.ctx = ctx
	require.ErrorIs(t, server.handleMessages(), context.Canceled)
	require.NoError(t, server.Shutdown())
}

//nolint:gocognit,funlen // Keep the scenario inputs and expected outcomes in one test table.
func TestMessages(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"success", "missing", "handler-error", "marshal-error", "publish-error"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			server := testServer(t)

			var published amqp.Publishing

			server.publishMessage = func(exchange string, msg amqp.Publishing) error {
				assert.Equal(t, "reply", exchange)

				published = msg

				if kind == "publish-error" {
					return io.ErrClosedPipe
				}

				return nil
			}

			server.router = map[string]CallHandler{"handler": func(ctx context.Context, d *amqp.Delivery) (any, error) {
				assert.NotNil(t, ctx)
				assert.Equal(t, "request", string(d.Body))

				if kind == "handler-error" {
					return nil, io.ErrUnexpectedEOF
				}

				if kind == "marshal-error" {
					return make(chan int), nil
				}

				return map[string]string{"result": "ok"}, nil
			}}
			if kind == "missing" {
				server.router = nil
			}

			ack := &acknowledger{}
			if kind == "publish-error" {
				ack.err = io.ErrClosedPipe
			}

			server.serveCall(&amqp.Delivery{Type: "handler", ReplyTo: "reply", CorrelationId: "id", Body: []byte("request"), Acknowledger: ack})
			assert.Equal(t, 1, ack.calls)
			assert.Equal(t, "id", published.CorrelationId)

			expected := rmqrpc.Success
			if kind == "missing" {
				expected = rmqrpc.ErrBadHandler.Error()
			}

			if kind == "handler-error" {
				expected = rmqrpc.ErrInternalServer.Error()
			}

			assert.Equal(t, expected, published.Type)

			if kind == "success" {
				assert.JSONEq(t, `{"result":"ok"}`, string(published.Body))
			}

			require.NoError(t, server.Shutdown())
		})
	}
}

func TestDeliveryLoop(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	deliveries := make(chan amqp.Delivery, 1)

	server.conn.Delivery = deliveries
	deliveries <- amqp.Delivery{Acknowledger: &acknowledger{}}

	server.publishMessage = func(string, amqp.Publishing) error {
		close(server.stop)

		return nil
	}
	require.NoError(t, server.handleMessages())
}

func TestReconnectWithoutAttempts(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	require.NoError(t, server.reconnect())
	require.NoError(t, server.Shutdown())
}
