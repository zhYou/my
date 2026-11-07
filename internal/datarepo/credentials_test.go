package datarepo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/command"
)

func TestReadCredentialDisablesRefreshAndNeverReturnsRawStderr(t *testing.T) {
	calls := 0
	source := CredentialSource{
		Capture: func(_ context.Context, call command.Invocation, execution command.Execution, capture command.CaptureOptions) command.Result {
			calls++
			require.Contains(t, execution.Env, "GH_DEBUG=")
			require.Contains(t, execution.Env, "GH_PROMPT_DISABLED=1")
			require.Positive(t, capture.Limit)
			if calls == 1 {
				require.Equal(t, []string{"auth", "token", "--help"}, call.Args)
				return command.Result{Stdout: []byte("--no-refresh"), Status: command.Succeeded}
			}
			require.Equal(t, []string{"auth", "token", "--hostname", "github.com", "--no-refresh"}, call.Args)
			return command.Result{
				Stdout: []byte("fixture-secret\n"),
				Stderr: []byte("private-stderr"),
				Status: command.Succeeded,
			}
		},
	}
	token, err := source.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, "fixture-secret", token)
}
