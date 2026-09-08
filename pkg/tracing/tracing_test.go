package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestConfiguration(t *testing.T) {
	original, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	t.Cleanup(func() { otel.SetTracerProvider(original); otel.SetTextMapPropagator(propagator) })
	shutdown, err := New(t.Context(), Config{})
	require.NoError(t, err)
	require.NoError(t, shutdown(t.Context()))
	assert.ElementsMatch(t, []string{"traceparent", "tracestate", "baggage"}, otel.GetTextMapPropagator().Fields())

	for _, insecure := range []bool{false, true} {
		shutdown, err = New(t.Context(), Config{Enabled: true, ServiceName: "test", Version: "1", Endpoint: "localhost:1", Insecure: insecure, SampleRate: 1})
		require.NoError(t, err)
		require.NoError(t, shutdown(t.Context()))
	}

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "invalid")
	_, err = New(t.Context(), Config{Enabled: true, Endpoint: "localhost:1"})
	require.ErrorContains(t, err, "resource.New")
}

//nolint:paralleltest // Tracing configuration changes the global OTel provider.
func TestShutdownError(t *testing.T) {
	original, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	t.Cleanup(func() { otel.SetTracerProvider(original); otel.SetTextMapPropagator(propagator) })
	shutdown, err := New(t.Context(), Config{Enabled: true, ServiceName: "test", Endpoint: "localhost:1", Insecure: true, SampleRate: 1})
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(t.Context(), "pending")
	span.End()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, shutdown(ctx))

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(t.Context(), carrier)
	assert.Empty(t, carrier)
}
