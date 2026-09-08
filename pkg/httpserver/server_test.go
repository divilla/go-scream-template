package httpserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/divilla/go-scream-template/pkg/logger"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type silentLogger struct{ logger.Interface }

func (silentLogger) Info(string, ...any) {}
func (silentLogger) Error(any, ...any)   {}

// TestConfiguration verifies server options and defaults (AC 3).
func TestConfiguration(t *testing.T) {
	t.Parallel()

	s := New(silentLogger{})

	t.Cleanup(func() { require.NoError(t, s.Shutdown()) })
	assert.Equal(t, ":80", s.address)

	configured := &http.Server{ReadHeaderTimeout: time.Second}
	require.NoError(t, s.configureHTTP(configured))
	assert.Equal(t, 5*time.Second, configured.ReadTimeout)
	assert.Equal(t, 5*time.Second, configured.WriteTimeout)
	assert.Equal(t, 3*time.Second, s.shutdownTimeout)
	Port("8088")(s)
	ReadTimeout(time.Second)(s)
	WriteTimeout(2 * time.Second)(s)
	ShutdownTimeout(4 * time.Second)(s)
	require.NoError(t, s.configureHTTP(configured))
	assert.Equal(t, ":8088", s.address)
	assert.Equal(t, time.Second, configured.ReadTimeout)
	assert.Equal(t, 2*time.Second, configured.WriteTimeout)
	assert.Equal(t, 4*time.Second, s.shutdownTimeout)

	rec := httptest.NewRecorder()

	s.App.GET("/", func(c *echo.Context) error { return c.String(http.StatusOK, "echo") })
	s.App.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	assert.Equal(t, "echo", rec.Body.String())
}

func TestStartupFailure(t *testing.T) {
	t.Parallel()

	s := New(silentLogger{}, Port("invalid-port"))
	s.Start()

	select {
	case err := <-s.Notify():
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("startup failure was not reported")
	}

	require.Error(t, s.Shutdown())

	_, open := <-s.Notify()
	assert.False(t, open)
}

func TestBodyLimit(t *testing.T) {
	t.Parallel()

	for _, size := range []int{(4 << 20) - 1, 4 << 20, (4 << 20) + 1, 5 << 20} {
		for _, chunked := range []bool{false, true} {
			s := New(silentLogger{})

			t.Cleanup(func() { require.NoError(t, s.Shutdown()) })

			bound := false

			s.App.POST("/", func(c *echo.Context) error {
				var body map[string]string
				if err := echo.BindBody(c, &body); err != nil {
					return echo.ErrStatusRequestEntityTooLarge
				}

				bound = true

				return c.NoContent(http.StatusNoContent)
			})

			body := `{"data":"` + strings.Repeat("x", size-len(`{"data":""}`)) + `"}`
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			if chunked {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}

			rec := httptest.NewRecorder()
			s.App.ServeHTTP(rec, req)

			if size > 4<<20 {
				assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
				assert.False(t, bound)
			} else {
				assert.Equal(t, http.StatusNoContent, rec.Code)
				assert.True(t, bound)
			}
		}
	}
}

func TestBodyReadError(t *testing.T) {
	t.Parallel()

	s := New(silentLogger{})

	t.Cleanup(func() { require.NoError(t, s.Shutdown()) })
	s.App.POST("/", func(*echo.Context) error {
		t.Fatal("handler must not run after a body read error")

		return nil
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", iotest.ErrReader(io.ErrUnexpectedEOF))
	rec := httptest.NewRecorder()
	s.App.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"message":"Bad Request"}`, rec.Body.String())
}

func reserveAddress(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	return address
}

func waitForListener(t *testing.T, address string) {
	t.Helper()
	require.Eventually(t, func() bool {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", address)
		if err != nil {
			return false
		}

		require.NoError(t, conn.Close())

		return true
	}, time.Second, time.Millisecond)
}

func TestGracefulShutdown(t *testing.T) {
	t.Parallel()

	s := New(silentLogger{}, ShutdownTimeout(time.Second))

	s.address = reserveAddress(t)

	entered, release := make(chan struct{}), make(chan struct{})

	s.App.GET("/", func(c *echo.Context) error {
		close(entered)
		<-release

		return c.String(http.StatusOK, "finished")
	})
	s.Start()
	s.Start()
	waitForListener(t, s.address)

	response := make(chan error, 1)

	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+s.address, http.NoBody)
		if err != nil {
			response <- err

			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			err = resp.Body.Close()
		}

		response <- err
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler was not called")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- s.Shutdown() }()

	select {
	case <-stopped:
		t.Fatal("shutdown did not wait for the active request")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-response)
	require.NoError(t, <-stopped)

	_, open := <-s.Notify()
	assert.False(t, open)
	require.NoError(t, s.Shutdown())
}

func TestShutdownDeadline(t *testing.T) {
	t.Parallel()

	for _, timeout := range []time.Duration{time.Millisecond, 0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			t.Parallel()
			testShutdownDeadline(t, timeout)
		})
	}
}

func testShutdownDeadline(t *testing.T, timeout time.Duration) {
	t.Helper()

	s := New(silentLogger{}, ShutdownTimeout(timeout))

	s.address = reserveAddress(t)

	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)

	s.App.GET("/", func(c *echo.Context) error {
		close(entered)
		<-release

		return c.NoContent(http.StatusOK)
	})
	s.Start()
	waitForListener(t, s.address)

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", s.address)
	require.NoError(t, err)

	defer conn.Close()

	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
	require.NoError(t, err)

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler was not called")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- s.Shutdown() }()

	select {
	case err := <-stopped:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		require.NoError(t, s.httpServer.Close())
		<-stopped
		t.Fatal("shutdown did not honor its deadline")
	}

	_, open := <-s.Notify()
	assert.False(t, open)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", s.address)
	require.NoError(t, err, "shutdown must release the listener")
	require.NoError(t, listener.Close())
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))

	buffer := make([]byte, 1)

	_, err = conn.Read(buffer)
	require.ErrorIs(t, err, io.EOF)
}
