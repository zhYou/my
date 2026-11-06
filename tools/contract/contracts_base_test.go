package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadContractBaseAcrossLayoutMigration(t *testing.T) {
	for _, mode := range []string{"old", "new", "both", "none"} {
		t.Run(mode, func(t *testing.T) {
			root := contractHistory(t)
			if mode == "old" || mode == "both" {
				writeContractFixture(t, root, "testdata/contracts/v1.json", []byte("old"))
			}
			if mode == "new" || mode == "both" {
				writeContractFixture(t, root, "test/testdata/contracts/v1.json", []byte("new"))
			}
			writeContractFixture(t, root, "test/testdata/contracts/initial.json", []byte("initial"))
			commitContractFixture(t, root)
			t.Chdir(root)
			data, initial, err := readContractBase(context.Background(), "HEAD")
			require.NoError(t, err)
			want := map[string]string{"old": "old", "new": "new", "both": "new", "none": "initial"}[mode]
			require.Equal(t, want, string(data))
			require.Equal(t, mode == "none", initial)
		})
	}
}

func TestReadContractBaseRejectsInvalidHistory(t *testing.T) {
	for _, mode := range []string{"ref", "option", "not-git", "cancelled", "directory", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := contractHistory(t)
			switch mode {
			case "directory":
				writeContractFixture(t, root, "test/testdata/contracts/v1.json/inside", []byte("fixture"))
			case "symlink":
				//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
				require.NoError(t, os.MkdirAll(filepath.Join(root, "test/testdata/contracts"), 0o755))
				if err := os.Symlink(
					"../../../README.md",
					filepath.Join(root, "test/testdata/contracts/v1.json"),
				); err != nil {
					t.Skipf("当前平台无法创建链接：%v", err)
				}
			}
			commitContractFixture(t, root)
			t.Chdir(root)
			ref := "HEAD"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "ref":
				ref = "unknown-ref"
			case "option":
				ref = "--all"
			case "not-git":
				t.Chdir(t.TempDir())
			case "cancelled":
				cancel()
			}
			_, _, err := readContractBase(ctx, ref)
			require.Error(t, err)
		})
	}
}

func TestInvalidNewContractDoesNotFallback(t *testing.T) {
	actual, err := captureContract(context.Background())
	require.NoError(t, err)
	for _, mode := range []string{"corrupt", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := contractHistory(t)
			writeContractFixture(t, root, "testdata/contracts/v1.json", actual)
			if mode == "corrupt" {
				writeContractFixture(t, root, "test/testdata/contracts/v1.json", []byte("{"))
			} else {
				//nolint:gosec // G301：测试路径和权限只来自当前临时 fixture，不读取用户文件。
				require.NoError(t, os.MkdirAll(filepath.Join(root, "test/testdata/contracts"), 0o755))
				if err := os.Symlink(
					"../../../testdata/contracts/v1.json",
					filepath.Join(root, "test/testdata/contracts/v1.json"),
				); err != nil {
					t.Skipf("当前平台无法创建链接：%v", err)
				}
			}
			commitContractFixture(t, root)
			require.NoError(t, os.Remove(filepath.Join(root, "test/testdata/contracts/v1.json")))
			writeContractFixture(t, root, "test/testdata/contracts/v1.json", actual)
			t.Chdir(root)
			require.Error(t, checkContract(context.Background(), "HEAD"))
		})
	}
}

func contractGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	data, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", data)
	return strings.TrimSpace(string(data))
}

func contractHistory(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	contractGit(t, root, "-c", "init.templateDir=", "init", "--quiet")
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture"), 0o600))
	return root
}

func writeContractFixture(t *testing.T, root, path string, data []byte) {
	t.Helper()
	//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), data, 0o600))
}

func commitContractFixture(t *testing.T, root string) {
	t.Helper()
	contractGit(t, root, "add", ".")
	contractGit(
		t,
		root,
		"-c",
		"user.name=fixture",
		"-c",
		"user.email=fixture@example.com",
		"commit",
		"--quiet",
		"-m",
		"fixture",
	)
}
