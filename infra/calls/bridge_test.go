package calls

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
)

func TestWithoutAProviderTheCallGoesToTheRealNumber(t *testing.T) {
	call, err := Direct{}.Connect(context.Background(), "+244923000111", "+244923000222", "AG-1")
	require.NoError(t, err)
	assert.Equal(t, &services.Call{Number: "+244923000222"}, call)
}

func TestWithoutANumberThereIsNoCall(t *testing.T) {
	_, err := Direct{}.Connect(context.Background(), "+244923000111", "", "AG-1")
	assert.ErrorIs(t, err, services.ErrCallsUnavailable)
}
