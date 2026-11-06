package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectCanceledCallDoesNotReturnSuccessfulReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, Options{})
	require.ErrorIs(t, err, context.Canceled)
}
