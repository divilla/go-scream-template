package httpserver

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Cancellation is normal during shutdown, but must not hide a shutdown failure (AC 3).
func TestShutdownCancellation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		shutdownErr error
	}{
		{name: "cancellation succeeds"},
		{name: "shutdown deadline is preserved", shutdownErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := New(silentLogger{})
			t.Cleanup(s.stop)

			s.shutdownErr = tc.shutdownErr
			s.eg.Go(func() error {
				<-s.ctx.Done()

				return fmt.Errorf("listener stopped: %w", s.ctx.Err())
			})

			err := s.Shutdown()
			if tc.shutdownErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tc.shutdownErr)
			require.NotErrorIs(t, err, context.Canceled)
		})
	}
}
