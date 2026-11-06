package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zhYou/my/tools/internal/acceptance"
	"github.com/zhYou/my/tools/internal/fixture"
	"github.com/zhYou/my/tools/internal/toolrun"

	"github.com/spf13/afero"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/tools/internal/scaffold"
)

func verifyTemplate() (resultErr error) {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "cli-template-check-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(dir))
	}()
	// 利用 Git 文件列表排除本地凭据、缓存和构建产物。
	cmd := exec.Command(gitProgram, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	files, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("列出模板文件失败（请在 Git 仓库中运行）：%w", err)
	}
	for name := range strings.SplitSeq(string(files), "\x00") {
		if name == "" || name == ".template-init.lock" || strings.HasPrefix(name, ".template-init/") {
			continue
		}
		if err := fixture.CopyFile(filepath.Join(root, name), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	module, name, err := scaffold.Identity(dir)
	if err != nil {
		return err
	}
	if module == scaffold.TemplateModule && name == scaffold.TemplateName {
		module, name = "example.com/template-check/mycli", "mycli"
	}
	if _, err := scaffold.Initialize(dir, module, name); err != nil {
		return err
	}
	if changed, err := scaffold.Initialize(dir, module, name); err != nil || len(changed) != 0 {
		if err != nil {
			return fmt.Errorf("初始化不满足幂等性：%w", err)
		}
		return fmt.Errorf("初始化不满足幂等性：重复调用仍改写文件")
	}
	project, err := scaffold.Inspect(dir)
	if err != nil {
		return err
	}
	if err := validateTemplateMetadata(dir); err != nil {
		return err
	}
	if err := fixture.PrepareGit(dir); err != nil {
		return err
	}
	// 子进程使用生成应用的前缀，原模板的 HOME 不会被改名后的程序读取。
	if err := toolrun.Run(
		dir,
		[]string{paths.EnvPrefix(project.Name) + "_HOME=" + filepath.Join(dir, templateCheckHome)},
		"go",
		"test",
		allPackages,
	); err != nil {
		return err
	}
	if err := checkTemplateLint(dir, project.Name); err != nil {
		return err
	}
	if err := toolrun.Run(
		dir,
		[]string{paths.EnvPrefix(project.Name) + "_HOME=" + filepath.Join(dir, templateCheckHome)},
		"go",
		"run",
		"./tools/lint",
		"test",
	); err != nil {
		return err
	}
	//nolint:gosec // G204：开发任务显式执行固定工具及拆分参数，不经过 shell；参数来自仓库或调用者。
	check := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "./"+project.CommandPath)
	check.Dir = dir
	imports, err := check.Output()
	if err != nil {
		return err
	}
	if module != scaffold.TemplateModule && strings.Contains(string(imports), scaffold.TemplateModule) {
		return fmt.Errorf("生成的项目仍在导入原模板模块")
	}
	if err := verifyTemplateExecutable(dir, project); err != nil {
		return err
	}
	if err := verifyCommandInstall(dir, project); err != nil {
		return err
	}
	if err := toolrun.Run(dir, nil, releaseProgram, "check"); err != nil {
		return err
	}
	fmt.Println("模板副本检查通过：初始化、幂等性、测试、lint、构建、双语契约、TOML/环境、go install 和发布配置。")
	return nil
}

func validateTemplateMetadata(dir string) error {
	origin, err := scaffold.ReadOrigin(afero.NewOsFs(), dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	module, name, err := scaffold.Identity(dir)
	if err != nil {
		return err
	}
	if origin.Module != module || origin.Name != name {
		return fmt.Errorf("来源记录与项目标识不一致")
	}
	for _, name := range []string{"docs/template-upgrades.md", "docs/maintenance.md", "SECURITY.md", ".github/PULL_REQUEST_TEMPLATE.md", ".github/CODEOWNERS.example", "docs/backlog.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

func verifyCommandInstall(root string, project scaffold.Project) (resultErr error) {
	installDir, err := os.MkdirTemp("", "cli-install-check-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(installDir))
	}()
	//nolint:gosec // G204：开发任务显式执行固定工具及拆分参数，不经过 shell；参数来自仓库或调用者。
	cmd := exec.Command("go", "install", "./"+project.CommandPath)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOBIN="+installDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("安装命令失败：%w：%s", err, out)
	}
	name := project.Name
	if runtime.GOOS == windowsPlatform {
		name += executableSuffix
	}
	if _, err := os.Stat(filepath.Join(root, configExample)); err == nil {
		return acceptance.Verify(
			filepath.Join(installDir, name),
			installDir,
			project.Name,
			filepath.Join(root, configExample),
			developmentVersion,
			developmentCommit,
		)
	}
	return toolrun.Run(
		root,
		[]string{paths.EnvPrefix(project.Name) + "_HOME=" + filepath.Join(installDir, "home")},
		filepath.Join(installDir, name),
		schemaCommand,
		greetCommand,
	)
}

func templateExecutablePath(dir, platform string) string {
	name := "app-check"
	if platform == windowsPlatform {
		name += executableSuffix
	}
	return filepath.Join(dir, name)
}

func verifyTemplateExecutable(dir string, project scaffold.Project) error {
	executable := templateExecutablePath(dir, runtime.GOOS)
	if err := toolrun.Run(dir, nil, "go", buildCommand, "-o", executable, "./"+project.CommandPath); err != nil {
		return err
	}
	return acceptance.Verify(
		executable,
		dir,
		project.Name,
		filepath.Join(dir, configExample),
		developmentVersion,
		developmentCommit,
	)
}

// checkTemplateLint 复用副本自己的 lint 流程；信任与应用 HOME 只作用于本次子进程。
func checkTemplateLint(dir, name string) error {
	return toolrun.Run(dir, []string{
		"MISE_TRUSTED_CONFIG_PATHS=" + dir,
		paths.EnvPrefix(name) + "_HOME=" + filepath.Join(dir, templateCheckHome),
	}, "mise", "run", "lint")
}
