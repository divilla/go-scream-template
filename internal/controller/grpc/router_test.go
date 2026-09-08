package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	pbgrpc "google.golang.org/grpc"
)

func TestRegistration(t *testing.T) {
	t.Parallel()

	server := pbgrpc.NewServer()
	defer server.Stop()

	NewRouter(server, nil, nil, nil, nil)

	services := server.GetServiceInfo()
	for _, name := range []string{"grpc.v1.AuthService", "grpc.v1.TaskService", "grpc.v1.Translation", "grpc.reflection.v1.ServerReflection", "grpc.reflection.v1alpha.ServerReflection"} {
		assert.Contains(t, services, name)
	}
}
