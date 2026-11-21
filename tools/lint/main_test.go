package main

import (
	"errors"
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

// 检查名、用法和分发共用同一注册表；新增检查只需增加一条声明。
func TestEntryRegistryDrivesUsageAndDispatch(t *testing.T) {
	ran := false
	checks := lintRegistry{
		{name: "first", run: func() error {
			ran = true
			return nil
		}},
		{name: "second", run: func() error { return errors.New("检查失败") }},
	}

	require.NoError(t, checks.run([]string{"first"}))
	require.True(t, ran)
	require.EqualError(t, checks.run([]string{"second"}), "检查失败")

	usage := "用法：lint <first|second>"
	require.Equal(t, usage, checks.usage())
	require.EqualError(t, checks.run(nil), usage)
	require.EqualError(t, checks.run([]string{"unknown"}), usage+"，未知检查 \"unknown\"")
}

// 开发入口使用 codex-rules 并执行真实检查。
func TestEntryDispatchesCodexRules(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	require.NoError(t, run([]string{"codex-rules"}))
	require.Contains(t, run(nil).Error(), "用法：lint <i18n|config|opencode|codex-rules|doc|style|versions|test>")
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
