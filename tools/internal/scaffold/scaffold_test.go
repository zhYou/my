package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitializeMovesCommandDirectory(t *testing.T) {
	root := projectFixture(t, TemplateModule, TemplateName)
	source := `package main
import _ "` + TemplateModule + `/internal/app/cli"
const appName = "` + TemplateName + `"
func main() {}
`
	//nolint:gosec // G306：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
	require.NoError(t, os.WriteFile(filepath.Join(root, "cmd", TemplateName, "main.go"), []byte(source), 0o640))
	changed, err := Initialize(root, "example.com/team/repository", "my-cli")
	require.NoError(t, err)
	require.NotEmpty(t, changed)
	project, err := Inspect(root)
	require.NoError(t, err)
	require.Equal(t, Project{"example.com/team/repository", "my-cli", "cmd/my-cli"}, project)
	require.NoDirExists(t, filepath.Join(root, "cmd", TemplateName))
	//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	data, err := os.ReadFile(filepath.Join(root, "cmd/my-cli/main.go"))
	require.NoError(t, err)
	require.Contains(t, string(data), "example.com/team/repository/internal/app/cli")
	changed, err = Initialize(root, "example.com/team/repository", "my-cli")
	require.NoError(t, err)
	require.Empty(t, changed)
}

func TestInitializeSameNameDoesNotMove(t *testing.T) {
	root := projectFixture(t, TemplateModule, TemplateName)
	_, err := Initialize(root, "example.com/team/repository", TemplateName)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "cmd", TemplateName, "main.go"))
	changed, err := Initialize(root, "example.com/team/repository", TemplateName)
	require.NoError(t, err)
	require.Empty(t, changed)
}

func TestInitializeRejectsGoVersionNamesBeforeWriting(t *testing.T) {
	for _, name := range []string{"v2", "v10", "v999999999999999999999999999999999999999"} {
		t.Run(name, func(t *testing.T) {
			root := projectFixture(t, TemplateModule, TemplateName)
			//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
			before, err := os.ReadFile(filepath.Join(root, "go.mod"))
			require.NoError(t, err)
			_, err = Initialize(root, "example.com/tool", name)
			require.Error(t, err)
			//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
			after, err := os.ReadFile(filepath.Join(root, "go.mod"))
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.NoDirExists(t, filepath.Join(root, "cmd", name))
		})
	}
}

func TestInitializeAcceptanceIdentityWhitelist(t *testing.T) {
	root := projectFixture(t, TemplateModule, TemplateName)
	for _, name := range []string{"tools/internal/acceptance/acceptance.go", "tools/release/release_install.go"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700))
		content := "package main\nimport _ \"" + TemplateModule + "/internal/base/paths\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o600))
	}
	unknown := filepath.Join(root, "user.toml")
	require.NoError(t, os.WriteFile(unknown, []byte("name = \""+TemplateName+"\""), 0o600))
	_, err := Initialize(root, "example.com/team/mycli", "mycli")
	require.NoError(t, err)
	for _, name := range []string{"tools/internal/acceptance/acceptance.go", "tools/release/release_install.go"} {
		//nolint:gosec // G304：仅读取本测试的临时 fixture。
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Contains(t, string(data), "example.com/team/mycli/internal/base/paths")
	}
	//nolint:gosec // G304：仅读取本测试临时目录中的未知文件。
	data, err := os.ReadFile(unknown)
	require.NoError(t, err)
	require.Contains(t, string(data), TemplateName)
}

func TestInitializeRewritesOnlyTemplateFiles(t *testing.T) {
	root := fixture(t)
	before, err := os.Stat(filepath.Join(root, "cmd", TemplateName, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	changed, err := Initialize(root, "github.com/acme/tool", "my-cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 4 {
		t.Fatalf("changed=%v", changed)
	}
	if !strings.HasPrefix(read(t, root, "go.mod"), "module github.com/acme/tool\n") {
		t.Fatal("模块未重命名")
	}
	if !strings.Contains(read(t, root, "main.go"), `appName = "my-cli"`) {
		t.Fatal("程序标识未重命名")
	}
	if got := read(t, root, "README.md"); got != "my-cli github.com/acme/tool MY_CLI_LOG_LEVEL\n" {
		t.Fatalf("README=%q", got)
	}
	if read(t, root, "user.txt") != "go-cli-starter\n" {
		t.Fatal("修改了无关文件")
	}
	info, err := os.Stat(filepath.Join(root, "cmd", "my-cli", "main.go"))
	if err != nil || info.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("文件权限发生变化：%v %v", info, err)
	}
}

func TestInitializeIsIdempotentAndRejectsConflictingIdentity(t *testing.T) {
	root := fixture(t)
	if _, err := Initialize(root, "example.com/team/tool", "tool"); err != nil {
		t.Fatal(err)
	}
	changed, err := Initialize(root, "example.com/team/tool", "tool")
	if err != nil || len(changed) != 0 {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	before := read(t, root, "main.go")
	if _, err := Initialize(root, "example.com/team/other", "other"); err == nil {
		t.Fatal("错误地接受了冲突的重复初始化")
	}
	if read(t, root, "main.go") != before {
		t.Fatal("冲突的初始化修改了项目")
	}
}

func TestInitializeRejectsInvalidInputsBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		module string
		name   string
	}{
		{"", "tool"},
		{"example.com/tool", ""},
		{"example.com/tool", "../tool"},
		{"example.com/tool", "tool;touch bad"},
		{"../tool", "tool"},
		{"example.com/team//tool", "tool"},
		{"example.com/tool\nother", "tool"},
		{"example.com/tool/v1", "tool"},
		{"example.com/tool.", "tool"},
	} {
		root := fixture(t)
		if _, err := Initialize(root, tc.module, tc.name); err == nil {
			t.Errorf("错误地接受了无效输入 %+v", tc)
		}
		if !strings.Contains(read(t, root, "go.mod"), "github.com/zhYou/go-cli-starter") {
			t.Fatal("无效输入修改了模块")
		}
	}
}

func TestInitializeDoesNotRewriteNewModuleSubstring(t *testing.T) {
	root := fixture(t)
	if _, err := Initialize(root, "example.com/go-cli-starter-tools", "tool"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, "go.mod"), "module example.com/go-cli-starter-tools\n") {
		t.Fatal("模块替换发生了级联改写")
	}
}

func TestInitializeKeepsRenamedImportsFormatted(t *testing.T) {
	root := fixture(t)
	source := "package main\n\nimport (\n\t\"github.com/spf13/cobra\"\n\t\"github.com/zhYou/go-cli-starter/internal/app/config\"\n)\n\nconst appName = \"go-cli-starter\"\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(root, "cmd", TemplateName, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(root, "example.com/team/tool", "tool"); err != nil {
		t.Fatal(err)
	}
	wantImports := "import (\n\t\"example.com/team/tool/internal/app/config\"\n\t\"github.com/spf13/cobra\"\n)"
	if got := read(t, root, "main.go"); !strings.Contains(got, wantImports) {
		t.Fatalf("重命名后的导入未格式化：\n%s", got)
	}
}

func TestInitializeRejectsSymlinkBeforeAnyWrites(t *testing.T) {
	root := fixture(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("go-cli-starter"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "README.md")); err != nil {
		t.Skipf("符号链接不可用：%v", err)
	}
	if _, err := Initialize(root, "example.com/tool", "tool"); err == nil {
		t.Fatal("错误地接受了符号链接")
	}
	if !strings.Contains(read(t, root, "go.mod"), "github.com/zhYou/go-cli-starter") {
		t.Fatal("预检失败时仍修改了部分项目文件")
	}
}
