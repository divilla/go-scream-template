package jwt_test

import (
	"testing"
	"time"

	"github.com/divilla/go-scream-template/pkg/jwt"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWT_GenerateAndParse(t *testing.T) {
	t.Parallel()

	j := jwt.New("test-secret", time.Hour)

	token, err := j.GenerateToken("user-123")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	userID, err := j.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", userID)
}

func TestJWT_ParseToken_Invalid(t *testing.T) {
	t.Parallel()

	j := jwt.New("test-secret", time.Hour)

	_, err := j.ParseToken("invalid-token")
	require.Error(t, err)
}

func TestJWT_ParseToken_WrongSecret(t *testing.T) {
	t.Parallel()

	j1 := jwt.New("secret-1", time.Hour)
	j2 := jwt.New("secret-2", time.Hour)

	token, err := j1.GenerateToken("user-123")
	require.NoError(t, err)

	_, err = j2.ParseToken(token)
	require.Error(t, err)
}

func TestJWT_ParseToken_Expired(t *testing.T) {
	t.Parallel()

	j := jwt.New("test-secret", -time.Hour)

	token, err := j.GenerateToken("user-123")
	require.NoError(t, err)

	_, err = j.ParseToken(token)
	require.Error(t, err)
}

func TestTokenClaimsAndAlgorithm(t *testing.T) {
	t.Parallel()

	manager := jwt.New("test-secret", time.Hour)
	wrongSubject, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, jwtlib.MapClaims{"sub": 123}).SignedString([]byte("test-secret"))
	require.NoError(t, err)
	_, err = manager.ParseToken(wrongSubject)
	require.ErrorContains(t, err, "GetSubject")
	unsigned, err := jwtlib.NewWithClaims(jwtlib.SigningMethodNone, jwtlib.MapClaims{"sub": "user"}).SignedString(jwtlib.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = manager.ParseToken(unsigned)
	require.ErrorIs(t, err, jwt.ErrUnexpectedSigningMethod)
}
