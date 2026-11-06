package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zhYou/my/tools/internal/fixture"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/tools/internal/scaffold"
)

func TestTemplateLintUsesCopiedTaskAndPropagatesFailure(t *testing.T) {
	suffix := ""
	if runtime.GOOS == windowsPlatform {
		suffix = executableSuffix
	}
	globalConfig := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(globalConfig, nil, 0o600))
	t.Setenv("MISE_GLOBAL_CONFIG_FILE", globalConfig)
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	goCommand := exec.Command("mise", "which", "go")
	goCommand.Env = append(os.Environ(), "MISE_TRUSTED_CONFIG_PATHS="+projectRoot)
	var goErrors strings.Builder
	goCommand.Stderr = &goErrors
	goPath, err := goCommand.Output()
	require.NoError(t, err, "%s", goErrors.String())
	goExecutable := strings.TrimSpace(string(goPath))
	probeName := "lint-probe" + suffix
	t.Setenv("MISE_TRUSTED_CONFIG_PATHS", t.TempDir())
	parentTrust := os.Getenv("MISE_TRUSTED_CONFIG_PATHS")
	for _, code := range []int{0, 7} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := t.TempDir()
			report := filepath.Join(dir, "lint-report")
			t.Setenv("LINT_FIXTURE_REPORT", report)
			config := fmt.Sprintf(
				"[settings]\nauto_install = false\nexec_auto_install = false\ntask.run_auto_install = false\n[tasks.lint]\nrun = %q\n",
				filepath.FromSlash("./"+probeName),
			)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "mise.toml"), []byte(config), 0o600))
			source := fmt.Sprintf(`package main
import "os"
func main() {
 if err := os.WriteFile(os.Getenv("LINT_FIXTURE_REPORT"), []byte(os.Getenv("LINTFIXTURE_HOME")+"\n"+os.Getenv("MISE_TRUSTED_CONFIG_PATHS")), 0600); err != nil { panic(err) }
 os.Exit(%d)
}
`, code)
			probeSource := filepath.Join(dir, "probe.go")
			require.NoError(t, os.WriteFile(probeSource, []byte(source), 0o600))
			// 先用项目 mise 解析的 Go 预编译，避免临时任务混用宿主 Go 与继承的 GOROOT。
			//nolint:gosec // G204：Go 路径由项目 mise 解析，源码和产物均位于测试临时目录。
			cmd := exec.Command(goExecutable, "build", "-o", filepath.Join(dir, probeName), probeSource)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			err = checkTemplateLint(dir, "lintfixture")
			if code == 0 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "run lint")
			}
			//nolint:gosec // G304：只读取当前临时副本中固定名称的 lint 验收报告。
			data, err := os.ReadFile(report)
			require.NoError(t, err)
			require.Equal(t, filepath.Join(dir, ".template-check-home")+"\n"+dir, string(data))
			require.Equal(t, parentTrust, os.Getenv("MISE_TRUSTED_CONFIG_PATHS"))
		})
	}
}

func TestCopiedRepositorySupportsGoVCSStamping(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":              "module example.com/fixture\n\ngo 1.27.0\n",
		"cmd/fixture/main.go": "package main\nconst appName=\"fixture\"\nfunc main(){}\n",
	} {
		//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.PrepareGit(dir); err != nil {
		t.Fatal(err)
	}
	remote := exec.Command("git", "remote", "get-url", "origin")
	remote.Dir = dir
	if output, err := remote.Output(); err != nil || len(output) == 0 {
		t.Fatalf("测试仓库缺少发布校验所需的 SCM 标识：%v", err)
	}
	//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "fixture"), "./cmd/fixture")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("独立构建失败：%v\n%s", err, output)
	}
}

func TestTemplateMetadataAndGuidesSurviveRename(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module " + scaffold.TemplateModule + "\n\ngo 1.27.0\n", "cmd/" + scaffold.TemplateName + "/main.go": "package main\nconst appName = \"" + scaffold.TemplateName + "\"\nfunc main(){}\n", ".template-version": "0.1.0\n"} {
		//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600))
	}
	for _, name := range []string{"SECURITY.md", ".github/PULL_REQUEST_TEMPLATE.md", ".github/CODEOWNERS.example", "docs/backlog.md", "docs/maintenance.md"} {
		//nolint:gosec // G301：测试路径和权限只来自当前临时 fixture，不读取用户文件。
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, fixture.CopyFile(filepath.Join("../..", name), filepath.Join(dir, name)))
	}
	//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))
	require.NoError(
		t,
		fixture.CopyFile("../../docs/template-upgrades.md", filepath.Join(dir, "docs/template-upgrades.md")),
	)
	_, err := scaffold.Initialize(dir, "example.com/copied/tool", "copied")
	require.NoError(t, err)
	origin, err := scaffold.ReadOrigin(afero.NewOsFs(), dir)
	require.NoError(t, err)
	require.Equal(t, scaffold.TemplateModule, origin.Template)
	require.Equal(t, "example.com/copied/tool", origin.Module)
	require.FileExists(t, filepath.Join(dir, "docs/template-upgrades.md"))
	require.NoError(t, validateTemplateMetadata(dir))

	backlogPath := filepath.Join(dir, "docs/backlog.md")
	require.FileExists(t, backlogPath)
	require.NoError(t, os.Remove(backlogPath))
	require.ErrorIs(t, validateTemplateMetadata(dir), os.ErrNotExist)
}

func TestInstallVersionLikeCommandNames(t *testing.T) {
	for _, name := range []string{"v0", "v1", "v02"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(
				t,
				os.WriteFile(
					filepath.Join(root, "go.mod"),
					[]byte("module example.com/installfixture\n\ngo 1.27.0\n"),
					0o600,
				),
			)
			dir := filepath.Join(root, "cmd", name)
			//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(
				t,
				os.WriteFile(
					filepath.Join(dir, "main.go"),
					[]byte("package main\nconst appName=\""+name+"\"\nfunc main(){}\n"),
					0o600,
				),
			)
			require.NoError(t, fixture.PrepareGit(root))
			project, err := scaffold.Inspect(root)
			require.NoError(t, err)
			require.NoError(t, verifyCommandInstall(root, project))
		})
	}
}

func TestTemplateExecutablePath(t *testing.T) {
	for _, tc := range []struct {
		platform string
		name     string
	}{
		{platform: "linux", name: "app-check"},
		{platform: "darwin", name: "app-check"},
		{platform: "windows", name: "app-check.exe"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			dir := t.TempDir()
			require.Equal(t, filepath.Join(dir, tc.name), templateExecutablePath(dir, tc.platform))
		})
	}
}

// TestTemplateExecutableAcceptance 直接调用模板任务的构建与执行边界，避免测试自行补齐扩展名。
func TestTemplateExecutableAcceptance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MY_HOME", filepath.Join(dir, "starter-home"))
	t.Setenv("MYCLI_HOME", filepath.Join(dir, "mycli-home"))
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	files, err := cmd.Output()
	require.NoError(t, err)
	for name := range strings.SplitSeq(string(files), "\x00") {
		if name != "" {
			require.NoError(t, fixture.CopyFile(filepath.Join(root, name), filepath.Join(dir, name)))
		}
	}
	module, name, err := scaffold.Identity(dir)
	require.NoError(t, err)
	if module == scaffold.TemplateModule && name == scaffold.TemplateName {
		module, name = "example.com/template-check/mycli", "mycli"
	}
	_, err = scaffold.Initialize(dir, module, name)
	require.NoError(t, err)
	require.NoError(t, fixture.PrepareGit(dir))
	project, err := scaffold.Inspect(dir)
	require.NoError(t, err)
	require.NoError(t, verifyTemplateExecutable(dir, project))
}
