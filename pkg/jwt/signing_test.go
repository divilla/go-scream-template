package jwt

import (
	"io"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestSigningFailure(t *testing.T) {
	t.Parallel()

	manager := New("test-secret", time.Hour)
	token, err := manager.generateToken("user-1", func(token *jwtlib.Token, key any) (string, error) {
		require.Equal(t, jwtlib.SigningMethodHS256, token.Method)
		require.Equal(t, []byte("test-secret"), key)

		return "", io.ErrUnexpectedEOF
	})
	require.Empty(t, token)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
