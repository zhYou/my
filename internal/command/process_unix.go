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
		terminateProcessGroup(-cmd.Process.Pid, options.WaitDelay, syscall.Kill)
	}()
	err := cmd.Wait()
	close(waited)
	<-cleaned
	return err
}

// terminateProcessGroup 依次发送软终止和强制终止信号，限定自有进程组的收尾窗口。
func terminateProcessGroup(group int, grace time.Duration, sendSignal func(int, syscall.Signal) error) {
	// 即使根进程成功退出，也要处理仍在同组运行的后代。
	if err := sendSignal(group, syscall.SIGTERM); errors.Is(err, syscall.ESRCH) {
		return
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	forced := false
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			if forced {
				// 未被收养的僵尸可能保留组号；观察窗口耗尽后交由操作系统回收。
				return
			}
			if err := sendSignal(group, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
				return
			}
			// SIGKILL 返回只表示信号已发送；继续观察，给内核时间终止组内后代。
			forced = true
			timer.Reset(grace)
			if errors.Is(sendSignal(group, 0), syscall.ESRCH) {
				return
			}
		case <-ticker.C:
			if errors.Is(sendSignal(group, 0), syscall.ESRCH) {
				return
			}
		}
	}
}
