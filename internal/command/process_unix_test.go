//go:build linux || darwin

package command

import (
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestTerminateProcessGroupWaitsAfterKill(t *testing.T) {
	killed := false
	observed := make(chan struct{})
	exited := make(chan struct{})
	done := make(chan struct{})
	var observe sync.Once
	var exit sync.Once
	t.Cleanup(func() { exit.Do(func() { close(exited) }) })
	sendSignal := func(group int, sig syscall.Signal) error {
		if group != -42 {
			t.Errorf("信号目标不是自有进程组: %d", group)
		}
		switch sig {
		case syscall.SIGTERM:
			return nil
		case syscall.SIGKILL:
			killed = true
			return nil
		case 0:
			if killed {
				observe.Do(func() { close(observed) })
				// 延迟内核确认：强制信号已接受，进程组仍在退出中。
				<-exited
				return syscall.ESRCH
			}
			return nil
		default:
			t.Errorf("未预期的信号: %v", sig)
			return syscall.EINVAL
		}
	}
	go func() {
		terminateProcessGroup(-42, 20*time.Millisecond, sendSignal)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("强制终止信号发出后未观察进程组退出便返回")
	case <-observed:
	case <-time.After(3 * time.Second):
		t.Fatal("未观察强制终止后的进程组状态")
	}
	select {
	case <-done:
		t.Fatal("进程组尚未退出便完成清理")
	default:
	}
	exit.Do(func() { close(exited) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("进程组退出后未完成清理")
	}
}

func TestTerminateProcessGroupBoundsForcedKillObservation(t *testing.T) {
	done := make(chan struct{})
	kills, observations := 0, 0
	sendSignal := func(group int, sig syscall.Signal) error {
		if group != -42 {
			t.Errorf("信号目标不是自有进程组: %d", group)
		}
		if sig == syscall.SIGKILL {
			kills++
		}
		if sig == 0 && kills > 0 {
			observations++
		}
		// 僵尸保留组号时，探测仍可能成功。
		return nil
	}
	go func() {
		terminateProcessGroup(-42, 20*time.Millisecond, sendSignal)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("强制终止后仍无限等待进程组消失")
	}
	if kills != 1 || observations == 0 {
		t.Fatalf("未完成一次强制终止及后续观察: kills=%d observations=%d", kills, observations)
	}
}
