package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/require"
)

func TestProcessExitCodes(t *testing.T) {
	name := "cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/...")
	cmd.Dir = root
	data, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", data)
	testscript.Run(t, testscript.Params{
		Dir: "testdata/process",
		Setup: func(env *testscript.Env) error {
			home := filepath.Join(env.WorkDir, "home")
			if err := os.MkdirAll(home, 0o700); err != nil {
				return err
			}
			for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA", "LOCALAPPDATA", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
				env.Setenv(key, home)
			}
			env.Setenv("MY_HOME", home)
			for _, key := range []string{"MY_LOG_LEVEL", "MY_LOGGING_LEVEL", "MY_LOGGING_OUTPUT", "MY_LANG", "MY_TELEMETRY_EXPORTER"} {
				env.Setenv(key, "")
			}
			return nil
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"cli": func(ts *testscript.TestScript, neg bool, args []string) {
				if neg || len(args) == 0 {
					ts.Fatalf("用法：cli <退出码> [参数...]")
				}
				want, err := strconv.Atoi(args[0])
				ts.Check(err)
				err = ts.Exec(binary, args[1:]...)
				code := 0
				if err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) {
						ts.Fatalf("执行程序失败：%v", err)
					}
					code = exit.ExitCode()
				}
				if code != want {
					ts.Fatalf("退出码=%d，期望=%d", code, want)
				}
			},
		},
	})
}

// 用 Go overlay 重建真实内嵌目录，避免修改仓库或共享可变全局变量。
func TestCorruptCatalogEndToEnd(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.toml")
	require.NoError(t, os.WriteFile(broken, []byte("[invalid\n"), 0o600))
	overlay, err := json.Marshal(
		map[string]any{
			"Replace": map[string]string{filepath.Join(root, "internal", "locales", "active.en.toml"): broken},
		},
	)
	require.NoError(t, err)
	overlayFile := filepath.Join(dir, "overlay.json")
	require.NoError(t, os.WriteFile(overlayFile, overlay, 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	//nolint:gosec // G204：执行固定 Go 测试命令，overlay 路径来自当前测试的临时目录。
	cmd := exec.CommandContext(
		ctx,
		"go",
		"test",
		"-count=1",
		"-overlay",
		overlayFile,
		"-run",
		"^TestCorruptCatalogProcess$",
		"./internal/app/greeting",
		"./internal/app/session",
		"./internal/command",
	)
	cmd.Dir = root
	cmd.Env = append(
		isolatedAppEnvironment(t, "my"),
		"TEST_CORRUPT_I18N_CATALOG=1",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
	)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
