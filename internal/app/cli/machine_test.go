package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/datarepo"
)

func TestMachineDiscoveryAndValidationPrecedeCredentials(t *testing.T) {
	code, out, stderr := invoke(t, "schema", "machine", "register")
	require.Zero(t, code, stderr)
	require.Contains(t, out, "kind")
	require.Contains(t, out, "note")
	_ = json.Valid([]byte(out))
}

func TestInternalRequestTimeoutIsRuntimeButParentCancellationWins(t *testing.T) {
	failure := (fault.Definition{Code: "data.access", Kind: fault.Runtime}).Wrap(
		&datarepo.RequestTimeoutError{Cause: context.DeadlineExceeded},
	)
	require.Equal(t, 1, operationExitCode(context.Background(), failure))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Equal(t, 130, operationExitCode(ctx, failure))
}

func TestBusinessDoctorReportsBadConfigurationWithoutSideEffects(t *testing.T) {
	home := t.TempDir()
	var out, stderr bytes.Buffer
	options := isolatedOptions(home, &out, &stderr)
	options.Dependencies.ProbeGH = func(context.Context) error {
		return nil
	}
	options.Dependencies.Credential = func(context.Context) (string, error) {
		t.Fatal("bad configuration must not read credentials")
		return "", nil
	}
	code := Run(t.Context(), []string{"doctor", "--remote", "--json"}, options)
	require.Equal(t, 1, code)
	require.Empty(t, stderr.String())
	var report struct {
		Required bool  `json:"required_available"`
		Checks   []any `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	require.False(t, report.Required)
	require.Len(t, report.Checks, 9)
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Empty(t, entries)
}
