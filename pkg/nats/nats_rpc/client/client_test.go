package client

import (
	"context"
	"io"
	"testing"
	"time"

	natsrpc "github.com/divilla/go-scream-template/pkg/nats/nats_rpc"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type connectionStub struct {
	request  *nats.Msg
	response *nats.Msg
	err      error
	closed   bool
	timeout  time.Duration
}

func (c *connectionStub) Close() { c.closed = true }
func (c *connectionStub) RequestMsg(msg *nats.Msg, timeout time.Duration) (*nats.Msg, error) {
	c.request = msg
	c.timeout = timeout

	return c.response, c.err
}

func TestConstruction(t *testing.T) {
	t.Parallel()

	_, err := New("://invalid", "rpc")
	require.ErrorContains(t, err, "nats.Connect")

	conn := &connectionStub{}
	client, err := newClient("url", "rpc", func(url string) (connection, error) {
		assert.Equal(t, "url", url)

		return conn, nil
	}, Timeout(time.Second))
	require.NoError(t, err)
	assert.Equal(t, time.Second, client.timeout)
	assert.Equal(t, "rpc", client.subject)
	require.NoError(t, client.Shutdown())
	assert.True(t, conn.closed)
}

func TestRemoteCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, status, body    string
		transportErr, wantErr error
	}{
		{"success", natsrpc.Success, `{"value":"result"}`, nil, nil},
		{"bad-handler", natsrpc.ErrBadHandler.Error(), "", nil, natsrpc.ErrBadHandler},
		{"internal", natsrpc.ErrInternalServer.Error(), "", nil, natsrpc.ErrInternalServer},
		{"timeout", "", "", context.DeadlineExceeded, natsrpc.ErrTimeout},
		{"connection", "", "", io.ErrClosedPipe, io.ErrClosedPipe},
		{"decode", natsrpc.Success, "{", nil, nil},
		{"unknown", "unknown", "", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conn := &connectionStub{response: &nats.Msg{Header: nats.Header{"Status": []string{tc.status}}, Data: []byte(tc.body)}, err: tc.transportErr}
			client, err := newClient("url", "rpc", func(string) (connection, error) { return conn, nil })
			require.NoError(t, err)

			var got map[string]string

			err = client.RemoteCall("handler", map[string]string{"input": "value"}, &got)

			assert.Equal(t, "rpc", conn.request.Subject)
			assert.Equal(t, "handler", conn.request.Header.Get("Handler"))
			assert.JSONEq(t, `{"input":"value"}`, string(conn.request.Data))
			assert.Equal(t, 2*time.Second, conn.timeout)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)

				return
			}

			if tc.name == "decode" {
				require.ErrorContains(t, err, "json.Unmarshal")

				return
			}

			require.NoError(t, err)

			if tc.name == "success" {
				assert.Equal(t, "result", got["value"])
			}
		})
	}
}

func TestRequestBodies(t *testing.T) {
	t.Parallel()

	conn := &connectionStub{response: &nats.Msg{}}
	client := &Client{connection: conn}
	err := client.RemoteCall("handler", make(chan int), nil)
	require.Error(t, err)
	assert.Nil(t, conn.request)
	require.NoError(t, client.RemoteCall("handler", nil, nil))
	assert.Empty(t, conn.request.Data)
}
