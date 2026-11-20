package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentRulesRepositoryPasses(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	require.NoError(t, checkAgentRules("."))
}

func TestAgentRulesRejectsInvalidJustifications(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		want    string
	}{
		{"English", `prefix_rule(justification = "Run tests", decision = "allow")` + "\n", "中文"},
		{"missing", `prefix_rule(decision = "allow")` + "\n", "缺少 justification"},
		{"comment decoy", `# justification = "注释不是规则"` + "\n", "缺少 justification"},
		{"unquoted", `prefix_rule(justification = run)` + "\n", "无效"},
		{"broken", `prefix_rule(justification = "运行测试)` + "\n", "无效"},
		{"unbalanced", "prefix_rule(\n    justification = \"运行测试\",\n    decision = \"allow\"\n", "括号"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, rulesDir := agentRulesFixture(t)
			require.NoError(t, os.WriteFile(filepath.Join(rulesDir, "fixture.rules"), []byte(tt.content), 0o600))

			require.ErrorContains(t, checkAgentRules(root), tt.want)
		})
	}
}

func TestAgentRulesRejectsSymlinkedRule(t *testing.T) {
	root, rulesDir := agentRulesFixture(t)
	target := filepath.Join(t.TempDir(), "target.rules")
	require.NoError(t, os.WriteFile(target, []byte(`prefix_rule(justification = "运行测试", decision = "allow")`), 0o600))
	if err := os.Symlink(target, filepath.Join(rulesDir, "linked.rules")); err != nil {
		t.Skipf("当前系统无法创建符号链接：%v", err)
	}

	require.ErrorContains(t, checkAgentRules(root), "不是普通文件")
}

func agentRulesFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	rulesDir := filepath.Join(root, ".codex", "rules")
	require.NoError(t, os.MkdirAll(rulesDir, 0o750))
	return root, rulesDir
}
