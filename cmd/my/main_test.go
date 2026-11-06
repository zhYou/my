package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/cli"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/greeting"
	"github.com/zhYou/my/internal/base/paths"
)

func TestSignalHelper(t *testing.T) {
	if os.Getenv("CLI_SIGNAL_HELPER") != "1" {
		return
	}
	notify := signalContext
	if runtime.GOOS == "windows" {
		notify = func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				if _, err := os.Stdin.Read(make([]byte, 1)); err != nil {
					t.Error(err)
				}
				cancel()
			}()
			return ctx, cancel
		}
	}
	deps := cli.Dependencies{
		LoadConfig: func(context.Context, config.Options) (config.Config, error) {
			return config.Defaults(), nil
		},
		Greet: func(ctx context.Context, _ greeting.Options) (greeting.Result, error) {
			if _, err := io.WriteString(os.Stderr, "就绪\n"); err != nil {
				t.Error(err)
			}
			<-ctx.Done()
			return greeting.Result{}, ctx.Err()
		},
	}
	os.Exit(run([]string{"greet", "--name", "test"}, &deps, notify))
}

func TestMainSignalCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSignalHelper$")

	prefix := paths.EnvPrefix(appName) + "_"
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "CLI_SIGNAL_HELPER=1", prefix+"HOME="+t.TempDir())
	cmd.Env = environment
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	readDone := make(chan struct{})
	var waitOnce sync.Once
	var waitErr error
	waitChild := func() error {
		waitOnce.Do(func() {
			waitErr = cmd.Wait()
		})
		return waitErr
	}
	t.Cleanup(func() {
		cancel()
		if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		// Wait 已完成时 Windows 会释放句柄；此时不再重复终止进程。
		if cmd.ProcessState == nil {
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
		}
		//nolint:errcheck // 主测试断言进程结果；早退收尾可能产生主动终止错误，仍必须等待进程退出。
		_ = waitChild()
		select {
		case <-readDone:
		case <-time.After(time.Second):
			t.Error("信号 helper 就绪读取未结束")
		}
	})
	reader := bufio.NewReader(stderr)
	ready := make(chan string, 1)
	go func() {
		defer close(readDone)
		line, err := reader.ReadString('\n')
		assert.NoError(t, err)
		ready <- line
	}()
	select {
	case line := <-ready:
		require.Equal(t, "就绪\n", line)
	case <-ctx.Done():
		t.Fatal("等待子进程就绪超时")
	}
	if runtime.GOOS == "windows" {
		t.Log("Windows 无法发送等价 Unix 信号，使用注入 notify 的受控取消")
		_, err = stdin.Write([]byte{1})
	} else {
		err = cmd.Process.Signal(os.Interrupt)
	}
	require.NoError(t, err)
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	err = waitChild()
	require.Error(t, err)
	require.Equal(t, 130, cmd.ProcessState.ExitCode())
	require.Empty(t, out.String())
	require.Contains(t, string(rest), "Operation cancelled")
}
