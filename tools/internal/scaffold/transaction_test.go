package scaffold

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type cleanupFaultFS struct {
	afero.Fs
	root          string
	point         string
	rollback      bool
	failedCleanup bool
	failedApply   bool
}

func (f *cleanupFaultFS) Rename(a, b string) error {
	if f.rollback && !f.failedApply && b == filepath.Join(f.root, "cmd", TemplateName, "main.go") {
		f.failedApply = true
		return errors.New("模拟应用失败")
	}
	return f.Fs.Rename(a, b)
}

func (f *cleanupFaultFS) Remove(path string) error {
	dir := filepath.Join(f.root, journalDir)
	match := false
	switch f.point {
	case "before":
		match = path == filepath.Join(dir, "000000.before")
	case "after":
		match = path == filepath.Join(dir, "000000.after")
	case "journal":
		match = path == filepath.Join(dir, "journal.json")
	case "directory":
		match = path == dir
	}
	if match && !f.failedCleanup {
		f.failedCleanup = true
		if f.point != "directory" {
			if err := f.Fs.Remove(path); err != nil {
				return err
			}
		}
		return errors.New("模拟部分清理后中断")
	}
	return f.Fs.Remove(path)
}

func (f *cleanupFaultFS) RemoveAll(path string) error {
	if path == filepath.Join(f.root, journalDir) && !f.failedCleanup {
		f.failedCleanup = true
		suffix := "000000." + f.point
		if f.point == "journal" {
			suffix = "journal.json"
		}
		if f.point == "directory" {
			if err := f.Fs.RemoveAll(path); err != nil {
				return err
			}
			if err := f.Mkdir(path, 0o700); err != nil {
				return err
			}
		} else if err := f.Fs.Remove(filepath.Join(path, suffix)); err != nil {
			return err
		}
		return errors.New("模拟部分清理后中断")
	}
	return f.Fs.RemoveAll(path)
}

func TestTerminalCleanupIsRetryable(t *testing.T) {
	for _, outcome := range []string{"committed", "rolled-back"} {
		for _, point := range []string{"before", "after", "journal", "directory"} {
			t.Run(outcome+"/"+point, func(t *testing.T) {
				i, root := memoryInitializer(t)
				base := i.FS
				fault := &cleanupFaultFS{Fs: base, root: root, point: point, rollback: outcome == "rolled-back"}
				i.FS = fault
				_, err := i.Initialize(context.Background(), root, "example.com/tool", "tool")
				require.Error(t, err)
				require.True(t, fault.failedCleanup)
				mod, err := afero.ReadFile(base, filepath.Join(root, "go.mod"))
				require.NoError(t, err)
				if outcome == "committed" {
					require.Contains(t, string(mod), "example.com/tool")
				} else {
					require.Contains(t, string(mod), TemplateModule)
				}
				i.FS = base
				_, err = i.Initialize(context.Background(), root, "example.com/tool", "tool")
				require.NoError(t, err)
				mod, err = afero.ReadFile(base, filepath.Join(root, "go.mod"))
				require.NoError(t, err)
				require.Contains(t, string(mod), "example.com/tool")
				ok, err := afero.Exists(base, filepath.Join(root, journalDir))
				require.NoError(t, err)
				require.False(t, ok)
			})
		}
	}
}

func TestRecoveryRejectsInconsistentExistence(t *testing.T) {
	for _, mode := range []string{"false", "missing", "null", "missing-mode"} {
		t.Run(mode, func(t *testing.T) {
			i, root := crashedTransaction(t)
			path := filepath.Join(root, journalDir, "journal.json")
			data, err := afero.ReadFile(i.FS, path)
			require.NoError(t, err)
			var doc map[string]any
			require.NoError(t, json.Unmarshal(data, &doc))
			files, ok := doc["files"].([]any)
			require.True(t, ok)
			require.NotEmpty(t, files)
			file, ok := files[0].(map[string]any)
			require.True(t, ok)
			switch mode {
			case "false":
				file["existed"] = false
			case "missing":
				delete(file, "existed")
			case "null":
				file["existed"] = nil
			case "missing-mode":
				delete(file, "mode")
			}
			modified, err := json.Marshal(doc)
			require.NoError(t, err)
			require.NoError(t, afero.WriteFile(i.FS, path, modified, 0o600))
			before, err := afero.ReadFile(i.FS, filepath.Join(root, "go.mod"))
			require.NoError(t, err)
			_, err = i.Initialize(context.Background(), root, "example.com/tool", "tool")
			require.Error(t, err)
			after, err := afero.ReadFile(i.FS, filepath.Join(root, "go.mod"))
			require.NoError(t, err)
			require.Equal(t, before, after)
			backup, err := afero.ReadFile(i.FS, filepath.Join(root, journalDir, "000000.before"))
			require.NoError(t, err)
			require.Contains(t, string(backup), TemplateModule)
		})
	}
}

func TestRecoverBeforeIdentityCheck(t *testing.T) {
	i, root := crashedTransaction(t)
	module, err := afero.ReadFile(i.FS, filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(module), "example.com/tool")
	main, err := afero.ReadFile(i.FS, filepath.Join(root, "cmd", TemplateName, "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(main), TemplateName)
	_, err = i.Initialize(context.Background(), root, "example.com/tool", "tool")
	require.NoError(t, err)
	main, err = afero.ReadFile(i.FS, filepath.Join(root, "cmd", "tool", "main.go"))
	require.NoError(t, err)
	require.Contains(t, string(main), `"tool"`)
}

func TestRecoveryRejectsEditedOrEscapingPaths(t *testing.T) {
	for _, mode := range []string{"edited", "escape", "conflict", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			i, root := crashedTransaction(t)
			path := filepath.Join(root, "cmd", TemplateName, "main.go")
			journal := filepath.Join(root, ".template-init", "journal.json")
			switch mode {
			case "edited":
				require.NoError(t, afero.WriteFile(i.FS, path, []byte("人工修改"), 0o600))
			case "escape":
				data, err := afero.ReadFile(i.FS, journal)
				require.NoError(t, err)
				require.NoError(
					t,
					afero.WriteFile(
						i.FS,
						journal,
						[]byte(strings.Replace(string(data), `"go.mod"`, `"../outside"`, 1)),
						0o600,
					),
				)
			case "corrupt":
				require.NoError(t, afero.WriteFile(i.FS, journal, []byte("{"), 0o600))
			}
			before, err := afero.ReadFile(i.FS, path)
			require.NoError(t, err)
			module := "example.com/tool"
			if mode == "conflict" {
				module = "example.com/other"
			}
			_, err = i.Initialize(context.Background(), root, module, "tool")
			require.Error(t, err)
			after, err := afero.ReadFile(i.FS, path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			ok, err := afero.Exists(i.FS, journal)
			require.NoError(t, err)
			require.True(t, ok)
		})
	}
}

func TestRollbackFailureKeepsRecoveryData(t *testing.T) {
	i, root := memoryInitializer(t)
	base := i.FS
	targets := 0
	i.FS = faultFS{
		Fs: base,
		rename: func(a, b string) error {
			if b == filepath.Join(root, "go.mod") || b == filepath.Join(root, "README.md") ||
				b == filepath.Join(root, "cmd", TemplateName, "main.go") {
				targets++
				if targets >= 2 {
					return errors.New("写入和回滚失败")
				}
			}
			return base.Rename(a, b)
		},
	}
	_, err := i.Initialize(context.Background(), root, "example.com/tool", "tool")
	require.Error(t, err)
	ok, err := afero.Exists(base, filepath.Join(root, ".template-init", "journal.json"))
	require.NoError(t, err)
	require.True(t, ok)
	i.FS = base
	_, err = i.Initialize(context.Background(), root, "example.com/tool", "tool")
	require.NoError(t, err)
}

func TestWriteFailureRestoresOriginalBytes(t *testing.T) {
	for failAt := 1; failAt <= 3; failAt++ {
		i, root := memoryInitializer(t)
		base := i.FS
		before := map[string][]byte{}
		for _, n := range []string{"go.mod", "cmd/" + TemplateName + "/main.go", "README.md"} {
			data, err := afero.ReadFile(base, filepath.Join(root, n))
			before[n] = data
			require.NoError(t, err)
		}
		count := 0
		i.FS = faultFS{
			Fs: base,
			rename: func(a, b string) error {
				if b == filepath.Join(root, "go.mod") || b == filepath.Join(root, "README.md") ||
					b == filepath.Join(root, "cmd", TemplateName, "main.go") {
					count++
					if count == failAt {
						return errors.New("指定写入失败")
					}
				}
				return base.Rename(a, b)
			},
		}
		_, err := i.Initialize(context.Background(), root, "example.com/tool", "tool")
		require.Error(t, err)
		for n, want := range before {
			got, err := afero.ReadFile(base, filepath.Join(root, n))
			require.NoError(t, err)
			require.Equal(t, want, got, n)
		}
	}
}

func TestLockHelper(t *testing.T) {
	if os.Getenv("TEMPLATE_LOCK_HELPER") != "1" {
		return
	}
	unlock, err := osLock(os.Getenv("TEMPLATE_LOCK_PATH"))
	if err != nil {
		os.Exit(3)
	}
	defer func() {
		require.NoError(t, unlock())
	}()
	if _, err := os.Stdout.WriteString("就绪\n"); err != nil {
		t.Error(err)
	}
	if _, err := os.Stdin.Read(make([]byte, 1)); err != nil {
		t.Error(err)
	}
}

func TestConcurrentInitializationDoesNotRecoverLiveTransaction(t *testing.T) {
	root := fixture(t)
	lock := filepath.Join(root, ".template-init.lock")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	//nolint:gosec // G204：锁 helper 只执行当前测试二进制。
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockHelper$")
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "TEMPLATE_LOCK_HELPER=1", "TEMPLATE_LOCK_PATH="+lock)
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	var waitOnce sync.Once
	var waitErr error
	waitChild := func() error {
		waitOnce.Do(func() {
			waitErr = cmd.Wait()
		})
		return waitErr
	}
	t.Cleanup(func() {
		cancel()
		if err := in.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		// Wait 已完成时 Windows 会释放句柄；此时不再重复终止进程。
		if cmd.ProcessState == nil {
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
		}
		//nolint:errcheck // 主测试断言进程结果；早退收尾可能产生主动终止错误，仍必须等待进程退出。
		_ = waitChild()
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "就绪\n", line)
	_, err = Initialize(root, "example.com/tool", "tool")
	require.Error(t, err)
	require.Contains(t, read(t, root, "go.mod"), TemplateModule)
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, waitChild())
	_, err = Initialize(root, "example.com/tool", "tool")
	require.NoError(t, err)
}
