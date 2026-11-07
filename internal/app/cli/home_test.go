package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnifiedHomeConfigurationPrecedesSession(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".my"), 0o700))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(home, ".my", "config.toml"), []byte("[logging]\nlevel='invalid'\n"), 0o600),
	)
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"greet", "--name", "fixture", "--json"}, Options{
		Name: "my", Out: &out, ErrOut: &stderr,
		LookupEnv: func(k string) (string, bool) { return home, k == "HOME" || k == "USERPROFILE" },
	})
	require.Equal(t, 1, code, stderr.String())
	require.Empty(t, out.String())
	_, err := os.Stat(filepath.Join(home, ".my", "state"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
