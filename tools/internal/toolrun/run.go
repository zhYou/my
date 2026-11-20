package toolrun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Run 执行显式 argv，继承 IO 并追加环境覆盖，向调用者返回失败。
func Run(dir string, env []string, name string, args ...string) error {
	return RunContext(context.Background(), dir, env, name, args...)
}

// RunContext 执行显式 argv，继承 IO 并响应调用方取消。
func RunContext(ctx context.Context, dir string, env []string, name string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	//nolint:gosec // G204：开发任务显式执行固定工具及拆分参数，不经过 shell；参数来自仓库或调用者。
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = WithEnvironment(os.Environ(), env)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("执行 %s %s 已取消：%w", name, strings.Join(args, " "), ctxErr)
		}
		return fmt.Errorf("执行 %s %s 失败：%w", name, strings.Join(args, " "), err)
	}
	return nil
}

// WithEnvironment 删除被覆盖键并追加覆盖值，Windows 键名按大小写不敏感处理。
func WithEnvironment(base, overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			keys[environmentKey(key)] = struct{}{}
		}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := keys[environmentKey(key)]; replaced {
				continue
			}
		}
		result = append(result, entry)
	}
	return append(result, overrides...)
}

func environmentKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}
