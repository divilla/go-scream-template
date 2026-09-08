package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pbgrpc "google.golang.org/grpc"
)

type silentLogger struct{ logger.Interface }

func (silentLogger) Info(string, ...any) {}
func (silentLogger) Error(any, ...any)   {}

func TestConfiguration(t *testing.T) {
	t.Parallel()

	server := New(silentLogger{}, ServerOptions(pbgrpc.MaxRecvMsgSize(1024)))
	assert.Equal(t, ":80", server.address)
	assert.Len(t, server.serverOpts, 1)
	Port("8088")(server)
	assert.Equal(t, ":8088", server.address)
	require.NoError(t, server.Shutdown())
}

func TestStartupFailure(t *testing.T) {
	t.Parallel()

	server := New(silentLogger{}, Port("invalid"))
	server.Start()

	select {
	case err := <-server.Notify():
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("missing startup error")
	}

	_, open := <-server.Notify()
	assert.False(t, open)
	require.Error(t, server.Shutdown())
}

func TestServingLifecycle(t *testing.T) {
	t.Parallel()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	server := New(silentLogger{})
	server.address = address
	server.Start()
	require.Eventually(t, func() bool {
		conn, dialErr := (&net.Dialer{Timeout: time.Millisecond * 50}).DialContext(t.Context(), "tcp", address)
		if dialErr != nil {
			return false
		}

		require.NoError(t, conn.Close())

		return true
	}, time.Second, time.Millisecond)
	require.NoError(t, server.Shutdown())
}

func TestStoppedBeforeServing(t *testing.T) {
	t.Parallel()

	server := New(silentLogger{}, Port("0"))
	server.App.Stop()
	server.Start()

	select {
	case err := <-server.Notify():
		require.ErrorIs(t, err, pbgrpc.ErrServerStopped)
	case <-time.After(time.Second):
		t.Fatal("missing serve error")
	}

	require.ErrorIs(t, server.Shutdown(), pbgrpc.ErrServerStopped)
}

func TestCanceledWorker(t *testing.T) {
	t.Parallel()

	server := New(silentLogger{})
	server.eg.Go(func() error { return context.Canceled })
	require.NoError(t, server.Shutdown())
}
