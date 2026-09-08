package task

import (
	"errors"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/divilla/go-scream-template/internal/entity"
	repotypes "github.com/divilla/go-scream-template/internal/repo"
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

func taskRow() []string {
	return []string{"task-1", "user-1", "title", "details", "todo", "2026-01-01 00:00:00+00", "2026-01-02 00:00:00+00"}
}

//nolint:gocognit,gocyclo,cyclop // Keep the scenario inputs and expected outcomes in one test table.
func TestMutations(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"store", "update", "delete"} {
		for _, outcome := range []string{"success", "empty", "error"} {
			t.Run(operation+outcome, func(t *testing.T) {
				t.Parallel()

				result := queryResult{contains: []string{"tasks", "'task-1'", "'user-1'"}, command: "UPDATE 1"}
				if outcome == "empty" {
					result.command = "UPDATE 0"
				}

				if outcome == "error" {
					result.code = "XX000"
				}

				repo := New(database(t, result))
				task := &entity.Task{ID: "task-1", UserID: "user-1", Title: "title", Status: entity.TaskStatusTodo}

				var err error

				switch operation {
				case "store":
					err = repo.Store(t.Context(), task)
				case "update":
					err = repo.Update(t.Context(), task)
				case "delete":
					err = repo.Delete(t.Context(), "user-1", "task-1")
				}

				if outcome == "error" {
					require.ErrorContains(t, err, "r.Pool.Exec")

					return
				}

				if outcome == "empty" && operation != "store" {
					require.ErrorIs(t, err, entity.ErrTaskNotFound)

					return
				}

				require.NoError(t, err)
			})
		}
	}
}

func TestReadTask(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"success", "missing", "forbidden", "query-error", "scan-error"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			result := queryResult{contains: []string{"FROM tasks WHERE id = 'task-1'"}, columns: 7, rows: [][]string{taskRow()}}

			switch kind {
			case "missing":
				result.rows = nil
			case "forbidden":
				result.rows[0][1] = "other-user"
			case "query-error":
				result.code = "XX000"
			case "scan-error":
				result.rows[0][5] = "bad-time"
			}

			task, err := New(database(t, result)).GetByID(t.Context(), "user-1", "task-1")

			switch kind {
			case "missing":
				require.ErrorIs(t, err, entity.ErrTaskNotFound)
			case "forbidden":
				require.ErrorIs(t, err, entity.ErrTaskForbidden)
			case "query-error", "scan-error":
				require.ErrorContains(t, err, "r.Pool.QueryRow")
			default:
				require.NoError(t, err)
				assert.Equal(t, "task-1", task.ID)
				assert.Equal(t, "user-1", task.UserID)
				assert.Equal(t, entity.TaskStatusTodo, task.Status)
			}
		})
	}
}

//nolint:gocognit // Keep the scenario inputs and expected outcomes in one test table.
func TestList(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"success", "filtered", "count-error", "query-error", "scan-error"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			count := queryResult{contains: []string{"COUNT(*) FROM tasks WHERE user_id = 'user-1'"}, columns: 1, rows: [][]string{{"1"}}}
			data := queryResult{contains: []string{"FROM tasks WHERE user_id = 'user-1'", "ORDER BY created_at DESC LIMIT 3 OFFSET 2"}, columns: 7, rows: [][]string{taskRow()}}
			filter := repotypes.TaskFilter{Limit: 3, Offset: 2}

			if kind == "filtered" {
				status := entity.TaskStatusTodo
				filter.Status = &status

				count.contains = append(count.contains, "status = 'todo'")
				data.contains = append(data.contains, "status = 'todo'")
			}

			if kind == "count-error" {
				count.code = "XX000"
			}

			if kind == "query-error" {
				data.code = "XX000"
			}

			if kind == "scan-error" {
				data.rows[0][5] = "bad-time"
			}

			results := []queryResult{count, data}
			if kind == "count-error" {
				results = results[:1]
			}

			tasks, total, err := New(database(t, results...)).List(t.Context(), "user-1", filter)
			if strings.HasSuffix(kind, "error") {
				require.Error(t, err)
				assert.Nil(t, tasks)
				assert.Zero(t, total)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, 1, total)
			require.Len(t, tasks, 1)
			assert.Equal(t, "task-1", tasks[0].ID)
		})
	}
}

func TestBuilderFailures(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"store", "get", "list-count", "list-data", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			pg := &postgres.Postgres{}
			failAt := 1

			if operation == "list-data" {
				pg = database(t, queryResult{contains: []string{"COUNT(*)"}, columns: 1, rows: [][]string{{"0"}}})
				failAt = 2
			}

			pg.Builder = squirrel.StatementBuilder.PlaceholderFormat(&brokenPlaceholder{failAt: failAt})
			repo := New(pg)

			var err error

			switch operation {
			case "store":
				err = repo.Store(t.Context(), &entity.Task{})
			case "get":
				_, err = repo.GetByID(t.Context(), "user-1", "task-1")
			case "list-count", "list-data":
				_, _, err = repo.List(t.Context(), "user-1", emptyFilter())
			case "update":
				err = repo.Update(t.Context(), &entity.Task{})
			case "delete":
				err = repo.Delete(t.Context(), "user-1", "task-1")
			}

			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		})
	}
}
func emptyFilter() repotypes.TaskFilter { return repotypes.TaskFilter{} }
func TestTracePaginationClamping(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(math.MaxInt64), safeUint64ToInt64(math.MaxUint64))
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
