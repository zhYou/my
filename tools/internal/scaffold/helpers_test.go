package scaffold

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func commandTransactionFixture(t *testing.T) (*Initializer, string, []change, *commandMove) {
	t.Helper()
	i, root := memoryInitializer(t)
	from := "cmd/" + TemplateName
	require.NoError(t, i.FS.MkdirAll(filepath.Join(root, from), 0o755))
	source := []byte("package main\nconst appName=\"" + TemplateName + "\"\nfunc main(){}\n")
	require.NoError(t, afero.WriteFile(i.FS, filepath.Join(root, from, "main.go"), source, 0o640))
	require.NoError(
		t,
		afero.WriteFile(i.FS, filepath.Join(root, from, "main_test.go"), []byte("package main\n"), 0o600),
	)
	doc := []byte("// Command " + TemplateName + " 提供入口。\npackage main\n")
	require.NoError(t, afero.WriteFile(i.FS, filepath.Join(root, from, "doc.go"), doc, 0o600))
	changes := []change{
		{filepath.Join(root, from, "doc.go"), []byte(strings.ReplaceAll(string(doc), TemplateName, "my-cli")), 0o600},
		{filepath.Join(root, "go.mod"), []byte("module example.com/team/repository\n\ngo 1.27.0\n"), 0o600},
		{
			filepath.Join(root, from, "main.go"),
			[]byte(strings.ReplaceAll(string(source), TemplateName, "my-cli")),
			0o640,
		},
	}
	return i, root, changes, &commandMove{From: from, To: "cmd/my-cli"}
}

func crashCommandMove(t *testing.T, point string) (*Initializer, string, *commandMove) {
	t.Helper()
	i, root, changes, move := commandTransactionFixture(t)
	base := i.FS
	i.FS = faultFS{
		Fs: base,
		rename: func(a, b string) error {
			if point == "before" && a == filepath.Join(root, move.From) {
				panic("移动前中断")
			}
			err := base.Rename(a, b)
			if err == nil && point == "after" && b == filepath.Join(root, move.To) {
				panic("移动后中断")
			}
			if err == nil && point == "committed" && b == filepath.Join(root, journalDir, "journal.json") {
				raw, err := afero.ReadFile(base, b)
				require.NoError(t, err)
				var tx transaction
				require.NoError(t, json.Unmarshal(raw, &tx))
				if tx.Phase == "committed" {
					panic("提交后中断")
				}
			}
			return err
		},
	}
	require.Panics(t, func() {
		if _, err := i.apply(
			context.Background(),
			root,
			"example.com/team/repository",
			"my-cli",
			changes,
			move,
		); err != nil {
			t.Error(err)
		}
	})
	i.FS = base
	return i, root, move
}

func projectFixture(t *testing.T, module, name string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+module+"\n\ngo 1.27.0\n"), 0o600))
	dir := filepath.Join(root, "cmd", name)
	//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(dir, "main.go"),
			[]byte("package main\nconst appName = \""+name+"\"\nfunc main() {}\n"),
			0o600,
		),
	)
	return root
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                           "module github.com/zhYou/go-cli-starter\n\ngo 1.27.0\n",
		"cmd/" + TemplateName + "/main.go": "package main\nconst appName = \"go-cli-starter\"\nfunc main() {}\n",
		"README.md":                        "go-cli-starter github.com/zhYou/go-cli-starter GO_CLI_STARTER_LOG_LEVEL\n",
		"user.txt":                         "go-cli-starter\n",
	}
	for name, content := range files {
		//nolint:gosec // G301：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	if name == "main.go" {
		matches, err := filepath.Glob(filepath.Join(root, "cmd", "*", "main.go"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("查找测试入口失败：%v %v", matches, err)
		}
		name, err = filepath.Rel(root, matches[0])
		require.NoError(t, err)
	}
	//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type faultFS struct {
	afero.Fs
	rename func(string, string) error
}

func (f faultFS) Rename(a, b string) error {
	if f.rename != nil {
		return f.rename(a, b)
	}
	return f.Fs.Rename(a, b)
}

func memoryInitializer(t *testing.T) (*Initializer, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	root, err := filepath.Abs(filepath.Join(t.TempDir(), "repo"))
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(root, 0o755))
	for n, s := range map[string]string{"go.mod": "module " + TemplateModule + "\n\ngo 1.27.0\n", "cmd/" + TemplateName + "/main.go": "package main\nconst appName = \"" + TemplateName + "\"\nfunc main(){}\n", "README.md": TemplateName} {
		require.NoError(t, fs.MkdirAll(filepath.Dir(filepath.Join(root, n)), 0o755))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(root, n), []byte(s), 0o600))
	}
	return &Initializer{
		FS: fs,
		RunCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, nil
		},
		Lock: func(string) (func() error, error) {
			return func() error {
				return nil
			}, nil
		},
	}, root
}

func crashedTransaction(t *testing.T) (*Initializer, string) {
	t.Helper()
	i, root := memoryInitializer(t)
	base := i.FS
	i.FS = faultFS{
		Fs: base,
		rename: func(a, b string) error {
			err := base.Rename(a, b)
			if err == nil && b == filepath.Join(root, "go.mod") {
				panic("模拟进程中断")
			}
			return err
		},
	}
	require.Panics(t, func() {
		_, err := i.Initialize(context.Background(), root, "example.com/tool", "tool")
		require.NoError(t, err)
	})
	i.FS = base
	return i, root
}
