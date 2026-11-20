package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func verificationProjectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.27.0\n"), 0o600),
	)
	require.NoError(t, os.WriteFile(filepath.Join(root, "mise.toml"), []byte("[tools]\ngo = \"1.27.4\"\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cmd", "demo"), 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "cmd", "demo", "main.go"),
		[]byte("package main\nconst appName = \"demo\"\nfunc main() {}\n"),
		0o600,
	))
	return root
}
