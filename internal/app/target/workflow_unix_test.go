//go:build unix

package target

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestSyncRejectsSpecialTargetFile(t *testing.T) {
	home, repository, service := capturedTopgrade(t, "verbose = true\n")
	path := filepath.Join(home, ".config", "topgrade.toml")
	require.NoError(t, os.Remove(path))
	require.NoError(t, unix.Mkfifo(path, 0o600))

	_, err := service.Sync(t.Context(), SyncOptions{Targets: []ID{Topgrade}})
	requireUnsupportedFile(t, err)
	require.Empty(t, repository.commits)
}
