package toolrun

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunContextRejectsCanceledContextBeforeStarting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, RunContext(ctx, "", nil, "command-that-must-not-start"), context.Canceled)
}

func TestRunContextCancelsStartedProcessAndWaits(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunContext(
			ctx,
			"",
			[]string{"TOOLRUN_HELPER=1", "TOOLRUN_READY=" + ready},
			os.Args[0],
			"-test.run=^TestToolrunHelperProcess$",
		)
	}()
	waitForFile(t, ready)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("取消后子进程未完成 Wait")
	}
}

func TestToolrunHelperProcess(t *testing.T) {
	if os.Getenv("TOOLRUN_HELPER") != "1" {
		return
	}
	//nolint:gosec // G703：路径只来自当前测试进程注入的临时 readiness 文件。
	require.NoError(t, os.WriteFile(os.Getenv("TOOLRUN_READY"), []byte("ready"), 0o600))
	time.Sleep(30 * time.Second)
}

func TestWithEnvironmentReplacesOverriddenKeys(t *testing.T) {
	environment := WithEnvironment(
		[]string{"PATH=/bin", "GOCACHE=/inherited", "KEEP=value"},
		[]string{"GOCACHE=/isolated", "XDG_CACHE_HOME=/cache"},
	)

	require.Equal(t, []string{"PATH=/bin", "KEEP=value", "GOCACHE=/isolated", "XDG_CACHE_HOME=/cache"}, environment)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 %s 超时", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
