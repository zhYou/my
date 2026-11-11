package target

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/datarepo"
	"golang.org/x/sys/unix"
)

func TestFileStateReadRejectsFIFO(t *testing.T) {
	state := NewFileState(filepath.Join(t.TempDir(), "state"), "workstation")
	metadata := datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"}
	require.NoError(t, state.Write(metadata, Baseline{Target: Topgrade, Units: map[UnitID]BaselineValue{}}))
	file, err := state.baselinePath(metadata, Topgrade)
	require.NoError(t, err)
	require.NoError(t, os.Remove(file))
	require.NoError(t, unix.Mkfifo(filepath.Clean(file), 0o600))
	_, _, err = state.Read(metadata, Topgrade)
	require.Error(t, err)
}
