package target

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/datarepo"
)

func TestFileStateIsolatesRepositoryBranchAndTarget(t *testing.T) {
	state := NewFileState(filepath.Join(t.TempDir(), "state"), "workstation")
	unlock, err := state.Lock(t.Context())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, unlock())
	}()

	first := datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"}
	second := datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "feature"}
	baseline := Baseline{
		Target: Mise,
		Units: map[UnitID]BaselineValue{
			FieldUnitID("tools", "go"): {
				Exists: true,
				Digest: "sha256:fixture",
				Data:   []byte("1.27.3"),
			},
		},
	}
	require.NoError(t, state.Write(first, baseline))
	loaded, exists, err := state.Read(first, Mise)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, baseline, loaded)

	_, exists, err = state.Read(second, Mise)
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, state.Clear(first, Mise))
	_, exists, err = state.Read(first, Mise)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestFileStateRejectsSecretData(t *testing.T) {
	state := NewFileState(filepath.Join(t.TempDir(), "state"), "workstation")
	err := state.Write(
		datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"},
		Baseline{
			Target: CodexAuth,
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
	state := NewFileState(filepath.Join(t.TempDir(), "state"), "workstation")
	unlock, err := state.Lock(t.Context())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, unlock())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = state.Lock(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestFileStatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不提供 POSIX mode")
	}
	state := NewFileState(filepath.Join(t.TempDir(), "state"), "workstation")
	require.NoError(t, state.Write(
		datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"},
		Baseline{Target: Mise, Units: map[UnitID]BaselineValue{}},
	))
	info, err := os.Stat(state.targetDirectory())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestFileStateRejectsSymlinkDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.Mkdir(root, 0o700))
	outside := t.TempDir()
	require.NoError(t, os.Chmod(outside, 0o700)) // #nosec G302 -- 目标为测试私有目录。
	if err := os.Symlink(outside, filepath.Join(root, "target")); err != nil {
		t.Skipf("当前环境不能创建符号链接：%v", err)
	}
	state := NewFileState(root, "workstation")
	unlock, err := state.Lock(t.Context())
	if unlock != nil {
		t.Cleanup(func() {
			require.NoError(t, unlock())
		})
	}
	require.Error(t, err)
	_, err = os.Stat(filepath.Join(outside, "lock"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestFileStateReadRejectsUnsafeBaseline(t *testing.T) {
	for _, kind := range []string{"symlink", "public", "parent-symlink"} {
		t.Run(kind, func(t *testing.T) {
			state := NewFileState(filepath.Join(t.TempDir(), "state"), "workstation")
			metadata := datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"}
			require.NoError(
				t,
				state.Write(metadata, Baseline{Target: Topgrade, Units: map[UnitID]BaselineValue{}}),
			)
			file, err := state.baselinePath(metadata, Topgrade)
			require.NoError(t, err)
			switch kind {
			case "symlink":
				renamed := filepath.Join(t.TempDir(), "outside.json")
				require.NoError(t, os.Rename(file, renamed))
				if err := os.Symlink(renamed, file); err != nil {
					t.Skipf("当前环境不能创建符号链接：%v", err)
				}
			case "public":
				if runtime.GOOS == "windows" {
					t.Skip("Windows 不使用 Unix mode 位证明 ACL")
				}
				require.NoError(t, os.Chmod(file, 0o644)) // #nosec G302 -- 故意构造过宽权限的拒绝 fixture。
			case "parent-symlink":
				directory := filepath.Dir(file)
				renamed := filepath.Join(t.TempDir(), "outside")
				require.NoError(t, os.Rename(directory, renamed))
				if err := os.Symlink(renamed, directory); err != nil {
					t.Skipf("当前环境不能创建符号链接：%v", err)
				}
			}
			_, _, err = state.Read(metadata, Topgrade)
			require.Error(t, err)
		})
	}
}

func TestFileStateSeparatesMachineIdentities(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	first := NewFileState(directory, "workstation")
	second := NewFileState(directory, "laptop")
	metadata := datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main"}
	require.NoError(t, first.Write(metadata, Baseline{Target: Mise, Units: map[UnitID]BaselineValue{}}))
	_, exists, err := second.Read(metadata, Mise)
	require.NoError(t, err)
	require.False(t, exists)
}
