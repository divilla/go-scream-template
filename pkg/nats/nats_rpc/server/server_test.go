package server

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/pkg/logger"
	natsrpc "github.com/divilla/go-scream-template/pkg/nats/nats_rpc"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type connectionStub struct {
	subscribeErr, publishErr error
	message                  *nats.Msg
	closed                   bool
	subscription             *nats.Subscription
}

func (c *connectionStub) Close() { c.closed = true }
func (c *connectionStub) Subscribe(string, nats.MsgHandler) (*nats.Subscription, error) {
	return c.subscription, c.subscribeErr
}

func (c *connectionStub) PublishMsg(msg *nats.Msg) error {
	c.message = msg

	return c.publishErr
}

type silentLogger struct{ logger.Interface }

func (silentLogger) Info(string, ...any) {}
func (silentLogger) Error(any, ...any)   {}
func TestConstruction(t *testing.T) {
	t.Parallel()

	_, err := New("://invalid", "rpc", nil, silentLogger{})
	require.ErrorContains(t, err, "nats.Connect")

	conn := &connectionStub{}

	server, err := newServer("url", "rpc", nil, silentLogger{}, func(url string) (connection, error) {
		assert.Equal(t, "url", url)

		return conn, nil
	}, Timeout(time.Second))
	require.NoError(t, err)
	assert.Equal(t, time.Second, server.timeout)
	assert.Equal(t, "rpc", server.subject)
	server.Start()
	require.NoError(t, server.Shutdown())
	assert.True(t, conn.closed)
}

func TestSubscriptionFailure(t *testing.T) {
	t.Parallel()

	conn := &connectionStub{subscribeErr: io.ErrClosedPipe}
	server, err := newServer("url", "rpc", nil, silentLogger{}, func(string) (connection, error) { return conn, nil })
	require.NoError(t, err)
	server.Start()

	select {
	case err = <-server.Notify():
		require.ErrorIs(t, err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("missing notification")
	}

	require.ErrorIs(t, server.Shutdown(), io.ErrClosedPipe)
}

func TestUnsubscribeFailure(t *testing.T) {
	t.Parallel()

	conn := &connectionStub{subscription: &nats.Subscription{}}
	server, err := newServer("url", "rpc", nil, silentLogger{}, func(string) (connection, error) { return conn, nil })
	require.NoError(t, err)
	require.NoError(t, server.subscribe())
	require.ErrorIs(t, server.Shutdown(), nats.ErrConnectionClosed)
}

//nolint:gocognit // Keep the scenario inputs and expected outcomes in one test table.
func TestMessages(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"success", "missing", "handler-error", "marshal-error", "publish-error"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			conn := &connectionStub{}
			server, err := newServer("url", "rpc", nil, silentLogger{}, func(string) (connection, error) { return conn, nil })
			require.NoError(t, err)

			server.router = map[string]CallHandler{"handler": func(ctx context.Context, msg *nats.Msg) (any, error) {
				assert.Equal(t, "request", string(msg.Data))
				assert.NotNil(t, ctx)

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

			if kind == "publish-error" {
				conn.publishErr = io.ErrClosedPipe
			}

			server.handleMessage(&nats.Msg{Reply: "reply", Header: nats.Header{"Handler": []string{"handler"}}, Data: []byte("request")})
			require.NotNil(t, conn.message)
			assert.Equal(t, "reply", conn.message.Subject)

			expected := natsrpc.Success
			if kind == "missing" {
				expected = natsrpc.ErrBadHandler.Error()
			}

			if kind == "handler-error" || kind == "marshal-error" {
				expected = natsrpc.ErrInternalServer.Error()
			}

			assert.Equal(t, expected, conn.message.Header.Get("Status"))

			if expected == natsrpc.Success {
				assert.JSONEq(t, `{"result":"ok"}`, string(conn.message.Data))
			}

			require.NoError(t, server.Shutdown())
		})
	}
}
