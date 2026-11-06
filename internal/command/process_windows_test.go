package command

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"golang.org/x/sys/windows"
)

func TestWindowsCanceledBeforeCreation(t *testing.T) {
	call, options := windowsHelper(t, "marker", filepath.Join(t.TempDir(), "marker"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Run(ctx, call, options); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(call.Args[3]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("取消后执行了业务: %v", err)
	}
}

func TestWindowsCancelAfterResumeAndDuringSpawn(t *testing.T) {
	for _, mode := range []string{"marker", "spawn", "spawn-loop"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "marker")
			call, options := windowsHelper(t, "marker", marker)
			switch mode {
			case "spawn":
				call, options = helper(t, "spawn", marker, "pipes", "wait")
			case "spawn-loop":
				call, options = windowsHelper(t, "spawn-loop", marker)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := startCapture(ctx, t, call, options)
			waitMarker(t, marker)
			cancel()
			result := awaitCapture(t, done)
			err := result.Err
			var exit *exec.ExitError
			if !errors.Is(err, context.Canceled) || !errors.As(err, &exit) {
				t.Fatalf("取消退出错误: %v", err)
			}
			assertHeartbeatStopped(t, marker, 80*time.Millisecond)
		})
	}
}

func TestWindowsUnexpectedSuspendCountStopsBeforeBusiness(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	call, options := windowsHelper(t, "marker", marker)
	options.IO = options.IO.WithDefaults()
	child, err := createWindows(t.Context(), call.Program, call.Args, options)
	if err != nil {
		t.Fatal(err)
	}
	// 真正增加挂起计数，验证 Resume 返回2时不会误报成功或遗留挂起进程。
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("SuspendThread")
	previous, _, suspendErr := proc.Call(uintptr(child.info.Thread))
	//nolint:gosec // G115：Win32 返回 DWORD；uintptr 来自系统 ABI，低 32 位保留文档规定的结果或错误码。
	if uint32(previous) == 0xffffffff {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, child.wait(ctx, DefaultWaitDelay), context.Canceled)
		t.Fatal(suspendErr)
	}
	err = child.wait(t.Context(), DefaultWaitDelay)
	if err == nil {
		t.Fatal("未检测异常挂起计数")
	}
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("异常 Resume 执行了业务: %v", err)
	}
}

func TestWindowsDrainDeadlineClosesOnlyOwnedPipes(t *testing.T) {
	// 借用管道有独立的有界释放；复制关闭自有写端后仍不得关闭借用 reader。
	borrowed, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		require.NoError(t, borrowed.Close())
	}()
	defer func() {
		if err := writer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	}()
	call, options := helper(t, "exit")
	// 包装为 Reader，真实文件读取在 goroutine 中进行，驱动 WaitDelay 分支。
	options.IO.In = struct{ io.Reader }{borrowed}
	options.WaitDelay = 10 * time.Millisecond
	released := make(chan struct{})
	timer := time.AfterFunc(500*time.Millisecond, func() {
		if err := writer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		close(released)
	})
	defer timer.Stop()
	r := Capture(t.Context(), call, options, CaptureOptions{})
	<-released
	if !errors.Is(r.Err, exec.ErrWaitDelay) || r.ExitCode != 7 {
		t.Fatalf("排空宽限/根退出状态: %+v", r)
	}
	if _, err = borrowed.Stat(); err != nil {
		t.Fatalf("关闭了借用 reader: %v", err)
	}
}

func TestWindowsResumeFailureCleansSuspendedProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	call, options := windowsHelper(t, "marker", marker)
	options.IO = options.IO.WithDefaults()
	child, err := createWindows(t.Context(), call.Program, call.Args, options)
	if err != nil {
		t.Fatal(err)
	}
	var restricted windows.Handle
	current := windows.CurrentProcess()
	err = windows.DuplicateHandle(
		current,
		child.info.Thread,
		current,
		&restricted,
		windows.THREAD_QUERY_INFORMATION,
		false,
		0,
	)
	if err != nil {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, child.wait(ctx, DefaultWaitDelay), context.Canceled)
		t.Fatal(err)
	}
	// 保留真实线程，但移除 Resume 所需权限，以真实系统错误驱动失败路径。
	require.NoError(t, windows.CloseHandle(child.info.Thread))
	child.info.Thread = restricted
	err = child.wait(t.Context(), DefaultWaitDelay)
	var exit *exec.ExitError
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || !errors.As(err, &exit) {
		t.Fatalf("恢复失败/退出状态: %v", err)
	}
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resume失败后执行业务: %v", err)
	}
}

func TestWindowsTerminationFailureClosesJobBeforeWaiting(t *testing.T) {
	for _, phase := range []string{"cancel_suspended", "abort_before_wait_handle"} {
		t.Run(phase, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "marker")
			call, options := windowsHelper(t, "marker", marker)
			options.IO = options.IO.WithDefaults()
			child, err := createWindows(t.Context(), call.Program, call.Args, options)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				require.NoError(t, child.streams.close())
			}()
			current := windows.CurrentProcess()
			var restricted, rescue windows.Handle
			// JOB_OBJECT_QUERY (0x0004) 不含 JOB_OBJECT_TERMINATE；保留真实 Job 的 kill-on-close。
			if err = windows.DuplicateHandle(current, child.job, current, &restricted, 0x0004, false, 0); err != nil {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				require.ErrorIs(t, child.wait(ctx, DefaultWaitDelay), context.Canceled)
				t.Fatal(err)
			}
			// 独立救援句柄让回归在旧实现死锁时仍可终止 helper，不触碰已关闭后可能复用的原始句柄。
			if err = windows.DuplicateHandle(
				current,
				child.info.Process,
				current,
				&rescue,
				0,
				false,
				windows.DUPLICATE_SAME_ACCESS,
			); err != nil {
				require.NoError(t, windows.CloseHandle(restricted))
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				require.ErrorIs(t, child.wait(ctx, DefaultWaitDelay), context.Canceled)
				t.Fatal(err)
			}
			defer func() {
				require.NoError(t, windows.CloseHandle(rescue))
			}()
			require.NoError(t, windows.CloseHandle(child.job))
			child.job = restricted
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var finish func() error
			if phase == "cancel_suspended" {
				finish = func() error {
					return child.wait(ctx, DefaultWaitDelay)
				}
			} else {
				// 模拟尚未获得独立 os.Process 等待句柄的创建中止；执行正式 abort 收尾函数。
				require.NoError(t, child.process.Release())
				finish = func() error {
					return abortWindowsProcess(child.info, child.job)
				}
			}
			done := make(chan error, 1)
			go func() {
				done <- finish()
			}()
			select {
			case err = <-done:
			case <-time.After(3 * time.Second):
				rescueErr := windows.TerminateProcess(rescue, 1)
				select {
				case cleanupErr := <-done:
					t.Fatalf("终止Job失败后等待根进程死锁，救援=%v，收尾=%v", rescueErr, cleanupErr)
				case <-time.After(3 * time.Second):
					t.Fatalf("终止Job失败后未收尾，救援=%v", rescueErr)
				}
			}
			if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Fatalf("丢失Job终止原生错误: %v", err)
			}
			if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
				t.Fatalf("重复关闭原生句柄: %v", err)
			}
			if status, waitErr := windows.WaitForSingleObject(
				rescue,
				0,
			); waitErr != nil ||
				status != windows.WAIT_OBJECT_0 {
				t.Fatalf("收尾返回后根进程仍存活: status=%d err=%v", status, waitErr)
			}
			var exitCode uint32
			if queryErr := windows.GetExitCodeProcess(rescue, &exitCode); queryErr != nil {
				t.Fatal(queryErr)
			}
			if phase == "cancel_suspended" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("丢失取消原因: %v", err)
				}
				// kill-on-close 未承诺非零退出码；独立原生句柄证明 ExitError 忠实反映实际退出。
				exit, hasExit := errors.AsType[*exec.ExitError](err)
				if hasExit != (exitCode != 0) || (hasExit && int64(exit.ExitCode()) != int64(exitCode)) {
					t.Fatalf("退出错误与原生状态不同: code=%d err=%v", exitCode, err)
				}
				// Go Windows 的 Wait 完成后释放等待句柄，再次等待返回 EINVAL。
				if _, secondWaitErr := child.process.Wait(); !errors.Is(secondWaitErr, syscall.EINVAL) {
					t.Fatalf("尚未完成原生 Wait: %v", secondWaitErr)
				}
			}
			t.Logf("kill-on-close 原生退出码: %d", exitCode)
			if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("挂起进程执行业务: %v", err)
			}
		})
	}
}
