package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolConfiguration(t *testing.T) {
	t.Parallel()

	pg, err := New("postgres://postgres:postgres@localhost:15432/db", MaxPoolSize(3), ConnAttempts(2), ConnTimeout(time.Millisecond))
	require.NoError(t, err)

	defer pg.Close()

	assert.Equal(t, int32(3), pg.Pool.Config().MaxConns)
	assert.Equal(t, 2, pg.connAttempts)
	assert.Equal(t, time.Millisecond, pg.connTimeout)
	assert.NotNil(t, pg.Pool.Config().ConnConfig.Tracer)
	query, args, err := pg.Builder.Select("id").From("users").Where("id = ?", "user-1").ToSql()
	require.NoError(t, err)
	assert.Equal(t, "SELECT id FROM users WHERE id = $1", query)
	assert.Equal(t, []any{"user-1"}, args)
}

func TestPoolErrors(t *testing.T) {
	t.Parallel()

	pg, err := New("://invalid")
	require.ErrorContains(t, err, "ParseConfig")
	assert.Nil(t, pg)
	pg, err = New("postgres://localhost/db", MaxPoolSize(0), ConnAttempts(2), ConnTimeout(0))
	require.ErrorContains(t, err, "connAttempts == 0")
	assert.Nil(t, pg)
	(&Postgres{}).Close()
}
