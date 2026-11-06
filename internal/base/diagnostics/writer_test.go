package diagnostics

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRotationPreservesWholeRecordsAndBoundsBackups(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, root.Close())
	}()
	w, err := newWriter(root, EventsFile, 12, 2)
	require.NoError(t, err)
	for _, s := range []string{"first\n", "second\n", "third\n", "fourth\n"} {
		_, err = w.Write([]byte(s))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
	entries, err := os.ReadDir(root.Name())
	require.NoError(t, err)
	require.Len(t, entries, 3)
	var all bytes.Buffer
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(root.Name(), e.Name()))
		require.NoError(t, err)
		all.Write(b)
		info, err := e.Info()
		require.NoError(t, err)
		if runtime.GOOS != "windows" {
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		}
	}
	require.Contains(t, all.String(), "second\n")
	require.Contains(t, all.String(), "third\n")
	require.Contains(t, all.String(), "fourth\n")
	require.NotContains(t, all.String(), "first\n")
	_, err = w.Write([]byte("closed"))
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestOversizeRecordIsNotSplit(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, root.Close())
	}()
	w, err := newWriter(root, TraceFile, 4, 0)
	require.NoError(t, err)
	_, err = w.Write([]byte("oversized\n"))
	require.NoError(t, err)
	_, err = w.Write([]byte("{}\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	b, err := root.ReadFile("trace.000001.jsonl")
	require.NoError(t, err)
	require.Equal(t, "oversized\n", string(b))
}
