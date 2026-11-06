package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zhYou/my/tools/internal/toolchain"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

func checkVersions(root string) error {
	versions, err := toolchain.Load(root)
	if err != nil {
		return err
	}
	if err := checkVersionBindings(root, versions.Variables); err != nil {
		return err
	}
	//nolint:gosec // G304：开发工具读取显式项目文件或执行固定 Git 子命令，不经过 shell，不是外部服务入口。
	data, err := os.ReadFile(filepath.Join(root, moduleFile))
	if err != nil {
		return err
	}
	module, err := modfile.Parse(moduleFile, data, nil)
	if err != nil {
		return err
	}
	goVersion := versions.Tools["go"]
	goToolchain := ""
	if module.Toolchain != nil {
		goToolchain = module.Toolchain.Name
	} else if module.Go != nil {
		// 未显式声明时，Go 使用与 go 指令同版本的隐式工具链。
		goToolchain = "go" + module.Go.Version
	}
	if module.Go == nil || goToolchain != "go"+goVersion || semver.Compare("v"+module.Go.Version, "v"+goVersion) > 0 ||
		semver.MajorMinor("v"+module.Go.Version) != semver.MajorMinor("v"+goVersion) {
		return fmt.Errorf("模块最低 Go 版本、工具链与 mise 不一致")
	}
	//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
	data, err = os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return err
	}
	readme := string(data)
	start := strings.Index(readme, "<!-- tool-versions:start -->")
	end := strings.Index(readme, "<!-- tool-versions:end -->")
	if start < 0 || end <= start {
		return fmt.Errorf("README 缺少当前工具版本受管表")
	}
	table := map[string]string{}
	for line := range strings.SplitSeq(readme[start:end], "\n") {
		cells := strings.Split(line, "|")
		if len(cells) == 4 {
			table[strings.TrimSpace(cells[1])] = strings.TrimSpace(cells[2])
		}
	}
	expected := map[string]string{"mise（本地最低版本、CI 固定版本）": versions.MiseMinimum}
	for key, want := range versions.Tools {
		spec, err := toolchain.Spec(key)
		if err != nil {
			return err
		}
		expected[spec.Display] = want
	}
	for key, want := range versions.Variables {
		label, err := toolchain.VariableLabel(key)
		if err != nil {
			return err
		}
		expected[label] = want
	}
	for key, want := range expected {
		if table[key] != want {
			return fmt.Errorf("README 工具 %s 的当前声明不一致", key)
		}
	}
	//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
	data, err = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return err
	}
	match := regexp.MustCompile(`mise ([0-9]+\.[0-9]+\.[0-9]+) 或更新版本`).FindSubmatch(data)
	if len(match) != 2 || string(match[1]) != versions.MiseMinimum {
		return fmt.Errorf("AGENTS 的 mise 最低版本不一致")
	}
	paths, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.y*ml"))
	if err != nil {
		return err
	}
	count := 0
	for _, path := range paths {
		//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var workflow struct {
			Jobs map[string]struct {
				Steps []struct {
					Uses string            `yaml:"uses"`
					With map[string]string `yaml:"with"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &workflow); err != nil {
			return err
		}
		for _, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "jdx/mise-action@") {
					count++
					if step.With[miseActionVersionField] != versions.MiseMinimum {
						return fmt.Errorf("%s 的 CI mise 版本不一致", path)
					}
				}
			}
		}
	}
	if count == 0 {
		return fmt.Errorf("未找到 CI mise 引导声明")
	}
	return nil
}

func checkVersionBindings(root string, variables map[string]string) error {
	//nolint:gosec // G304：只读取调用者指定 checkout 的固定工具配置。
	data, err := os.ReadFile(filepath.Join(root, "mise.toml"))
	if err != nil {
		return err
	}
	var config struct {
		Tasks map[string]any `toml:"tasks"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return err
	}
	tasks, err := toml.Marshal(config.Tasks)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, match := range regexp.MustCompile(`\{\{\s*vars\.([a-z_]+)\s*\}\}`).FindAllSubmatch(tasks, -1) {
		key := string(match[1])
		if _, exists := variables[key]; !exists {
			return fmt.Errorf("任务引用了未声明的版本变量 %s", key)
		}
		used[key] = true
	}
	for key := range variables {
		if !used[key] {
			return fmt.Errorf("版本变量 %s 未被任务引用", key)
		}
	}
	return nil
}
