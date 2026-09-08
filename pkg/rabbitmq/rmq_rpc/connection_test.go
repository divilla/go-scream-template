package rmqrpc

import (
	"encoding/binary"
	"io"
	"math"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixture speaks only the AMQP setup protocol; no external broker is used.
//
//nolint:gocognit // Keep the scenario inputs and expected outcomes in one test table.
func broker(t *testing.T, failAt int) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)

		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()

		if deadlineErr := conn.SetDeadline(time.Now().Add(5 * time.Second)); deadlineErr != nil {
			t.Error(deadlineErr)

			return
		}

		if _, err = io.ReadFull(conn, make([]byte, 8)); err != nil {
			return
		}
		// connection.start: version, empty properties, mechanisms and locales.
		if !sendMethod(conn, 0, 10, 10, []byte{0, 9, 0, 0, 0, 0, 0, 0, 0, 5, 'P', 'L', 'A', 'I', 'N', 0, 0, 0, 5, 'e', 'n', '_', 'U', 'S'}) {
			return
		}

		if _, _, _, ok := readMethod(conn); !ok {
			return
		}

		if !sendMethod(conn, 0, 10, 30, []byte{0, 0, 0, 2, 0, 0, 0, 0}) {
			return
		}

		if _, _, _, ok := readMethod(conn); !ok {
			return
		} // tune-ok

		if _, _, _, ok := readMethod(conn); !ok {
			return
		} // open

		if !sendMethod(conn, 0, 10, 41, []byte{0}) {
			return
		}

		serveSetup(conn, failAt)
	}()

	t.Cleanup(func() { require.NoError(t, listener.Close()); <-done })

	return "amqp://guest:guest@" + listener.Addr().String() + "/"
}

func sendMethod(conn net.Conn, channel, class, method uint16, payload []byte) bool {
	body := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(body, class)
	binary.BigEndian.PutUint16(body[2:], method)
	copy(body[4:], payload)

	if len(body) > math.MaxUint32 {
		return false
	}

	frame := make([]byte, 8+len(body))
	frame[0] = 1
	binary.BigEndian.PutUint16(frame[1:], channel)
	binary.BigEndian.PutUint32(frame[3:], uint32(len(body))) //nolint:gosec // The length is bounded by MaxUint32 above.
	copy(frame[7:], body)
	frame[len(frame)-1] = 206
	_, err := conn.Write(frame)

	return err == nil
}

func readMethod(conn net.Conn) (channel, class, method uint16, ok bool) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, 0, 0, false
	}

	size := binary.BigEndian.Uint32(header[3:])
	if size < 4 || size > 131072 {
		return 0, 0, 0, false
	}

	body := make([]byte, size+1)
	if _, err := io.ReadFull(conn, body); err != nil {
		return 0, 0, 0, false
	}

	return binary.BigEndian.Uint16(header[1:]), binary.BigEndian.Uint16(body), binary.BigEndian.Uint16(body[2:]), true
}

//nolint:gocyclo,cyclop // Keep the scenario inputs and expected outcomes in one test table.
func serveSetup(conn net.Conn, failAt int) {
	for step := 1; ; step++ {
		channel, class, method, ok := readMethod(conn)
		if !ok || step == failAt {
			return
		}

		var payload []byte

		reply := method + 1

		switch class {
		case 10:
			if method == 50 {
				sendMethod(conn, channel, 10, 51, nil)

				return
			}
		case 20:
			payload = []byte{0, 0, 0, 0}
		case 40: // exchange.declare-ok
		case 50:
			if method == 10 {
				payload = []byte{5, 'q', 'u', 'e', 'u', 'e', 0, 0, 0, 0, 0, 0, 0, 0}
			}
		case 60:
			payload = []byte{3, 't', 'a', 'g'}
		}

		if !sendMethod(conn, channel, class, reply, payload) {
			return
		}
	}
}

func TestConnectionSetup(t *testing.T) {
	t.Parallel()

	for _, step := range []int{0, 1, 2, 3, 4, 5} {
		t.Run(strconv.Itoa(step), func(t *testing.T) {
			t.Parallel()
			conn := New("exchange", Config{URL: broker(t, step), Attempts: 1})

			err := conn.AttemptConnect()
			if step != 0 {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.NotNil(t, conn.Connection)
			assert.NotNil(t, conn.Channel)
			assert.NotNil(t, conn.Delivery)
			require.NoError(t, conn.Connection.Close())
		})
	}
}

func TestConnectFailure(t *testing.T) {
	t.Parallel()

	conn := New("exchange", Config{URL: "://invalid", Attempts: 2})
	require.ErrorContains(t, conn.AttemptConnect(), "amqp.Dial")
	conn.Attempts = 0
	require.NoError(t, conn.AttemptConnect())
}
