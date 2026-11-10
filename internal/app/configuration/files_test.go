package configuration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnifiedTextDiffMarksBothSides(t *testing.T) {
	diff := UnifiedTextDiff("go.rules", []byte("old\n"), []byte("new\n"))
	require.Contains(t, diff, "--- go.rules")
	require.Contains(t, diff, "+++ go.rules")
	require.Contains(t, diff, "-old")
	require.Contains(t, diff, "+new")
}

func TestAtomicWriteFilePreservesExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不提供 POSIX mode")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	//nolint:gosec // 测试需要验证非默认权限被保留。
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o640))
	require.NoError(t, AtomicWriteFile(path, []byte("new\n"), 0o600))
	data, err := os.ReadFile(path) //nolint:gosec // 路径来自 t.TempDir()。
	require.NoError(t, err)
	require.Equal(t, "new\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestAtomicWriteFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("符号链接创建需要额外 Windows 权限")
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))
	link := filepath.Join(directory, "link")
	require.NoError(t, os.Symlink(target, link))
	require.Error(t, AtomicWriteFile(link, []byte("new"), 0o600))
}
