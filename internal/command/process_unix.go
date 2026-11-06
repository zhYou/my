//go:build linux || darwin

package command

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// runPlatform 是原生后端边界：程序已定位，argv/env 已复制，IO 与宽限已补齐。
// 后端拥有直接子进程、管道及监控；返回前 Wait 子进程并终止自有进程组。
// 不改变全局 subreaper，不保证清理主动 setsid/脱离进程组的守护进程。
func runPlatform(ctx context.Context, program string, args []string, options Execution) error {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	// #nosec G204 -- 调用方显式指定程序和独立 argv，不经 shell。
	cmd := exec.Command(program, args...)
	cmd.Dir, cmd.Env = options.Dir, options.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = options.IO.In, options.IO.Out, options.IO.ErrOut
	cmd.WaitDelay = options.WaitDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	waited := make(chan struct{})
	cleaned := make(chan struct{})
	go func() {
		defer close(cleaned)
		select {
		case <-ctx.Done():
		case <-waited:
		}
		pid := -cmd.Process.Pid
		// 即使根进程成功退出，也要处理仍在同组运行的后代。
		if err := syscall.Kill(pid, syscall.SIGTERM); errors.Is(err, syscall.ESRCH) {
			return
		}
		timer := time.NewTimer(options.WaitDelay)
		defer timer.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-timer.C:
				//nolint:errcheck // 进程组可能已经退出；此处尽力收尾，根进程结果和取消原因由 Wait 及上层保留。
				_ = syscall.Kill(pid, syscall.SIGKILL)
				return
			case <-ticker.C:
				if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
					return
				}
			}
		}
	}()
	err := cmd.Wait()
	close(waited)
	<-cleaned
	return err
}
