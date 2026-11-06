package toolchain

import (
	"debug/buildinfo"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/semver"
)

// VersionSet 保存项目统一工具清单的固定版本和 mise 最低版本。
type VersionSet struct {
	MiseMinimum string
	Tools       map[string]string
	Variables   map[string]string
}

// Load 读取并校验 mise 中的准确工具版本。
func Load(root string) (VersionSet, error) {
	//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
	data, err := os.ReadFile(filepath.Join(root, miseConfig))
	if err != nil {
		return VersionSet{}, err
	}
	var cfg struct {
		Minimum string            `toml:"min_version"`
		Tools   map[string]string `toml:"tools"`
		Vars    map[string]string `toml:"vars"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return VersionSet{}, err
	}
	v := VersionSet{MiseMinimum: cfg.Minimum, Tools: cfg.Tools, Variables: cfg.Vars}
	if !regexp.MustCompile(exactVersionPattern).MatchString(v.MiseMinimum) {
		return v, fmt.Errorf("mise 最低版本必须精确固定")
	}
	for key, version := range v.Tools {
		if !regexp.MustCompile(exactVersionPattern).MatchString(version) {
			return v, fmt.Errorf("工具 %s 必须固定稳定的精确版本", key)
		}
	}
	for key, version := range v.Variables {
		if !regexp.MustCompile(exactVersionPattern).MatchString(version) {
			return v, fmt.Errorf("工具变量 %s 必须固定稳定的精确版本", key)
		}
		if _, err := VariableLabel(key); err != nil {
			return v, err
		}
	}
	return v, nil
}

// VariableLabel 返回按需执行的工具或独立运行时在文档版本表中的名称。
func VariableLabel(key string) (string, error) {
	labels := map[string]string{
		"prettier_version":         "Prettier",
		"xml_plugin_version":       "@prettier/plugin-xml",
		"toml_plugin_version":      "prettier-plugin-toml",
		"markdownlint_version":     "markdownlint-cli2",
		"yamllint_version":         "yamllint",
		"pre_commit_hooks_version": "pre-commit-hooks",
		"serena_version":           "Serena",
		"serena_python_version":    "Serena Python",
	}
	label, ok := labels[key]
	if !ok {
		return "", fmt.Errorf("未知的工具版本变量 %s", key)
	}
	return label, nil
}

// ToolSpec 描述真实工具的版本探测命令与输出格式。
type ToolSpec struct {
	Name    string
	Display string
	Pattern string
	Args    []string
}

// Spec 返回工具的离线版本探测规格。
func Spec(key string) (ToolSpec, error) {
	switch key {
	case miseProgram:
		return ToolSpec{
			miseProgram,
			miseProgram,
			plainVersionPattern,
			[]string{versionFlag},
		}, nil
	case "go":
		return ToolSpec{"go", "Go", `\bgo([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`, []string{versionCommand}}, nil
	case goplsTool:
		return ToolSpec{
			goplsProgram,
			goplsProgram,
			`(?m)^golang.org/x/tools/gopls v([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`,
			[]string{versionCommand},
		}, nil
	case lintProgram:
		return ToolSpec{
			lintProgram,
			lintProgram,
			`\bgolangci-lint has version ([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`,
			[]string{versionCommand},
		}, nil
	case actionlintProgram:
		return ToolSpec{
			actionlintProgram,
			actionlintProgram,
			plainVersionPattern,
			[]string{"-version"},
		}, nil
	case releaseProgram:
		return ToolSpec{
			releaseProgram,
			"GoReleaser",
			`(?m)^GitVersion:\s+v?([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`,
			[]string{versionFlag},
		}, nil
	case govulncheckTool:
		return ToolSpec{
			govulncheckProgram,
			govulncheckProgram,
			`(?m)^Scanner:\s+govulncheck@v([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`,
			[]string{"-h"},
		}, nil
	case "node":
		return ToolSpec{"node", "Node.js", `(?m)^v([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`, []string{versionFlag}}, nil
	case "python":
		return ToolSpec{
			"python",
			"Python",
			`(?m)^Python ([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`,
			[]string{versionFlag},
		}, nil
	case "uv":
		return ToolSpec{"uv", "uv", `(?m)^uv ([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`, []string{versionFlag}}, nil
	case "taplo":
		return ToolSpec{
			"taplo",
			"Taplo",
			`(?m)^taplo ([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`,
			[]string{versionFlag},
		}, nil
	case "syft":
		return ToolSpec{
			"syft",
			"Syft",
			`(?m)^Version:\s+v?([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`,
			[]string{versionCommand},
		}, nil
	default:
		return ToolSpec{}, fmt.Errorf("缺少工具 %s 的版本提取器", key)
	}
}

// Match 校验探测输出是否满足固定版本或 mise 最低版本。
func Match(key, output, want string) error {
	spec, err := Spec(key)
	if err != nil {
		return err
	}
	match := regexp.MustCompile(spec.Pattern).FindStringSubmatch(output)
	if key == miseProgram {
		if len(match) != 2 || semver.Compare("v"+match[1], "v"+want) < 0 {
			return fmt.Errorf("mise 版本无效或低于最低版本 %s", want)
		}
		return nil
	}
	if len(match) != 2 || match[1] != want {
		return fmt.Errorf("%s 与固定版本 %s 不符，请运行 mise run setup", spec.Name, want)
	}
	return nil
}

// SortedTools 按稳定顺序返回工具声明的键。
func SortedTools(v VersionSet) []string {
	keys := slices.Collect(func(yield func(string) bool) {
		for key := range v.Tools {
			if !yield(key) {
				return
			}
		}
	})
	sort.Strings(keys)
	return keys
}

// MatchGovulncheckBuildInfo 离线核验真实 Go 可执行文件，避免 -version 访问漏洞库。
func MatchGovulncheckBuildInfo(info *buildinfo.BuildInfo, want string) error {
	if info.Path != "golang.org/x/vuln/cmd/govulncheck" || info.Main.Path != "golang.org/x/vuln" ||
		info.Main.Version != "v"+want ||
		info.Main.Replace != nil {
		return fmt.Errorf("govulncheck 与固定来源和版本 %s 不符，请运行 mise run setup", want)
	}
	return nil
}
