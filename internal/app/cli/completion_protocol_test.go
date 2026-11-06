package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompletionRecognizesInitializedHelpAndVersionFlags(t *testing.T) {
	for _, flag := range []string{"--help=false", "--version=false", "-h=false", "-v=false"} {
		code, out, stderr := invoke(t, flag, "__complete", "--lang", "")
		require.Zero(t, code, stderr)
		require.Empty(t, stderr)
		require.Contains(t, out, ":4\n")
	}
}

func TestCanceledCompletionDoesNotCommitErrorDirective(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, out, stderr := invokeContext(ctx, t, "__complete", "--unknown", "")
	require.Equal(t, 130, code)
	require.Empty(t, out)
	require.NotEmpty(t, stderr)
}
