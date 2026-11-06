package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestRecoveryRejectsRealSymlink(t *testing.T) {
	root := fixture(t)
	fs := afero.NewOsFs()
	i := &Initializer{FS: faultFS{
		Fs: fs,
		rename: func(a, b string) error {
			err := fs.Rename(a, b)
			if err == nil && b == filepath.Join(root, "go.mod") {
				panic("中断")
			}
			return err
		},
	}}
	require.Panics(t, func() {
		if _, err := i.Initialize(context.Background(), root, "example.com/tool", "tool"); err != nil {
			t.Error(err)
		}
	})
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("人工内容"), 0o600))
	require.NoError(t, os.Remove(filepath.Join(root, "cmd", TemplateName, "main.go")))
	if err := os.Symlink(outside, filepath.Join(root, "cmd", TemplateName, "main.go")); err != nil {
		t.Skipf("当前平台不支持创建符号链接：%v", err)
	}
	_, err := Initialize(root, "example.com/tool", "tool")
	require.Error(t, err)
	//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "人工内容", string(data))
}

func TestCanceledInitializationDoesNotWrite(t *testing.T) {
	i, root := memoryInitializer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := i.Initialize(ctx, root, "example.com/tool", "tool")
	require.ErrorIs(t, err, context.Canceled)
	ok, err := afero.Exists(i.FS, filepath.Join(root, journalDir))
	require.NoError(t, err)
	require.False(t, ok)
}
