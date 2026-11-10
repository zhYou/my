package configuration

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/datarepo"
)

func TestFileStateIsolatesRepositoryBranchAndTarget(t *testing.T) {
	state := NewFileState(t.TempDir())
	unlock, err := state.Lock(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, unlock()) }()

	first := datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"}
	second := datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "feature"}
	baseline := Baseline{
		Target: TargetMise,
		Units: map[UnitID]BaselineValue{
			FieldUnitID("tools", "go"): {
				Exists: true,
				Digest: "sha256:fixture",
				Data:   []byte("1.27.3"),
			},
		},
	}
	require.NoError(t, state.Write(first, baseline))
	loaded, exists, err := state.Read(first, TargetMise)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, baseline, loaded)

	_, exists, err = state.Read(second, TargetMise)
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, state.Clear(first, TargetMise))
	_, exists, err = state.Read(first, TargetMise)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestFileStateRejectsSecretData(t *testing.T) {
	state := NewFileState(t.TempDir())
	err := state.Write(
		datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"},
		Baseline{
			Target: TargetCodexAuth,
			Units: map[UnitID]BaselineValue{
				SecretUnitID: {
					Exists: true,
					Secret: true,
					Digest: "sha256:fixture",
					Data:   []byte("secret-marker"),
				},
			},
		},
	)
	require.Error(t, err)
}

func TestFileStateLockHonorsCancellation(t *testing.T) {
	state := NewFileState(t.TempDir())
	unlock, err := state.Lock(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, unlock()) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = state.Lock(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestFileStatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不提供 POSIX mode")
	}
	state := NewFileState(t.TempDir())
	require.NoError(t, state.Write(
		datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"},
		Baseline{Target: TargetMise, Units: map[UnitID]BaselineValue{}},
	))
	require.Equal(t, os.FileMode(stateModeDirectory), state.directoryModeForTest())
}
