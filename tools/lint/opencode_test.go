package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenCodeConfigAcceptsRepository(t *testing.T) {
	require.NoError(t, checkOpenCodeConfig(filepath.Join("..", "..")))
}

func TestOpenCodeConfigRejectsDrift(t *testing.T) {
	root, contents := openCodeFixture(t)
	for _, tt := range []struct {
		name string
		old  string
		new  string
	}{
		{"超时", `"startup": 300000`, `"startup": 30000`},
		{"工作区", `"directory": ".worktrees"`, `"directory": "../worktrees"`},
		{"Codex context", `"mcp:serve:opencode"`, `"mcp:serve"`},
		{"MCP 目录", `"cwd": "."`, `"cwd": ".."`},
		{"服务超时覆盖", `"cwd": "."`, `"cwd": ".",
        "timeout": {"startup": 30000}`},
		{"默认审批", `"resource": "*",
      "effect": "ask"`, `"resource": "*",
      "effect": "allow"`},
		{"Graphify 工具", `"graphify_god_nodes"`, `"graphify_debug"`},
		{"未知顶层字段", `"permissions": [`, `"provider": {},
  "permissions": [`},
		{"未知服务字段", `"cwd": "."`, `"cwd": ".",
        "options": {"apiKey": "synthetic"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, string(contents), tt.old)
			path := writeOpenCodeFixture(t, root, strings.Replace(string(contents), tt.old, tt.new, 1))
			err := checkOpenCodeConfig(root)
			require.Error(t, err)
			require.Contains(t, err.Error(), filepath.FromSlash(openCodeConfigPath))
			require.FileExists(t, path)
		})
	}
}

func TestOpenCodeConfigRejectsInvalidOrMissingFile(t *testing.T) {
	root, _ := openCodeFixture(t)
	path := writeOpenCodeFixture(t, root, "{")
	require.ErrorContains(t, checkOpenCodeConfig(root), "JSON 无效")
	require.NoError(t, os.Remove(path))
	require.Error(t, checkOpenCodeConfig(root))
}

func TestOpenCodeConfigRejectsSymlink(t *testing.T) {
	root, contents := openCodeFixture(t)
	target := filepath.Join(t.TempDir(), "opencode.json")
	require.NoError(t, os.WriteFile(target, contents, 0o600))
	path := filepath.Join(root, filepath.FromSlash(openCodeConfigPath))
	require.NoError(t, os.Remove(path))
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("当前系统无法创建符号链接: %v", err)
	}
	require.ErrorContains(t, checkOpenCodeConfig(root), "不是普通文件")
}

func openCodeFixture(t *testing.T) (string, []byte) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(openCodeConfigPath)))
	require.NoError(t, err)
	root := t.TempDir()
	writeOpenCodeFixture(t, root, string(contents))
	return root, contents
}

func writeOpenCodeFixture(t *testing.T, root, contents string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(openCodeConfigPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	//nolint:gosec // G703：只改写当前测试创建的临时 fixture。
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}
