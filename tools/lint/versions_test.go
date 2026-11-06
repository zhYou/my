package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zhYou/my/tools/internal/fixture"
	"github.com/zhYou/my/tools/internal/toolchain"

	"github.com/stretchr/testify/require"
)

func TestVersionsWithoutLock(t *testing.T) {
	require.Error(t, toolchain.Match("go", "go version go1.27.30 linux/amd64", "1.27.3"))
	require.NoError(t, toolchain.Match("go", "go version go1.27.3 linux/amd64", "1.27.3"))
	root := versionFixture(t)
	require.NoError(t, checkVersions(root))
}

func TestVersionsLoadUnifiedTools(t *testing.T) {
	root := versionFixture(t)
	versions, err := toolchain.Load(root)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"go":                                   "1.27.3",
		"golangci-lint":                        "2.15.0",
		"actionlint":                           "1.7.12",
		"goreleaser":                           "2.19.0",
		"go:golang.org/x/vuln/cmd/govulncheck": "1.9.0",
		"syft":                                 "1.55.0",
		"node":                                 "26.13.0",
		"python":                               "3.15.2",
		"uv":                                   "0.13.7",
		"taplo":                                "0.10.0",
		"go:golang.org/x/tools/gopls":          "0.24.0",
	}, versions.Tools)
	require.NoError(t, checkVersions(root))
}

func TestVersionsRejectNonExactToolVersion(t *testing.T) {
	for _, version := range []string{"latest", "0.24", "0.24.0-rc.1", "0.24.0+patched"} {
		t.Run(version, func(t *testing.T) {
			root := versionFixture(t)
			require.NoError(
				t,
				os.WriteFile(
					filepath.Join(root, "mise.toml"),
					[]byte(
						"min_version = \"2026.10.21\"\n[tools]\n\"go:golang.org/x/tools/gopls\" = \""+version+"\"\n",
					),
					0o600,
				),
			)
			require.ErrorContains(t, checkVersions(root), "必须固定稳定的精确版本")
		})
	}
}

func TestVersionsRejectGoplsDocumentationMismatch(t *testing.T) {
	root := versionFixture(t)
	path := filepath.Join(root, "README.md")
	//nolint:gosec // G304：只读取当前测试临时目录内的文档 fixture。
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	//nolint:gosec // G703：只修改当前测试临时目录内的文档 fixture。
	require.NoError(
		t,
		os.WriteFile(
			path,
			[]byte(
				regexp.MustCompile(`(?m)(\|\s*gopls\s*\|\s*)0\.24\.0(\s*\|)`).
					ReplaceAllString(string(data), "${1}0.24.1${2}"),
			),
			0o600,
		),
	)
	require.ErrorContains(t, checkVersions(root), "README 工具 gopls")
}

func TestVersionsRequireTaskVariableBindings(t *testing.T) {
	for _, replacement := range []string{"{{vars.missing_version}}", "3.9.10"} {
		t.Run(replacement, func(t *testing.T) {
			root := versionFixture(t)
			path := filepath.Join(root, "mise.toml")
			//nolint:gosec // G304：只读取本次临时版本检查 fixture。
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			changed := strings.ReplaceAll(string(data), "{{vars.prettier_version}}", replacement)
			require.NotEqual(t, string(data), changed)
			//nolint:gosec // G703：只改写同一临时 fixture 的工具声明。
			require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
			require.Error(t, checkVersions(root))
		})
	}
}

func TestVersionsRejectCIMismatch(t *testing.T) {
	root := versionFixture(t)
	path := filepath.Join(root, ".github/workflows/ci.yml")
	//nolint:gosec // G304：路径仅来自当前测试创建的临时项目，不读取用户文件。
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	//nolint:gosec // G703：只修改临时 fixture 的固定 CI 配置，用于验证版本不一致。
	require.NoError(
		t,
		os.WriteFile(
			path,
			[]byte(strings.ReplaceAll(string(data), "version: 2026.10.21", "version: 2026.10.20")),
			0o600,
		),
	)
	require.ErrorContains(t, checkVersions(root), "CI mise 版本不一致")
}

func TestHistoricalVersionTextIsNotCurrentDeclaration(t *testing.T) {
	root := versionFixture(t)
	path := filepath.Join(root, "docs/superpowers/specs/history.md")
	//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("Go 1.20.0 mise 2020.1.1"), 0o600))
	require.NoError(t, checkVersions(root))
	file := filepath.Join(root, "README.md")
	//nolint:gosec // G304：测试路径和权限只来自当前临时 fixture，不读取用户文件。
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	require.NoError(
		t,
		os.WriteFile(
			file,
			[]byte(
				regexp.MustCompile(`(?m)(\|\s*Go\s*\|\s*)1\.27\.3(\s*\|)`).
					ReplaceAllString(string(data), "${1}1.27.30${2}"),
			),
			0o600,
		),
	)
	require.Error(t, checkVersions(root))
}

func TestVersionsGoToolchain(t *testing.T) {
	tests := []struct {
		name      string
		minimum   string
		toolchain string
		wantError bool
	}{
		{name: "implicit-matching", minimum: "1.27.3"},
		{name: "explicit-matching", minimum: "1.27.3", toolchain: "go1.27.3"},
		{name: "explicit-newer-than-minimum", minimum: "1.27.0", toolchain: "go1.27.3"},
		{name: "implicit-mismatching", minimum: "1.27.0", wantError: true},
		{name: "explicit-mismatching", minimum: "1.27.3", toolchain: "go1.27.4", wantError: true},
		{name: "minimum-too-new", minimum: "1.27.4", toolchain: "go1.27.3", wantError: true},
		{name: "minimum-different-series", minimum: "1.26.0", toolchain: "go1.27.3", wantError: true},
		{name: "missing-minimum", toolchain: "go1.27.3", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := versionFixture(t)
			module := "module example.com/versions\n"
			if tt.minimum != "" {
				module += "\ngo " + tt.minimum + "\n"
			}
			if tt.toolchain != "" {
				module += "\ntoolchain " + tt.toolchain + "\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(module), 0o600))
			err := checkVersions(root)
			if tt.wantError {
				require.ErrorContains(t, err, "模块最低 Go 版本、工具链与 mise 不一致")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func versionFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"mise.toml", "go.mod", "README.md", "AGENTS.md", ".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		dest := filepath.Join(root, name)
		//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
		require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
		require.NoError(t, fixture.CopyFile(filepath.Join("../..", name), dest))
	}
	return root
}
