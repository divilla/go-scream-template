package natsrpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCarrier(t *testing.T) {
	t.Parallel()

	carrier := HeaderCarrier{"invalid": []string{}}
	assert.Empty(t, carrier.Get("absent"))
	assert.Empty(t, carrier.Get("invalid"))
	carrier.Set("traceparent", "parent")
	assert.Equal(t, "parent", carrier.Get("traceparent"))
	assert.ElementsMatch(t, []string{"traceparent", "invalid"}, carrier.Keys())
}
