package shellenv

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileCacheReadRejectsUnsafeFile(t *testing.T) {
	for _, kind := range []string{"symlink", "public"} {
		t.Run(kind, func(t *testing.T) {
			service, _ := newEnvironmentService(t, "workstation")
			_, err := service.Set(t.Context(), SetOptions{Name: "EDITOR", Value: []byte("nvim")})
			require.NoError(t, err)
			cache, ok := service.options.Cache.(*FileCache)
			require.True(t, ok)
			file := ""
			require.NoError(t, filepath.WalkDir(cache.directory, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Name() == "state.json" {
					file = path
				}
				return nil
			}))
			require.NotEmpty(t, file)
			if kind == "symlink" {
				renamed := filepath.Join(t.TempDir(), "outside.json")
				require.NoError(t, os.Rename(file, renamed))
				if err := os.Symlink(renamed, file); err != nil {
					t.Skipf("当前环境不能创建符号链接：%v", err)
				}
			} else {
				if runtime.GOOS == "windows" {
					t.Skip("Windows 不使用 Unix mode 位证明 ACL")
				}
				require.NoError(t, os.Chmod(file, 0o644)) // #nosec G302 -- 故意构造过宽权限的拒绝 fixture。
			}
			_, _, err = cache.Read()
			require.Error(t, err)
		})
	}
}

func TestFileCacheRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "cache")
	outside := t.TempDir()
	require.NoError(t, os.Chmod(outside, 0o700)) // #nosec G302 -- 目标为测试私有目录。
	if err := os.Symlink(outside, directory); err != nil {
		t.Skipf("当前环境不能创建符号链接：%v", err)
	}
	cache := NewFileCache(directory, "workstation")
	unlock, err := cache.Lock(t.Context())
	if unlock != nil {
		t.Cleanup(func() {
			require.NoError(t, unlock())
		})
	}
	require.Error(t, err)
}
