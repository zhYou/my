package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"doc", "extra"}} {
		require.ErrorContains(t, run(args), "用法")
	}
}

// 开发入口必须执行真实检查，并向调用者返回配置错误。
func TestI18nAndConfigCheckTasks(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	require.NoError(t, run([]string{"i18n"}))
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll("configs", 0o700))
	example := filepath.Join("configs", "config.example.toml")
	require.NoError(t, os.WriteFile(example, []byte("[logging]\nlevel = \"invalid\"\n"), 0o600))
	err := run([]string{"config"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "config.example.toml")
	require.NoError(t, os.WriteFile(example, []byte("[logging]\nlevel = \"info\"\n"), 0o600))
	require.NoError(t, run([]string{"config"}))
}

func TestDocCheckTaskReportsDocumentationFailures(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile("feature.go", []byte("package feature\n"), 0o600))
	require.ErrorContains(t, run([]string{"doc"}), "doc.go")
	require.NoError(t, os.WriteFile("doc.go", []byte("// Package feature 校验输入。\npackage feature\n"), 0o600))
	require.NoError(t, run([]string{"doc"}))
	require.NoError(t, os.WriteFile("doc.go", []byte("package feature\n"), 0o600))
	require.ErrorContains(t, run([]string{"doc"}), "包说明不能为空")
}
