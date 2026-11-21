//go:build unix

package target

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestAtomicWriteFileRejectsSpecialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	require.NoError(t, unix.Mkfifo(path, 0o600))

	err := AtomicWriteFile(path, []byte("new"), 0o600)
	requireUnsupportedFile(t, err)
	info, statErr := os.Lstat(path)
	require.NoError(t, statErr)
	require.NotZero(t, info.Mode()&os.ModeNamedPipe)
}
