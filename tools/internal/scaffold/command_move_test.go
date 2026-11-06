package scaffold

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCommandMoveHardExitHelper(t *testing.T) {
	if os.Getenv("LAYOUT_HARD_EXIT_HELPER") != "1" {
		return
	}
	root := os.Getenv("LAYOUT_HARD_EXIT_ROOT")
	point := os.Getenv("LAYOUT_HARD_EXIT_POINT")
	base := afero.NewOsFs()
	failed := false
	fs := faultFS{
		Fs: base,
		rename: func(a, b string) error {
			if point == "rollback" && a == filepath.Join(root, "cmd", TemplateName) &&
				b == filepath.Join(root, "cmd/my-cli") {
				failed = true
				return errors.New("触发回滚")
			}
			if b == filepath.Join(root, "cmd", TemplateName, "main.go") &&
				(point == "apply" || point == "same-name" || failed) {
				os.Exit(99)
			}
			return base.Rename(a, b)
		},
	}
	i := Initializer{FS: fs}
	name := "my-cli"
	if point == "same-name" {
		name = TemplateName
	}
	if _, err := i.Initialize(context.Background(), root, "example.com/tool", name); err != nil {
		t.Error(err)
	}
	t.Fatal("未触发进程退出")
}

func TestCommandMoveRecoversHardExitTemporaryFile(t *testing.T) {
	for _, point := range []string{"apply", "rollback", "same-name"} {
		t.Run(point, func(t *testing.T) {
			root := projectFixture(t, TemplateModule, TemplateName)
			require.NoError(
				t,
				os.WriteFile(
					filepath.Join(root, "cmd", TemplateName, "doc.go"),
					[]byte("// Command "+TemplateName+" 提供入口。\npackage main\n"),
					0o600,
				),
			)
			source := "package main\nimport _ \"" + TemplateModule + "/internal/app/cli\"\nconst appName=\"" + TemplateName + "\"\nfunc main(){}\n"
			require.NoError(t, os.WriteFile(filepath.Join(root, "cmd", TemplateName, "main.go"), []byte(source), 0o600))
			//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
			cmd := exec.Command(os.Args[0], "-test.run=^TestCommandMoveHardExitHelper$")
			cmd.Env = append(
				os.Environ(),
				"LAYOUT_HARD_EXIT_HELPER=1",
				"LAYOUT_HARD_EXIT_ROOT="+root,
				"LAYOUT_HARD_EXIT_POINT="+point,
			)
			err := cmd.Run()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			require.Equal(t, 99, exit.ExitCode())
			name := "my-cli"
			if point == "same-name" {
				name = TemplateName
			}
			_, err = Initialize(root, "example.com/tool", name)
			require.NoError(t, err)
			project, err := Inspect(root)
			require.NoError(t, err)
			require.Equal(t, name, project.Name)
			entries, err := os.ReadDir(filepath.Join(root, "cmd", name))
			require.NoError(t, err)
			require.Len(t, entries, 2)
			//nolint:gosec // G304：路径仅来自本次模板测试的临时根目录和固定命令名称。
			doc, err := os.ReadFile(filepath.Join(root, "cmd", name, "doc.go"))
			require.NoError(t, err)
			require.Contains(t, string(doc), name)
		})
	}
}

func TestCommandMoveTransaction(t *testing.T) {
	i, root, changes, move := commandTransactionFixture(t)
	paths, err := i.apply(context.Background(), root, "example.com/team/repository", "my-cli", changes, move)
	require.NoError(t, err)
	require.Contains(t, paths, filepath.Join(root, "cmd/my-cli/main.go"))
	require.Contains(t, paths, filepath.Join(root, "cmd/my-cli"))
	ok, err := afero.Exists(i.FS, filepath.Join(root, move.From))
	require.NoError(t, err)
	require.False(t, ok)
	info, err := i.FS.Stat(filepath.Join(root, move.To, "main.go"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	data, err := afero.ReadFile(i.FS, filepath.Join(root, move.To, "main_test.go"))
	require.NoError(t, err)
	require.Equal(t, "package main\n", string(data))
	doc, err := afero.ReadFile(i.FS, filepath.Join(root, move.To, "doc.go"))
	require.NoError(t, err)
	require.Contains(t, string(doc), "Command my-cli")
}

func TestCommandMovePreflightProtectsUserFiles(t *testing.T) {
	for _, mode := range []string{"target", "extra", "directory"} {
		t.Run(mode, func(t *testing.T) {
			i, root, changes, move := commandTransactionFixture(t)
			switch mode {
			case "target":
				require.NoError(t, i.FS.MkdirAll(filepath.Join(root, move.To), 0o755))
			case "extra":
				require.NoError(
					t,
					afero.WriteFile(i.FS, filepath.Join(root, move.From, "user.go"), []byte("人工内容"), 0o600),
				)
			case "directory":
				require.NoError(t, i.FS.MkdirAll(filepath.Join(root, move.From, "other"), 0o755))
			}
			before, err := afero.ReadFile(i.FS, filepath.Join(root, "go.mod"))
			require.NoError(t, err)
			_, err = i.apply(context.Background(), root, "example.com/team/repository", "my-cli", changes, move)
			require.Error(t, err)
			after, err := afero.ReadFile(i.FS, filepath.Join(root, "go.mod"))
			require.NoError(t, err)
			require.Equal(t, before, after)
			ok, err := afero.Exists(i.FS, filepath.Join(root, journalDir))
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestCommandMoveRecovery(t *testing.T) {
	for _, point := range []string{"before", "after", "committed", "cleanup"} {
		t.Run(point, func(t *testing.T) {
			var i *Initializer
			var root string
			var move *commandMove
			if point == "cleanup" {
				var changes []change
				i, root, changes, move = commandTransactionFixture(t)
				base := i.FS
				i.FS = &cleanupFaultFS{Fs: base, root: root, point: "before"}
				_, err := i.apply(context.Background(), root, "example.com/team/repository", "my-cli", changes, move)
				require.Error(t, err)
				i.FS = base
			} else {
				i, root, move = crashCommandMove(t, point)
			}
			require.NoError(t, i.recover(root, "example.com/team/repository", "my-cli"))
			wantDir, wantName := move.From, TemplateName
			if point == "committed" || point == "cleanup" {
				wantDir, wantName = move.To, "my-cli"
			}
			data, err := afero.ReadFile(i.FS, filepath.Join(root, wantDir, "main.go"))
			require.NoError(t, err)
			require.Contains(t, string(data), wantName)
			doc, err := afero.ReadFile(i.FS, filepath.Join(root, wantDir, "doc.go"))
			require.NoError(t, err)
			require.Contains(t, string(doc), "Command "+wantName)
			info, err := i.FS.Stat(filepath.Join(root, wantDir, "main.go"))
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
			ok, err := afero.Exists(i.FS, filepath.Join(root, journalDir))
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestCommandMoveRecoveryRejectsExternalChanges(t *testing.T) {
	for _, mode := range []string{"both", "neither", "edited", "unchanged-file", "documentation", "missing-documentation", "extra", "conflict", "escape"} {
		t.Run(mode, func(t *testing.T) {
			i, root, move := crashCommandMove(t, "after")
			switch mode {
			case "both":
				require.NoError(t, i.FS.MkdirAll(filepath.Join(root, move.From), 0o755))
			case "neither":
				require.NoError(t, i.FS.RemoveAll(filepath.Join(root, move.To)))
			case "edited":
				require.NoError(
					t,
					afero.WriteFile(i.FS, filepath.Join(root, move.To, "main.go"), []byte("人工修改"), 0o600),
				)
			case "unchanged-file":
				require.NoError(
					t,
					afero.WriteFile(i.FS, filepath.Join(root, move.To, "main_test.go"), []byte("人工修改"), 0o600),
				)
			case "documentation":
				require.NoError(t, afero.WriteFile(i.FS, filepath.Join(root, move.To, "doc.go"), []byte("人工修改"), 0o600))
			case "missing-documentation":
				require.NoError(t, i.FS.Remove(filepath.Join(root, move.To, "doc.go")))
			case "extra":
				require.NoError(
					t,
					afero.WriteFile(i.FS, filepath.Join(root, move.To, "extra.txt"), []byte("人工修改"), 0o600),
				)
			case "escape":
				file := filepath.Join(root, journalDir, "journal.json")
				raw, err := afero.ReadFile(i.FS, file)
				require.NoError(t, err)
				require.NoError(
					t,
					afero.WriteFile(
						i.FS,
						file,
						[]byte(strings.ReplaceAll(string(raw), `"cmd/my-cli"`, `"../outside"`)),
						0o600,
					),
				)
			}
			module := "example.com/team/repository"
			if mode == "conflict" {
				module = "example.com/other"
			}
			require.Error(t, i.recover(root, module, "my-cli"))
			ok, err := afero.Exists(i.FS, filepath.Join(root, journalDir, "journal.json"))
			require.NoError(t, err)
			require.True(t, ok)
		})
	}
}

func TestCommandMoveCancellation(t *testing.T) {
	for _, point := range []string{"before", "after"} {
		t.Run(point, func(t *testing.T) {
			i, root, changes, move := commandTransactionFixture(t)
			base := i.FS
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if point == "before" {
				cancel()
			} else {
				i.FS = faultFS{
					Fs: base,
					rename: func(a, b string) error {
						err := base.Rename(a, b)
						if err == nil && b == filepath.Join(root, move.To) {
							cancel()
						}
						return err
					},
				}
			}
			_, err := i.apply(ctx, root, "example.com/team/repository", "my-cli", changes, move)
			require.ErrorIs(t, err, context.Canceled)
			data, err := afero.ReadFile(base, filepath.Join(root, move.From, "main.go"))
			require.NoError(t, err)
			require.Contains(t, string(data), TemplateName)
		})
	}
}

func TestCommandMovePreflightRejectsSymlink(t *testing.T) {
	root := projectFixture(t, TemplateModule, TemplateName)
	outside := filepath.Join(t.TempDir(), "outside.go")
	require.NoError(t, os.WriteFile(outside, []byte("人工内容"), 0o600))
	if err := os.Symlink(outside, filepath.Join(root, "cmd", TemplateName, "main_test.go")); err != nil {
		t.Skipf("当前平台无法创建链接：%v", err)
	}
	i := (&Initializer{}).defaults()
	require.Error(t, i.preflightCommandMove(root, &commandMove{"cmd/" + TemplateName, "cmd/my-cli"}))
	//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "人工内容", string(data))
}

func TestRecoveryRejectsV1Journal(t *testing.T) {
	i, root, changes, _ := commandTransactionFixture(t)
	tx, err := i.prepare(root, "example.com/tool", "tool", changes, nil)
	require.NoError(t, err)
	tx.FormatVersion = 1
	require.NoError(t, i.save(root, tx))
	err = i.recover(root, "example.com/tool", "tool")
	require.Error(t, err)
	require.Contains(t, err.Error(), "原版本")
	ok, err := afero.Exists(i.FS, filepath.Join(root, journalDir, "journal.json"))
	require.NoError(t, err)
	require.True(t, ok)
}

func TestCommandMoveRecoveryRejectsDirectoryLink(t *testing.T) {
	root := projectFixture(t, TemplateModule, TemplateName)
	base := afero.NewOsFs()
	i := &Initializer{FS: base}
	move := &commandMove{"cmd/" + TemplateName, "cmd/my-cli"}
	file := filepath.Join(root, move.From, "main.go")
	//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	i.FS = faultFS{
		Fs: base,
		rename: func(a, b string) error {
			err := base.Rename(a, b)
			if err == nil && b == filepath.Join(root, move.To) {
				panic("移动后中断")
			}
			return err
		},
	}
	require.Panics(t, func() {
		if _, err := i.apply(
			context.Background(),
			root,
			"example.com/tool",
			"my-cli",
			[]change{{file, []byte(strings.ReplaceAll(string(data), TemplateName, "my-cli")), 0o600}},
			move,
		); err != nil {
			t.Error(err)
		}
	})
	i.FS = base
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.Rename(filepath.Join(root, move.To), outside))
	if err := os.Symlink(outside, filepath.Join(root, move.To)); err != nil {
		t.Skipf("当前平台无法创建链接：%v", err)
	}
	require.Error(t, i.recover(root, "example.com/tool", "my-cli"))
	//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	actual, err := os.ReadFile(filepath.Join(outside, "main.go"))
	require.NoError(t, err)
	require.Equal(t, strings.ReplaceAll(string(data), TemplateName, "my-cli"), string(actual))
}

func TestCommandMoveRollbackInterruptionIsRetryable(t *testing.T) {
	i, root, move := crashCommandMove(t, "after")
	base := i.FS
	i.FS = faultFS{
		Fs: base,
		rename: func(a, b string) error {
			err := base.Rename(a, b)
			if err == nil && b == filepath.Join(root, move.From) {
				panic("移回后中断")
			}
			return err
		},
	}
	require.Panics(t, func() {
		require.NoError(t, i.recover(root, "example.com/team/repository", "my-cli"))
	})
	i.FS = base
	require.NoError(t, i.recover(root, "example.com/team/repository", "my-cli"))
	data, err := afero.ReadFile(base, filepath.Join(root, move.From, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(data), TemplateName)
}
