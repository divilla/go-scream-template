package user

import (
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/divilla/go-scream-template/pkg/postgres"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type queryResult struct {
	contains      []string
	rows          [][]string
	columns       int
	command, code string
}

func database(t *testing.T, results ...queryResult) *postgres.Postgres {
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

		serveDatabase(t, conn, results)
	}()

	pg, err := postgres.New("postgres://postgres:postgres@" + listener.Addr().String() + "/db?sslmode=disable&default_query_exec_mode=simple_protocol")
	require.NoError(t, err)
	t.Cleanup(func() { pg.Close(); require.NoError(t, listener.Close()); <-done })

	return pg
}

func sendResult(backend *pgproto3.Backend, result *queryResult) {
	if result.code != "" {
		backend.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: result.code, Message: "query failed"})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})

		return
	}

	if result.columns > 0 {
		sendRows(backend, result)
	}

	command := result.command
	if command == "" {
		command = "SELECT 1"
	}

	backend.Send(&pgproto3.CommandComplete{CommandTag: []byte(command)})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
}

func sendRows(backend *pgproto3.Backend, result *queryResult) {
	fields := make([]pgproto3.FieldDescription, result.columns)
	for i := range fields {
		fields[i] = pgproto3.FieldDescription{Name: []byte(strconv.Itoa(i)), DataTypeOID: 25, DataTypeSize: -1}
	}

	if result.columns == 1 {
		fields[0].DataTypeOID = 23
	}

	if result.columns == 6 || result.columns == 7 {
		fields[result.columns-1].DataTypeOID = 1184
		fields[result.columns-2].DataTypeOID = 1184
	}

	backend.Send(&pgproto3.RowDescription{Fields: fields})

	for _, row := range result.rows {
		values := make([][]byte, len(row))
		for i, value := range row {
			values[i] = []byte(value)
		}

		backend.Send(&pgproto3.DataRow{Values: values})
	}
}

type brokenPlaceholder struct{ calls, failAt int }

func (p *brokenPlaceholder) ReplacePlaceholders(query string) (string, error) {
	p.calls++
	if p.calls == p.failAt {
		return "", io.ErrUnexpectedEOF
	}

	return squirrel.Dollar.ReplacePlaceholders(query)
}

func userRow() []string {
	return []string{"user-1", "alice", "alice@example.com", "hash", "2026-01-01 00:00:00+00", "2026-01-02 00:00:00+00"}
}

func TestStore(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"", "23505", "XX000"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			pg := database(t, queryResult{contains: []string{"INSERT INTO users", "'user-1'", "'alice'", "'hash'"}, command: "INSERT 0 1", code: code})
			err := New(pg).Store(t.Context(), &entity.User{ID: "user-1", Username: "alice", Email: "alice@example.com", PasswordHash: "hash"})

			switch code {
			case "":
				require.NoError(t, err)
			case "23505":
				require.ErrorIs(t, err, entity.ErrUserAlreadyExists)
			default:
				require.ErrorContains(t, err, "r.Pool.Exec")
			}
		})
	}
}

//nolint:gocyclo,cyclop // Keep the scenario inputs and expected outcomes in one test table.
func TestReadUser(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"id", "email", "missing", "database", "scan"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			result := queryResult{contains: []string{"SELECT id, username, email, password_hash, created_at, updated_at FROM users"}, columns: 6, rows: [][]string{userRow()}}

			switch kind {
			case "id":
				result.contains = append(result.contains, "id = 'user-1'")
			case "email":
				result.contains = append(result.contains, "email = 'alice@example.com'")
			case "missing":
				result.rows = nil
			case "database":
				result.code = "XX000"
			case "scan":
				result.rows[0][4] = "invalid-time"
			}

			repo := New(database(t, result))

			var user entity.User

			var err error
			if kind == "email" {
				user, err = repo.GetByEmail(t.Context(), "alice@example.com")
			} else {
				user, err = repo.GetByID(t.Context(), "user-1")
			}

			if kind == "missing" {
				require.ErrorIs(t, err, entity.ErrUserNotFound)

				return
			}

			if kind == "database" || kind == "scan" {
				require.ErrorContains(t, err, "r.Pool.QueryRow")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "user-1", user.ID)
			assert.Equal(t, "alice@example.com", user.Email)
			assert.Equal(t, 2026, user.CreatedAt.Year())
		})
	}
}

func TestBuilderErrors(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"store", "read"} {
		pg := &postgres.Postgres{Builder: squirrel.StatementBuilder.PlaceholderFormat(&brokenPlaceholder{failAt: 1})}
		repo := New(pg)

		var err error
		if operation == "store" {
			err = repo.Store(t.Context(), &entity.User{})
		} else {
			_, err = repo.GetByID(t.Context(), "id")
		}

		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	}
}

func serveDatabase(t *testing.T, conn net.Conn, results []queryResult) {
	t.Helper()

	backend := pgproto3.NewBackend(conn, conn)
	if _, receiveErr := backend.ReceiveStartupMessage(); receiveErr != nil {
		return
	}

	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})

	if flushErr := backend.Flush(); flushErr != nil {
		return
	}

	for _, result := range results {
		message, receiveErr := backend.Receive()
		if receiveErr != nil {
			t.Error(receiveErr)

			return
		}

		query, ok := message.(*pgproto3.Query)
		if !assert.True(t, ok, "expected simple query, got %T", message) {
			return
		}

		for _, text := range result.contains {
			assert.Contains(t, strings.Join(strings.Fields(query.String), " "), text)
		}

		sendResult(backend, &result)

		if flushErr := backend.Flush(); flushErr != nil {
			t.Error(flushErr)

			return
		}
	}

	_, receiveErr := backend.Receive() // Wait for the pool to terminate its connection.
	if receiveErr != nil && !errors.Is(receiveErr, io.EOF) {
		t.Error(receiveErr)
	}
}
