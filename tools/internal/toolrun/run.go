package toolrun

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run 执行显式 argv，继承 IO 并追加环境覆盖，向调用者返回失败。
func Run(dir string, env []string, name string, args ...string) error {
	//nolint:gosec // G204：开发任务显式执行固定工具及拆分参数，不经过 shell；参数来自仓库或调用者。
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("执行 %s %s 失败：%w", name, strings.Join(args, " "), err)
	}
	return nil
}
