package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAllowsOnlyUnmanagedToolsAndFixtureData(t *testing.T) {
	root := fixtureRoot(t)
	for name, contents := range map[string]string{
		"configs/nested/production.toml":     "logging.level = \"debug\"\n",
		"configs/testdata/invalid.json":      "{",
		"configs/testdata/invalid.toml":      "invalid = [",
		"internal/locales/catalog.go":        "package locales\n",
		"internal/locales/active.en.toml":    "[\"message.id\"]\nother = \"Hello\"\n",
		"internal/locales/testdata/bad.yaml": "not: [valid",
		"mise.toml":                          "not valid TOML = [",
		".codex/config.toml":                 "not valid TOML = [",
		".github/workflows/ci.yml":           "invalid: [yaml",
		".golangci.yml":                      "invalid: [yaml",
		"renovate.json":                      "{",
		"test/testdata/contracts/v1.json":    "{",
		"internal/business/data.json":        "{",
	} {
		writeFixture(t, root, name, contents)
	}
	t.Setenv("MY_LOGGING_LEVEL", "not-a-level")
	t.Setenv("MY_HOME", "not-an-absolute-path")
	if err := checkConfig(root); err != nil {
		t.Fatalf("受管 TOML、工具配置和测试数据应通过: %v", err)
	}
}

func TestCheckAllowsMissingLocalesDuringTransition(t *testing.T) {
	if err := checkConfig(fixtureRoot(t)); err != nil {
		t.Fatalf("过渡期间允许缺少集中语言目录: %v", err)
	}
}

func TestCheckRejectsInvalidManagedFiles(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		contents string
	}{
		{"应用 JSON", "configs/extra/app.json", "{}"},
		{"应用 YAML", "configs/app.yaml", "name: value"},
		{"应用 YML 大写", "configs/APP.YML", "name: value"},
		{"语言 JSON", "internal/locales/active.en.json", "{}"},
		{"语言 YAML", "internal/locales/nested/active.en.yaml", "x: y"},
		{"根配置 JSON", "config.json", "{}"},
		{"根示例 YML", "config.example.yml", "x: y"},
		{"未支持应用格式", "configs/app.ini", "[logging]"},
		{"未支持语言格式", "internal/locales/active.en.xml", "<message/>"},
		{"嵌套 testdata 并非豁免", "configs/nested/testdata/app.json", "{}"},
		{"应用 TOML 语法", "configs/nested/bad.toml", "[logging\n"},
		{"应用未知键", "configs/nested/bad.toml", "unknown = true\n"},
		{"应用弱类型", "configs/nested/bad.toml", "[diagnostics]\nmax_size_mib = \"10\"\n"},
		{"应用范围", "configs/nested/bad.toml", "[logging]\nlevel = \"invalid\"\n"},
		{"应用字面点号键", "configs/nested/bad.toml", "\"logging.level\" = \"info\"\n"},
		{"根 TOML 内容", "config.toml", "[logging]\nlevel = \"invalid\"\n"},
		{"语言 TOML 语法", "internal/locales/active.en.toml", "[\"message.id\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := fixtureRoot(t)
			writeFixture(t, root, tt.path, tt.contents)
			t.Setenv("MY_LOGGING_LEVEL", "info")
			err := checkConfig(root)
			if err == nil {
				t.Fatalf("应拒绝 %s", tt.path)
			}
			if !strings.Contains(err.Error(), filepath.FromSlash(tt.path)) {
				t.Fatalf("错误应指出受影响路径 %s: %v", tt.path, err)
			}
		})
	}
}

func TestCheckRejectsMissingManagedExample(t *testing.T) {
	root := t.TempDir()
	if err := checkConfig(root); err == nil {
		t.Fatal("应拒绝缺少受管配置示例的工作区")
	}
}

func TestCheckRejectsManagedSymlink(t *testing.T) {
	root := fixtureRoot(t)
	path := filepath.Join(root, "configs", "linked.toml")
	if err := os.Symlink(filepath.Join(root, "configs", "config.example.toml"), path); err != nil {
		t.Skipf("当前系统无法创建符号链接: %v", err)
	}
	if err := checkConfig(root); err == nil || !strings.Contains(err.Error(), "linked.toml") {
		t.Fatalf("应拒绝受管符号链接并指出路径: %v", err)
	}
}

func TestCheckAcceptsRepository(t *testing.T) {
	if err := checkConfig(filepath.Join("..", "..")); err != nil {
		t.Fatalf("当前仓库应符合应用配置策略: %v", err)
	}
}

func writeFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, root, "configs/config.example.toml", "[logging]\nlevel = \"info\"\n")
	return root
}
