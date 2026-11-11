package privatefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedReadAndAtomicReplacement(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	require.NoError(t, Directory(directory, true))
	path := filepath.Join(directory, "value")
	require.NoError(t, AtomicWrite(path, []byte("old"), WriteOptions{Mode: 0o600}))
	_, err := ReadFile(path, 2)
	require.ErrorIs(t, err, ErrLimit)
	require.NoError(t, AtomicWrite(path, []byte("new"), WriteOptions{Mode: 0o600}))
	data, err := ReadFile(path, 3)
	require.NoError(t, err)
	require.Equal(t, "new", string(data))
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	_, err = ReadFile(filepath.Join(directory, "missing"), 3)
	require.ErrorIs(t, err, os.ErrNotExist)
}
