package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHelperProcess 只运行测试程序自身，不加载应用配置或用户状态。
func TestHelperProcess(t *testing.T) {
	if os.Getenv("COMMAND_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	switch args[0] {
	case "echo":
		cwd, err := os.Getwd()
		require.NoError(t, err)
		data, err := json.Marshal([]any{args[1:], os.Getenv("VALUE"), cwd})
		require.NoError(t, err)
		if _, err := os.Stdout.Write(data); err != nil {
			t.Error(err)
		}
	case "bytes":
		if _, err := os.Stdout.Write([]byte{0, 255, '\n'}); err != nil {
			t.Error(err)
		}
		if _, err := os.Stderr.Write([]byte{254, 0, 'x'}); err != nil {
			t.Error(err)
		}
	case "large":
		if _, err := os.Stdout.Write(bytes.Repeat([]byte{'a'}, 2<<20)); err != nil {
			t.Error(err)
		}
		if _, err := os.Stderr.Write(bytes.Repeat([]byte{'b'}, 2<<20)); err != nil {
			t.Error(err)
		}
	case "exit":
		os.Exit(7)
	case "gate-fail":
		signal.Ignore(syscall.SIGTERM)
		//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
		require.NoError(t, os.WriteFile(args[1], []byte("ready"), 0o600))
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
			if _, err := os.Stat(args[2]); err == nil {
				os.Exit(7)
			}
			time.Sleep(time.Millisecond)
		}
		os.Exit(8)
	case "term-marker":
		received := make(chan os.Signal, 1)
		signal.Notify(received, syscall.SIGTERM)
		//nolint:gosec // G703：测试路径和权限只来自当前临时 fixture，不读取用户文件。
		require.NoError(t, os.WriteFile(args[1], []byte("ready"), 0o600))
		select {
		case <-received:
			//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
			require.NoError(t, os.WriteFile(args[2], []byte("term"), 0o600))
		case <-time.After(5 * time.Second):
			t.Fatal("等待终止信号超时")
		}
		time.Sleep(5 * time.Second)
		signal.Stop(received)
	case "interval":
		start := time.Now().UnixNano()
		//nolint:gosec // G703：同步路径只由当前测试的临时目录生成。
		if err := os.WriteFile(args[1], []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		waitMarker(t, args[2])
		data, err := json.Marshal([]int64{start, time.Now().UnixNano()})
		require.NoError(t, err)
		if _, err := os.Stdout.Write(data); err != nil {
			t.Error(err)
		}

	case "ready-sleep":
		//nolint:gosec // G703：同步路径只由当前测试的临时目录生成。
		if err := os.WriteFile(args[1], []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		delay, err := time.ParseDuration(args[2])
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(delay)
		if _, err := os.Stdout.WriteString(args[3]); err != nil {
			t.Error(err)
		}
	case "gate-output":
		//nolint:gosec // G703：同步路径只由当前测试的临时目录生成。
		if err := os.WriteFile(args[1], []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		waitMarker(t, args[2])
		if _, err := os.Stdout.WriteString(args[3]); err != nil {
			t.Error(err)
		}
	case "sleep":
		delay, err := time.ParseDuration(args[1])
		require.NoError(t, err)
		time.Sleep(delay)
		if _, err := os.Stdout.WriteString(args[2]); err != nil {
			t.Error(err)
		}
	case "heartbeat":
		signal.Ignore(syscall.SIGTERM)
		time.AfterFunc(10*time.Second, func() {
			os.Exit(10)
		})
		for {
			//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
			require.NoError(t, os.WriteFile(args[1], []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o600))
			time.Sleep(10 * time.Millisecond)
		}
	case "branch":
		executable, err := os.Executable()
		require.NoError(t, err)
		//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
		child := exec.Command(executable, "-test.run=^TestHelperProcess$", "--", "spawn", args[1], args[2], "success")
		child.Env = os.Environ()
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Run(); err != nil {
			os.Exit(9)
		}
	case "spawn":
		executable, err := os.Executable()
		require.NoError(t, err)
		//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
		child := exec.Command(executable, "-test.run=^TestHelperProcess$", "--", "heartbeat", args[1])
		child.Env = os.Environ()
		if args[2] == "pipes" {
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
		}
		if err := child.Start(); err != nil {
			os.Exit(9)
		}
		waitMarker(t, args[1])
		if args[3] == "wait" {
			time.Sleep(time.Minute)
		}
		if args[3] == "fail" {
			os.Exit(7)
		}
	}
	os.Exit(0)
}

func helper(t *testing.T, args ...string) (Invocation, Execution) {
	t.Helper()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Invocation{
		Program:   executable,
		Operation: "test.helper",
		Args:      append([]string{"-test.run=^TestHelperProcess$", "--"}, args...),
	}, Execution{
		Dir: root,
		Env: []string{
			"COMMAND_HELPER=1",
			"GORACE=atexit_sleep_ms=0",
			"MY_HOME=" + root,
			"HOME=" + root,
			"USERPROFILE=" + root,
			"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
		},
	}
}

// startCapture 让主测试控制就绪与取消，清理始终等待执行器归还资源。
func startCapture(ctx context.Context, t *testing.T, call Invocation, options Execution) <-chan Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	done := make(chan Result, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- Capture(ctx, call, options, CaptureOptions{})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("捕获 helper 未完成收尾")
		}
	})
	return done
}

func awaitCapture(t *testing.T, done <-chan Result) Result {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("等待捕获 helper 结果超时")
	}
	return Result{}
}

type batchOutcome struct {
	items []Result
	err   error
}

func startBatch(
	ctx context.Context,
	t *testing.T,
	calls []Invocation,
	options Execution,
	batch BatchOptions,
) <-chan batchOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	done := make(chan batchOutcome, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		items, err := RunBatch(ctx, calls, options, batch)
		done <- batchOutcome{items, err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("批量 helper 未完成收尾")
		}
	})
	return done
}

func awaitBatch(t *testing.T, done <-chan batchOutcome) batchOutcome {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("等待批量 helper 结果超时")
	}
	return batchOutcome{}
}

func assertHeartbeatStopped(t *testing.T, path string, window time.Duration) {
	t.Helper()
	//nolint:gosec // G304：心跳路径只来自当前测试的临时目录。
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("心跳观察被取消")
	}
	//nolint:gosec // G304：心跳路径只来自当前测试的临时目录。
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("返回后后代仍在写入心跳 %s", path)
	}
}

func waitHeartbeatUpdate(t *testing.T, path string, before []byte) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		//nolint:gosec // G304：心跳路径只来自当前测试的临时目录。
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 0 && !bytes.Equal(data, before) {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("兄弟进程未继续更新心跳 %s", path)
		case <-t.Context().Done():
			t.Fatal("等待兄弟进程心跳被取消")
		case <-ticker.C:
		}
	}
}

func waitMarker(t *testing.T, path string) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		//nolint:gosec // G703：同步路径只由父测试的临时 fixture 传入，不读取用户路径。
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("读取进程同步标记 %s: %v", path, err)
		}
		select {
		case <-t.Context().Done():
			t.Fatalf("等待进程同步标记 %s 被取消: %v", path, t.Context().Err())
		case <-timer.C:
			t.Fatalf("未出现进程同步标记 %s", path)
		case <-ticker.C:
		}
	}
}
