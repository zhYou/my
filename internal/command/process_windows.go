package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

// Windows 10/Server 2016 起，在创建时绑定独立 Job；失败时不降级到无 Job 的启动。
func runPlatform(ctx context.Context, program string, args []string, options Execution) error {
	child, err := createWindows(ctx, program, args, options)
	if err != nil {
		return err
	}
	return child.wait(ctx, options.WaitDelay)
}

type windowsChild struct {
	info    windows.ProcessInformation
	job     windows.Handle
	process *os.Process
	streams *windowsIO
}

// wait 消费一次创建结果。监控先于 Resume 建立，并独占 Job 终止与关闭权。
// 主流程等监控完成后再关闭原始进程/线程句柄。
func (child *windowsChild) wait(ctx context.Context, delay time.Duration) (result error) {
	defer func() {
		result = errors.Join(result, windows.CloseHandle(child.info.Thread), windows.CloseHandle(child.info.Process))
		result = errors.Join(result, child.streams.close())
	}()
	child.streams.start()
	rootDone := make(chan struct{})
	cleaned := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
		case <-rootDone:
		}
		cleaned <- terminateAndCloseWindowsJob(child.job)
	}()
	var resumeErr error
	if ctx.Err() != nil {
		resumeErr = contextError(ctx)
	} else {
		previous, err := windows.ResumeThread(child.info.Thread)
		resumeErr = err
		if err == nil && previous != 1 {
			resumeErr = windowsSuspendInvalid.New()
		}
	}
	// Resume 失败/取消的挂起进程也必须先终止，再获取真实退出状态。
	if resumeErr != nil {
		close(rootDone)
	}
	state, waitErr := child.process.Wait()
	if resumeErr == nil {
		close(rootDone)
	}
	cleanupErr := <-cleaned
	if waitErr == nil && !state.Success() {
		waitErr = &exec.ExitError{ProcessState: state}
	}
	streamErr := child.streams.wait(delay)
	return errors.Join(resumeErr, waitErr, cleanupErr, streamErr)
}
