package client

import (
	"context"
	"io"
	"testing"
	"time"

	rmqrpc "github.com/divilla/go-scream-template/pkg/rabbitmq/rmq_rpc"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

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
func testClient(t *testing.T) *Client {
	t.Helper()
	group, ctx := errgroup.WithContext(t.Context())

	return &Client{ctx: ctx, eg: group, conn: rmqrpc.New("client", rmqrpc.Config{}), serverExchange: "server", stop: make(chan struct{}), notify: make(chan error, 1), calls: map[string]*pendingCall{}, timeout: time.Second, closeConnection: func() error { return nil }}
}

func TestConstruction(t *testing.T) {
	t.Parallel()

	client, err := New("://invalid", "server", "client", ConnAttempts(1), ConnWaitTime(0))
	require.ErrorContains(t, err, "AttemptConnect")
	assert.Nil(t, client)
	client, err = New("url", "server", "client", ConnAttempts(0), ConnWaitTime(0), Timeout(time.Second), func(c *Client) { c.closeConnection = func() error { return nil } })
	require.NoError(t, err)
	assert.Equal(t, time.Second, client.timeout)
	require.NoError(t, client.Shutdown())
}

//nolint:gocognit,cyclop,funlen // Keep the scenario inputs and expected outcomes in one test table.
func TestRemoteReplies(t *testing.T) {
	t.Parallel()

	for _, reply := range []string{rmqrpc.Success, rmqrpc.ErrBadHandler.Error(), rmqrpc.ErrInternalServer.Error(), "malformed", "unknown"} {
		t.Run(reply, func(t *testing.T) {
			t.Parallel()
			client := testClient(t)
			published := make(chan amqp.Publishing, 1)
			client.publishMessage = func(exchange string, message amqp.Publishing) error {
				assert.Equal(t, "server", exchange)

				published <- message

				return nil
			}

			done := make(chan struct{})
			go func() {
				defer close(done)

				message := <-published
				assert.Equal(t, "handler", message.Type)
				assert.Equal(t, "client", message.ReplyTo)
				assert.Equal(t, "application/json", message.ContentType)
				assert.JSONEq(t, `{"input":"value"}`, string(message.Body))

				var call *pendingCall

				for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
					if value, ok := client.getCall(message.CorrelationId); ok {
						call = value

						break
					}

					time.Sleep(time.Millisecond)
				}

				if !assert.NotNil(t, call) {
					return
				}

				kind, body := reply, []byte(`{"result":"ok"}`)
				if reply == "malformed" {
					kind = rmqrpc.Success
					body = []byte("{")
				}

				client.serveCall(&amqp.Delivery{CorrelationId: message.CorrelationId, Type: kind, Body: body, Acknowledger: &acknowledger{}})
			}()

			var got map[string]string

			err := client.RemoteCall("handler", map[string]string{"input": "value"}, &got)

			<-done

			switch reply {
			case rmqrpc.ErrBadHandler.Error():
				require.ErrorIs(t, err, rmqrpc.ErrBadHandler)
			case rmqrpc.ErrInternalServer.Error():
				require.ErrorIs(t, err, rmqrpc.ErrInternalServer)
			case "malformed":
				require.ErrorContains(t, err, "json.Unmarshal")
			default:
				require.NoError(t, err)
			}

			if reply == rmqrpc.Success {
				assert.Equal(t, "ok", got["result"])
			}

			assert.Empty(t, client.calls)
		})
	}
}

func TestPublishFailures(t *testing.T) {
	t.Parallel()
	client := testClient(t)
	require.Error(t, client.RemoteCall("handler", make(chan int), nil))
	client.publishMessage = func(_ string, msg amqp.Publishing) error {
		assert.Empty(t, msg.Body)

		return io.ErrClosedPipe
	}
	require.ErrorIs(t, client.RemoteCall("handler", nil, nil), io.ErrClosedPipe)
	client.publishMessage = func(string, amqp.Publishing) error { return nil }
	client.timeout = 0
	require.ErrorIs(t, client.RemoteCall("handler", nil, nil), rmqrpc.ErrTimeout)
}

func TestWaitFailures(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"canceled", "stopped", "notification"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			for _, pending := range []bool{false, true} {
				client := testClient(t)

				var expected error

				switch kind {
				case "canceled":
					ctx, cancel := context.WithCancel(t.Context())
					cancel()

					client.ctx = ctx
					expected = context.Canceled
				case "stopped":
					close(client.stop)

					expected = ErrConnectionClosed
				case "notification":
					client.notify <- io.ErrClosedPipe

					expected = io.ErrClosedPipe
				}

				var err error
				if pending {
					err = client.remoteCallWait(&pendingCall{done: make(chan struct{})})
				} else {
					err = client.RemoteCall("handler", nil, nil)
				}

				require.ErrorIs(t, err, expected)
			}
		})
	}
}

func TestDeliveryLifecycle(t *testing.T) {
	t.Parallel()
	client := testClient(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	client.ctx = ctx
	require.ErrorIs(t, client.handleMessages(), context.Canceled)
	client = testClient(t)
	ack := &acknowledger{err: io.ErrClosedPipe}
	client.serveCall(&amqp.Delivery{CorrelationId: "unknown", Acknowledger: ack})
	assert.Equal(t, 1, ack.calls)

	deliveries := make(chan amqp.Delivery)
	close(deliveries)
	client.conn.Delivery = deliveries
	client.conn.URL = "://invalid"
	client.conn.Attempts = 1
	client.start()

	select {
	case err := <-client.notify:
		require.ErrorContains(t, err, "AttemptConnect")
	case <-time.After(time.Second):
		t.Fatal("missing worker failure")
	}

	client.closeConnection = func() error { return io.ErrClosedPipe }
	err := client.Shutdown()
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.ErrorContains(t, err, "AttemptConnect")
}

func TestDeliveryLoop(t *testing.T) {
	t.Parallel()
	client := testClient(t)
	deliveries := make(chan amqp.Delivery, 1)
	client.conn.Delivery = deliveries
	call := &pendingCall{done: make(chan struct{})}
	client.addCall("id", call)

	deliveries <- amqp.Delivery{CorrelationId: "id", Type: rmqrpc.Success, Acknowledger: &acknowledger{}}

	done := make(chan struct{})
	go func() { <-call.done; close(client.stop); close(done) }()

	require.NoError(t, client.handleMessages())
	<-done
	assert.Equal(t, rmqrpc.Success, call.status)

	client.conn.Attempts = 0
	require.NoError(t, client.reconnect())
}
