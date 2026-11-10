package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigurationCommandsAreDiscoverable(t *testing.T) {
	code, out, stderr := invoke(t, "schema", "config", "capture")
	require.Zero(t, code, stderr)
	require.Contains(t, out, `"all"`)
	require.Contains(t, out, `"machine"`)

	code, out, stderr = invoke(t, "schema", "config", "resolve")
	require.Zero(t, code, stderr)
	require.Contains(t, out, `"unit"`)
	require.Contains(t, out, `"from"`)
}

func TestActivateRejectsJSONWithoutReadingCache(t *testing.T) {
	code, out, stderr := invoke(t, "activate", "bash", "--json")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	require.Contains(t, stderr, "cli.usage")
}
