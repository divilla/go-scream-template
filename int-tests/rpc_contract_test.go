package integration_test

import (
	"testing"

	"github.com/divilla/go-scream-template/internal/entity"
	natsClient "github.com/divilla/go-scream-template/pkg/nats/nats_rpc/client"
	rmqClient "github.com/divilla/go-scream-template/pkg/rabbitmq/rmq_rpc/client"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type contractRPC interface {
	RemoteCall(string, any, any) error
	Shutdown() error
}

func contractClients(t *testing.T, run func(*testing.T, contractRPC)) {
	t.Helper()

	for _, transport := range []string{"rabbitmq", "nats"} {
		t.Run(transport, func(t *testing.T) {
			var client contractRPC

			var err error
			if transport == "rabbitmq" {
				client, err = rmqClient.New(rmqURL, rpcServerExchange, rpcClientExchange)
			} else {
				client, err = natsClient.New(natsURL, rpcServerExchange)
			}

			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Shutdown()) })
			run(t, client)
		})
	}
}

func TestRPCFullTaskLifecycle(t *testing.T) {
	contractClients(t, func(t *testing.T, client contractRPC) {
		t.Helper()

		token := registerAndLogin(t)

		var task entity.Task

		call := func(operation string, data, response any) {
			t.Helper()
			require.NoError(t, client.RemoteCall("v1.task."+operation, authenticatedPayload(token, data), response))
		}
		call("create", map[string]string{"title": "lifecycle"}, &task)
		require.NotEmpty(t, task.ID)
		id := task.ID
		call("get", map[string]string{"id": id}, &task)
		require.Equal(t, "lifecycle", task.Title)
		call("update", map[string]string{"id": id, "title": "updated"}, &task)
		require.Equal(t, "updated", task.Title)

		for _, next := range []string{"in_progress", "todo", "in_progress", "done"} {
			call("transition", map[string]string{"id": id, "status": next}, &task)
			require.Equal(t, next, string(task.Status))
		}

		var list struct {
			Tasks []entity.Task
			Total int
		}
		call("list", map[string]any{"status": "done", "limit": 0, "offset": -1}, &list)
		require.Equal(t, 1, list.Total)
		require.Len(t, list.Tasks, 1)
		require.Equal(t, id, list.Tasks[0].ID)

		var deleted struct{ Status string }
		call("delete", map[string]string{"id": id}, &deleted)
		require.Equal(t, "deleted", deleted.Status)
		require.ErrorContains(t, client.RemoteCall("v1.task.get", authenticatedPayload(token, map[string]string{"id": id}), &task), "internal server error")
	})
}

func TestRPCValidationAndAuthentication(t *testing.T) {
	contractClients(t, func(t *testing.T, client contractRPC) {
		t.Helper()

		token := registerAndLogin(t)

		operations := []string{"task.create", "task.get", "task.list", "task.update", "task.transition", "task.delete", "translation.translate", "translation.getHistory"}
		for _, operation := range operations {
			t.Run(operation, func(t *testing.T) {
				for _, payload := range []any{"invalid envelope", authenticatedPayload("invalid", nil)} {
					var response any
					require.ErrorContains(t, client.RemoteCall("v1."+operation, payload, &response), "internal server error")
				}

				if operation == "translation.getHistory" {
					return
				}

				for _, data := range []any{"invalid data type", map[string]string{"status": "invalid"}} {
					var response any
					require.ErrorContains(t, client.RemoteCall("v1."+operation, authenticatedPayload(token, data), &response), "internal server error")
				}
			})
		}
	})
}

func TestRPCPublicValidation(t *testing.T) {
	contractClients(t, func(t *testing.T, client contractRPC) {
		t.Helper()

		for _, operation := range []string{"register", "login"} {
			for _, payload := range []any{"invalid data type", map[string]string{}} {
				var response any
				require.ErrorContains(t, client.RemoteCall("v1.auth."+operation, payload, &response), "internal server error")
			}
		}

		var response any
		require.ErrorContains(t, client.RemoteCall("v1.unknown", nil, &response), "unregistered handler")
		name := uniqueUsername(t)
		registration := map[string]string{"username": name, "email": name + "@test.com", "password": testPassword}
		require.NoError(t, client.RemoteCall("v1.auth.register", registration, &response))
		require.ErrorContains(t, client.RemoteCall("v1.auth.register", registration, &response), "internal server error")
		require.ErrorContains(t, client.RemoteCall("v1.auth.login", map[string]string{"email": name + "@test.com", "password": "wrongpassword"}, &response), "internal server error")
	})
}

func TestRPCTaskFailuresAndTranslation(t *testing.T) {
	contractClients(t, func(t *testing.T, client contractRPC) {
		t.Helper()

		token := registerAndLogin(t)
		task := httpCreateTask(t, token, "owned", "")

		other := registerAndLogin(t)
		for _, operation := range []string{"get", "update", "transition", "delete"} {
			for _, test := range []struct{ token, id string }{{other, task.ID}, {token, uuid.NewString()}, {token, "invalid-uuid"}} {
				var response any
				require.ErrorContains(t, client.RemoteCall("v1.task."+operation, authenticatedPayload(test.token, map[string]string{"id": test.id, "title": "changed", "status": "in_progress"}), &response), "internal server error")
			}
		}

		var response any
		require.ErrorContains(t, client.RemoteCall("v1.task.transition", authenticatedPayload(token, map[string]string{"id": task.ID, "status": "done"}), &response), "internal server error")

		var translation entity.Translation
		require.NoError(t, client.RemoteCall("v1.translation.translate", authenticatedPayload(token, map[string]string{"source": "auto", "destination": "en", "original": "текст для перевода"}), &translation))
		require.Equal(t, "text for translation", translation.Translation)

		var history entity.TranslationHistory
		require.NoError(t, client.RemoteCall("v1.translation.getHistory", authenticatedPayload(token, nil), &history))
		require.Len(t, history.History, 1)
		require.Equal(t, translation.Translation, history.History[0].Translation)
		require.ErrorContains(t, client.RemoteCall("v1.translation.translate", authenticatedPayload(token, map[string]string{"source": "auto", "destination": "en", "original": "fixture failure"}), &response), "internal server error")
	})
}
