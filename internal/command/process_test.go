//go:build linux || darwin || windows

package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCancellationRetainsExitAndCause(t *testing.T) {
	call, options := helper(t, "ready-sleep")
	ready := filepath.Join(options.Dir, "ready")
	call.Args = append(call.Args, ready, "1m", "never")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	cause := errors.New("调用者取消")
	done := startCapture(ctx, t, call, options)
	waitMarker(t, ready)
	cancel(cause)
	r := awaitCapture(t, done)
	var exit *exec.ExitError
	if !errors.Is(r.Err, context.Canceled) || !errors.Is(r.Err, cause) || !errors.As(r.Err, &exit) ||
		r.Status != Canceled ||
		r.ExitCode != exit.ExitCode() {
		t.Fatalf("取消错误链 %+v", r)
	}
}

func TestInvalidExecutionDoesNotStart(t *testing.T) {
	if err := Run(t.Context(), Invocation{}, Execution{}); err == nil {
		t.Fatal("空程序应失败")
	}
	if _, err := RunBatch(
		t.Context(),
		nil,
		Execution{},
		BatchOptions{Concurrency: -1},
	); err == nil ||
		strings.Contains(err.Error(), "panic") {
		t.Fatalf("无效并发数: %v", err)
	}
}

func TestNoImplicitExecutionDeadline(t *testing.T) {
	call, options := helper(t, "sleep", "400ms", "complete")
	r := Capture(t.Context(), call, options, CaptureOptions{})
	if r.Err != nil || string(r.Stdout) != "complete" {
		t.Fatalf("默认宽限错误地限制执行时间 %+v", r)
	}
}

func TestOwnedDescendantsStopOnEveryReturn(t *testing.T) {
	for _, outcome := range []string{"success", "fail", "wait"} {
		for _, streams := range []string{"pipes", "closed"} {
			t.Run(outcome+"/"+streams, func(t *testing.T) {
				call, options := helper(t, "spawn")
				heartbeat := filepath.Join(options.Dir, "heartbeat")
				call.Args = append(call.Args, heartbeat, streams, outcome)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				start := time.Now()
				done := startCapture(ctx, t, call, options)
				if outcome == "wait" {
					waitMarker(t, heartbeat)
					cancel()
				}
				r := awaitCapture(t, done)
				if time.Since(start) > 3*time.Second {
					t.Fatal("未有界收尾")
				}
				switch outcome {
				case "success":
					switch {
					case r.Err == nil:
						if r.Status != Succeeded || r.ExitCode != 0 {
							t.Fatalf("成功结果不完整: %+v", r)
						}
					case streams == "pipes" && errors.Is(r.Err, exec.ErrWaitDelay):
						// 根进程已成功退出，后代占用管道仍可能耗尽排空宽限。
						if r.Status != Failed || r.ExitCode != -1 {
							t.Fatalf("管道宽限结果不一致: %+v", r)
						}
					default:
						t.Fatalf("成功场景返回非预期错误: %+v", r)
					}
				case "fail":
					var exit *exec.ExitError
					if !errors.As(r.Err, &exit) || exit.ExitCode() != 7 || r.ExitCode != 7 || r.Status != Failed {
						t.Fatalf("失败结果不完整: %+v", r)
					}
				case "wait":
					var exit *exec.ExitError
					if !errors.Is(r.Err, context.Canceled) || !errors.As(r.Err, &exit) || r.Status != Canceled ||
						r.ExitCode != exit.ExitCode() {
						t.Fatalf("取消结果不完整: %+v", r)
					}
				}
				assertHeartbeatStopped(t, heartbeat, 60*time.Millisecond)
			})
		}
	}
}

func TestCleanupDoesNotKillSibling(t *testing.T) {
	sibling, opts := helper(t, "heartbeat")
	path := filepath.Join(opts.Dir, "sibling")
	sibling.Args = append(sibling.Args, path)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	//nolint:gosec // G204：只执行当前测试二进制，参数来自临时 fixture。
	process := exec.CommandContext(ctx, sibling.Program, sibling.Args...)
	process.Env = opts.Env
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		// Wait 已完成时 Windows 会释放句柄；此时不再重复终止进程。
		if process.ProcessState == nil {
			if err := process.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
		}
		//nolint:errcheck // 主测试断言进程结果；早退收尾可能产生主动终止错误，仍必须等待进程退出。
		_ = process.Wait()
	})
	waitMarker(t, path)
	call, options := helper(t, "exit")
	err := Run(t.Context(), call, options)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("清理目标没有按预期失败: %v", err)
	}
	//nolint:gosec // G304：心跳路径只来自当前测试的临时目录。
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	waitHeartbeatUpdate(t, path, before)
}

func TestRootExitsBeforeGrandchildHoldingPipes(t *testing.T) {
	call, options := helper(t, "branch")
	heartbeat := filepath.Join(options.Dir, "grandchild")
	call.Args = append(call.Args, heartbeat, "pipes")
	r := awaitCapture(t, startCapture(t.Context(), t, call, options))
	switch {
	case r.Err == nil:
		if r.Status != Succeeded || r.ExitCode != 0 {
			t.Fatalf("根成功结果不完整: %+v", r)
		}
	case errors.Is(r.Err, exec.ErrWaitDelay):
		if r.Status != Failed || r.ExitCode != -1 {
			t.Fatalf("管道宽限结果不一致: %+v", r)
		}
	default:
		t.Fatal(r.Err)
	}
	assertHeartbeatStopped(t, heartbeat, 60*time.Millisecond)
}
