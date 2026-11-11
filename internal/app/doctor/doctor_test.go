package doctor

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDirectCanceledCallDoesNotReturnSuccessfulReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, Options{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestBusinessDoctorIsOfflineAndDoesNotCreateState(t *testing.T) {
	options := businessOptions(t)
	dirs, err := options.Paths()
	require.NoError(t, err)
	result, err := Run(t.Context(), options)
	require.NoError(t, err)
	require.True(t, result.RequiredAvailable)
	require.Len(t, result.Checks, 5)
	files, err := os.ReadDir(dirs.Config)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestBusinessCheckTimeoutReturnsCompletedFailure(t *testing.T) {
	options := businessOptions(t)
	options.Timeout = 10 * time.Millisecond
	options.CheckGH = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	result, err := Run(t.Context(), options)
	require.NoError(t, err)
	require.False(t, result.RequiredAvailable)
	require.Equal(t, StatusFailed, result.Checks[2].Status)
	require.Equal(t, StatusSkipped, result.Checks[3].Status)
}
