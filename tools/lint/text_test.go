package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/tools/internal/fixture"
)

func TestTextFormattingSelectionAndReadOnlyCheck(t *testing.T) {
	root := textFixture(t)
	first := writeTextFixture(t, root, "目录 with spaces/first.json", `{"value":1}`)
	second := writeTextFixture(t, root, "second.json", `{"value":2}`)
	output, err := textTask(t, root, "fmt:text", "--", "目录 with spaces/first.json")
	require.NoError(t, err, output)
	data := readTextFixture(t, first)
	require.Equal(t, "{ \"value\": 1 }\n", string(data))
	untouched := readTextFixture(t, second)
	require.Equal(t, `{"value":2}`, string(untouched))
	output, err = textTask(t, root, "lint:text")
	require.Error(t, err, output)
	after := readTextFixture(t, second)
	require.Equal(t, untouched, after)
	output, err = textTask(t, root, "fmt:text")
	require.NoError(t, err, output)
	before := readTextFixture(t, second)
	output, err = textTask(t, root, "fmt:text")
	require.NoError(t, err, output)
	after = readTextFixture(t, second)
	require.Equal(t, before, after)
	output, err = textTask(t, root, "lint:text")
	require.NoError(t, err, output)
}

func TestTextSyntaxRejectsInvalidDocuments(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
	}{
		{"bad.json", `{"value": }`},
		{"duplicate.json", `{"value":1,"value":2}`},
		{".hidden/config.toml", "value = 1\nvalue = 2\n"},
		{"bad.xml", "<root><child></root>\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := textFixture(t)
			path := writeTextFixture(t, root, tt.name, tt.value)
			output, err := textTask(t, root, "lint:syntax")
			require.Error(t, err, output)
			require.Contains(t, output, filepath.Base(tt.name))
			data := readTextFixture(t, path)
			require.Equal(t, tt.value, string(data))
		})
	}
}

func TestTextLintChecksStructureAfterFormatting(t *testing.T) {
	for _, tt := range []struct {
		name       string
		value      string
		diagnostic string
	}{
		{"duplicate.json", `{"value":1,"value":2}`, "Duplicate key"},
		{"bad.yaml", "value: 1\nvalue: 2\n", "duplication of key"},
		{"bad.md", "# 标题\n\n### 跳级标题\n", "MD001"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := textFixture(t)
			path := writeTextFixture(t, root, tt.name, tt.value)
			output, err := textTask(t, root, "fmt:text")
			require.NoError(t, err, output)
			before := readTextFixture(t, path)
			output, err = textTask(t, root, "lint:text")
			require.Error(t, err, output)
			require.Contains(t, output, tt.diagnostic)
			after := readTextFixture(t, path)
			require.Equal(t, before, after)
		})
	}
}

func TestTextTasksCoverFiveFormatsAndExclusions(t *testing.T) {
	root := textFixture(t)
	for name, value := range map[string]string{
		"sample.json":                  `{"value":1}`,
		"sample.toml":                  "value=1\n",
		"sample.xml":                   "<root value = \"x\">原有文本 <b>bold</b> 尾部</root>\n",
		".github/workflows/sample.yml": "on:\n  push:\nshared: &shared\n  value: 1\ncopy: *shared\n",
		"sample.md":                    "# 中文标题\n\n保留人工换行\n继续同一段。\n\n```json\n这是故意不完整的示例\n```\n",
	} {
		writeTextFixture(t, root, name, value)
	}
	for _, dir := range []string{".worktrees/other", "docs/superpowers", "bin", "dist", "tools/lint/testdata/invalid", "vendor"} {
		for _, ext := range []string{"json", "xml", "toml", "yaml", "md"} {
			writeTextFixture(t, root, dir+"/invalid."+ext, "[<not valid data")
		}
	}
	snapshot := writeTextFixture(t, root, "test/testdata/contracts/v1.json", `{"generated":true}`)
	output, err := textTask(t, root, "fmt:text")
	require.NoError(t, err, output)
	data := readTextFixture(t, snapshot)
	require.Equal(t, `{"generated":true}`, string(data))
	xml := readTextFixture(t, filepath.Join(root, "sample.xml"))
	require.Contains(t, string(xml), "原有文本 <b>bold</b> 尾部")
	output, err = textTask(t, root, "lint:text")
	require.NoError(t, err, output)
	// Git 排除规则变化应同时作用于三类检查，而不是只依赖预先列出的目录。
	//nolint:gosec // G304：仅在本次临时副本追加 Git 忽略规则，不修改真实工作区。
	ignore, err := os.OpenFile(filepath.Join(root, ".gitignore"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = ignore.WriteString("\n/private-fixtures/\n")
	require.NoError(t, err)
	require.NoError(t, ignore.Close())
	for _, ext := range []string{"json", "xml", "toml", "yaml", "md"} {
		writeTextFixture(t, root, "private-fixtures/invalid."+ext, "[<not valid data")
	}
	output, err = textTask(t, root, "lint:text")
	require.NoError(t, err, output)
}

func TestTextTasksPrepareToolsOutsideProject(t *testing.T) {
	root := textFixture(t)
	cache := t.TempDir()
	t.Setenv("npm_config_cache", filepath.Join(cache, "npm"))
	t.Setenv("UV_CACHE_DIR", filepath.Join(cache, "uv"))
	writeTextFixture(t, root, "目录 with spaces/sample.xml", "<root value = \"x\">text <b>bold</b> tail</root>\n")
	writeTextFixture(t, root, "sample.toml", "value=1\n")
	output, err := textTask(t, root, "fmt:text")
	require.NoError(t, err, output)
	output, err = textTask(t, root, "lint:text")
	require.NoError(t, err, output)
	// 热缓存复用不需要联网；检查仍应执行，不能因缓存命中跳过。
	t.Setenv("npm_config_offline", "true")
	t.Setenv("UV_OFFLINE", "true")
	output, err = textTask(t, root, "lint:text")
	require.NoError(t, err, output)
	for _, name := range []string{"node_modules", ".venv", "package.json", "package-lock.json"} {
		_, err := os.Stat(filepath.Join(root, name))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestTextTaskDownloadFailureKeepsFiles(t *testing.T) {
	root := textFixture(t)
	file := writeTextFixture(t, root, "unchanged.toml", "value=1\n")
	t.Setenv("npm_config_cache", t.TempDir())
	t.Setenv("npm_config_offline", "true")
	output, err := textTask(t, root, "lint:text")
	require.Error(t, err, output)
	require.Equal(t, "value=1\n", string(readTextFixture(t, file)))
}

func readTextFixture(t *testing.T, path string) []byte {
	t.Helper()
	//nolint:gosec // G304：调用者只传入本次测试临时副本中的文件路径，不读取真实用户数据。
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func textFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"mise.toml", ".prettierrc.cjs", ".prettierignore", ".markdownlint-cli2.yaml", ".yamllint.yaml", ".editorconfig", ".gitignore"} {
		require.NoError(t, fixture.CopyFile(filepath.Join("../..", name), filepath.Join(root, name)))
	}
	require.NoError(t, fixture.PrepareGit(root))
	return root
}

func textTask(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	//nolint:gosec // G204：只在本次临时副本执行固定 mise 任务及测试给定的独立参数。
	cmd := exec.CommandContext(t.Context(), "mise", append([]string{"run"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(
		os.Environ(),
		"MISE_TRUSTED_CONFIG_PATHS="+root,
		"MY_HOME="+filepath.Join(root, ".test-home"),
	)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeTextFixture(t *testing.T, root, name, value string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(value), 0o600))
	return path
}
