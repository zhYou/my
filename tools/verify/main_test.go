package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

type commandCall struct {
	program string
	args    []string
	dir     string
	env     []string
}

func TestRunRejectsArgumentsBeforeCreatingResources(t *testing.T) {
	err := run(context.Background(), []string{"extra"}, ".", dependencies{})
	require.EqualError(t, err, usageMessage)
	require.ErrorIs(t, err, errUsage)
}

func TestExitCodeMapsUsageAndCancellation(t *testing.T) {
	require.Equal(t, 2, exitCode(errUsage))
	require.Equal(t, 130, exitCode(context.Canceled))
	require.Equal(t, 1, exitCode(errors.New("runtime failure")))
}

func TestRunUsesIsolatedEnvironmentAndCleansTemporaryRoot(t *testing.T) {
	root := verificationProjectFixture(t)
	tempRoot := filepath.Join(t.TempDir(), "verify")
	require.NoError(t, os.Mkdir(tempRoot, 0o700))
	var calls []commandCall
	var removed string
	deps := dependencies{
		Environ: func() []string {
			return []string{
				"PATH=" + os.Getenv("PATH"),
				"MISE_TRUSTED_CONFIG_PATHS=/existing",
			}
		},
		MakeTempDir: func(pattern string) (string, error) {
			require.Contains(t, pattern, "demo")
			return tempRoot, nil
		},
		RemoveAll: func(path string) error {
			removed = path
			return nil
		},
		Run: func(_ context.Context, dir string, env []string, program string, args ...string) error {
			calls = append(calls, commandCall{
				program: program,
				args:    append([]string(nil), args...),
				dir:     dir,
				env:     append([]string(nil), env...),
			})
			return nil
		},
	}

	require.NoError(t, run(context.Background(), nil, root, deps))
	require.Equal(t, tempRoot, removed)
	require.Equal(t, []commandCall{
		{program: "mise", args: []string{"run", "check"}, dir: root},
		{program: "mise", args: []string{"exec", "--", "go", "test", "-race", "./..."}, dir: root},
		{program: "mise", args: []string{"run", "mcp:check"}, dir: root},
	}, stripEnvironment(calls))

	env := environmentMap(calls[0].env)
	for key, value := range map[string]string{
		"DEMO_HOME":             filepath.Join(tempRoot, "app-home"),
		"MISE_STATE_DIR":        filepath.Join(tempRoot, "mise", "state"),
		"MISE_CACHE_DIR":        filepath.Join(tempRoot, "mise", "cache"),
		"GOCACHE":               filepath.Join(tempRoot, "go", "cache"),
		"GOLANGCI_LINT_CACHE":   filepath.Join(tempRoot, "golangci-lint"),
		"XDG_CACHE_HOME":        filepath.Join(tempRoot, "xdg-cache"),
		"UV_CACHE_DIR":          filepath.Join(tempRoot, "uv", "cache"),
		"UV_TOOL_DIR":           filepath.Join(tempRoot, "uv", "tools"),
		"UV_PYTHON_INSTALL_DIR": filepath.Join(tempRoot, "uv", "python"),
		"npm_config_cache":      filepath.Join(tempRoot, "npm"),
		"GOTOOLCHAIN":           "local",
	} {
		require.Equal(t, value, env[key], key)
	}
	require.ElementsMatch(t, []string{root, "/existing"}, filepath.SplitList(env["MISE_TRUSTED_CONFIG_PATHS"]))
}

func TestRunStopsAtFirstFailureAndStillCleansTemporaryRoot(t *testing.T) {
	root := verificationProjectFixture(t)
	tempRoot := filepath.Join(t.TempDir(), "verify")
	require.NoError(t, os.Mkdir(tempRoot, 0o700))
	var calls int
	var removed string
	deps := dependencies{
		Environ: os.Environ,
		MakeTempDir: func(string) (string, error) {
			return tempRoot, nil
		},
		RemoveAll: func(path string) error {
			removed = path
			return nil
		},
		Run: func(_ context.Context, _ string, _ []string, _ string, _ ...string) error {
			calls++
			if calls == 2 {
				return errors.New("synthetic failure")
			}
			return nil
		},
	}

	err := run(context.Background(), nil, root, deps)
	require.ErrorContains(t, err, "隔离验证 go test -race 失败")
	require.ErrorContains(t, err, "synthetic failure")
	require.Equal(t, 2, calls)
	require.Equal(t, tempRoot, removed)
}

func TestRemoveIsolatedTreeMakesNestedModuleDirectoriesWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不使用 Unix mode 位验证只读目录")
	}
	root := t.TempDir()
	nested := filepath.Join(root, "go", "mod", "example.com", "module@v1")
	require.NoError(t, os.MkdirAll(nested, 0o750))
	file := filepath.Join(nested, "source.go")
	require.NoError(t, os.WriteFile(file, []byte("package module\n"), 0o600))
	require.NoError(t, os.Chmod(file, 0o400))
	//nolint:gosec // G302：测试需要模拟 Go 模块缓存的只读目录权限。
	require.NoError(t, os.Chmod(nested, 0o500))

	require.NoError(t, removeIsolatedTree(root))
	require.NoDirExists(t, root)
}

func stripEnvironment(calls []commandCall) []commandCall {
	stripped := make([]commandCall, len(calls))
	for i, call := range calls {
		stripped[i] = commandCall{
			program: call.program,
			args:    call.args,
			dir:     call.dir,
		}
	}
	return stripped
}

func environmentMap(entries []string) map[string]string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := splitEnvironmentEntry(entry)
		if ok {
			values[key] = value
		}
	}
	return values
}

func splitEnvironmentEntry(entry string) (string, string, bool) {
	for i := range entry {
		if entry[i] == '=' {
			return entry[:i], entry[i+1:], true
		}
	}
	return "", "", false
}
