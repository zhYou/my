package diagnostics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
)

const helperRootEnv = "MY_DIAGNOSTICS_TEST_ROOT"

func TestRetentionProtectsActiveAndUnknownFiles(t *testing.T) {
	base := t.TempDir()
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 1
	run, err := Open(base, cfg, nil)
	require.NoError(t, err)
	writer, err := run.Writer(EventsFile)
	require.NoError(t, err)
	_, err = writer.Write([]byte(strings.Repeat("x", 2*1024*1024)))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(base, "legacy.jsonl"), []byte("legacy"), 0o600))
	require.NoError(t, Cleanup(base, cfg))
	_, err = os.Stat(run.Path)
	require.NoError(t, err)
	require.NoError(t, run.Close())
	_, err = os.Stat(run.Path)
	require.ErrorIs(t, err, os.ErrNotExist)
	// #nosec G304 -- 仅读取本测试创建的临时目录文件。
	legacy, err := os.ReadFile(filepath.Join(base, "legacy.jsonl"))
	require.NoError(t, err)
	require.Equal(t, "legacy", string(legacy))
}

func TestExpiredRunAndUnknownContent(t *testing.T) {
	base := t.TempDir()
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 0
	run, err := Open(base, cfg, nil)
	require.NoError(t, err)
	require.NoError(t, run.Close())
	old := time.Now().Add(-48 * time.Hour)
	meta := metadata{Version: 1, ID: run.ID, Started: old, Finished: old}
	require.NoError(t, writeMetadataPath(run.Path, meta))
	cfg.RetentionDays = 1
	require.NoError(t, Cleanup(base, cfg))
	_, err = os.Stat(run.Path)
	require.ErrorIs(t, err, os.ErrNotExist)
	unknown, err := Open(base, Defaults(), nil)
	require.NoError(t, err)
	require.NoError(t, unknown.Close())
	require.NoError(t, os.WriteFile(filepath.Join(unknown.Path, "user.txt"), []byte("keep"), 0o600))
	require.NoError(
		t,
		writeMetadataPath(unknown.Path, metadata{Version: 1, ID: unknown.ID, Started: old, Finished: old}),
	)
	require.NoError(t, Cleanup(base, cfg))
	_, err = os.Stat(filepath.Join(unknown.Path, "user.txt"))
	require.NoError(t, err)
}

func TestRetentionReclaimsPublishedRunWithInterruptedMetadataUpdate(t *testing.T) {
	base := t.TempDir()
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 0
	run, err := Open(base, cfg, nil)
	require.NoError(t, err)
	require.NoError(t, run.Close())
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, writeMetadataPath(run.Path, metadata{Version: 1, ID: run.ID, Started: old, Finished: old}))
	require.NoError(t, os.WriteFile(filepath.Join(run.Path, metadataTemporaryName), []byte("{"), 0o600))
	cfg.RetentionDays = 1
	require.NoError(t, Cleanup(base, cfg))
	_, err = os.Stat(run.Path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDiagnosticLeaseHelper(t *testing.T) {
	base := os.Getenv(helperRootEnv)
	if base == "" {
		return
	}
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 0
	run, err := Open(base, cfg, nil)
	require.NoError(t, err)
	w, err := run.Writer(EventsFile)
	require.NoError(t, err)
	_, err = w.Write([]byte(strings.Repeat("x", 2*1024*1024)))
	require.NoError(t, err)
	fmt.Println(run.Path)
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Error(err)
	}
	require.NoError(t, run.Close())
}

func TestDiagnosticCleanupHelper(t *testing.T) {
	base := os.Getenv(helperRootEnv)
	if base == "" {
		return
	}
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 1
	require.NoError(t, Cleanup(base, cfg))
}

func TestProcessesProtectLiveRunAndReclaimKilledWriter(t *testing.T) {
	base := t.TempDir()
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	//nolint:gosec // G204：仅执行当前测试二进制的专用子进程。
	child := exec.CommandContext(ctx, exe, "-test.run=^TestDiagnosticLeaseHelper$")
	child.WaitDelay = time.Second
	child.Env = append(os.Environ(), helperRootEnv+"="+base)
	input, err := child.StdinPipe()
	require.NoError(t, err)
	output, err := child.StdoutPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	})
	waitChild := startDiagnosticProcess(t, child)
	line, err := bufio.NewReader(output).ReadString('\n')
	require.NoError(t, err)
	path := strings.TrimSpace(line)
	require.Equal(t, base, filepath.Dir(path))
	var cleaners []func() error
	for range 3 {
		// #nosec G204 -- 仅执行当前测试二进制的专用清理子进程。
		cleaner := exec.CommandContext(ctx, exe, "-test.run=^TestDiagnosticCleanupHelper$")
		cleaner.Env = append(os.Environ(), helperRootEnv+"="+base)
		cleaners = append(cleaners, startDiagnosticProcess(t, cleaner))
	}
	for _, wait := range cleaners {
		require.NoError(t, wait())
	}
	_, err = os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, child.Process.Kill())
	require.Error(t, waitChild())
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 1
	require.NoError(t, Cleanup(base, cfg))
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRetentionDoesNotFollowArtifactSymlinks(t *testing.T) {
	base := t.TempDir()
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 0
	run, err := Open(base, cfg, nil)
	require.NoError(t, err)
	require.NoError(t, run.Close())
	victim := filepath.Join(t.TempDir(), "keep")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o600))
	if err := os.Symlink(victim, filepath.Join(run.Path, EventsFile)); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("当前 Windows 主机无法创建符号链接：%v", err)
		}
		require.NoError(t, err)
	}
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, writeMetadataPath(run.Path, metadata{Version: 1, ID: run.ID, Started: old, Finished: old}))
	cfg.RetentionDays = 1
	require.NoError(t, Cleanup(base, cfg))
	// #nosec G304 -- 仅读取本测试创建的链接目标，验证它未被清理器修改。
	b, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "keep", string(b))
	_, err = os.Lstat(filepath.Join(run.Path, EventsFile))
	require.NoError(t, err)
}

// TestDiagnosticInterruptedCleanupHelper 在首次真实数据删除后握手，持锁等待父进程终止。
func TestDiagnosticInterruptedCleanupHelper(t *testing.T) {
	base := os.Getenv(helperRootEnv)
	if base == "" {
		return
	}
	cfg := Defaults()
	cfg.RetentionDays = 1
	cfg.MaxTotalSizeMiB = 0
	require.NoError(t, cleanup(base, cfg, func(root *os.Root, name string) error {
		if err := root.Remove(name); err != nil {
			return err
		}
		if artifactPattern.MatchString(name) {
			fmt.Println(name)
			if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
				t.Error(err)
			}
		}
		return nil
	}))
}

func TestInterruptedCleanupDoesNotOrphanData(t *testing.T) {
	base := t.TempDir()
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 0
	create := func() *Run {
		run, err := Open(base, cfg, nil)
		require.NoError(t, err)
		for _, name := range []string{EventsFile, "events.000001.jsonl", "trace.000001.jsonl"} {
			require.NoError(t, os.WriteFile(filepath.Join(run.Path, name), []byte("{}\n"), 0o600))
		}
		return run
	}
	expired := create()
	require.NoError(t, expired.Close())
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(
		t,
		writeMetadataPath(
			expired.Path,
			metadata{Version: metadataVersion, ID: expired.ID, Started: old, Finished: old},
		),
	)
	active := create()
	defer func() {
		require.NoError(t, active.Close())
	}()
	unknown := create()
	require.NoError(t, unknown.Close())
	marker := filepath.Join(unknown.Path, "user.txt")
	require.NoError(t, os.WriteFile(marker, []byte("保留"), 0o600))
	require.NoError(
		t,
		writeMetadataPath(
			unknown.Path,
			metadata{Version: metadataVersion, ID: unknown.ID, Started: old, Finished: old},
		),
	)
	// 活动目录也过期，确保保留依赖租约而非时间筛选。
	require.NoError(
		t,
		writeMetadataPath(active.Path, metadata{Version: metadataVersion, ID: active.ID, Started: old, Finished: old}),
	)
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	//nolint:gosec // G204：仅执行当前测试二进制的专用子进程。
	child := exec.CommandContext(ctx, exe, "-test.run=^TestDiagnosticInterruptedCleanupHelper$")
	child.WaitDelay = time.Second
	child.Env = append(os.Environ(), helperRootEnv+"="+base)
	input, err := child.StdinPipe()
	require.NoError(t, err)
	output, err := child.StdoutPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	})
	waitChild := startDiagnosticProcess(t, child)
	ready := make(chan string, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		line, err := bufio.NewReader(output).ReadString('\n')
		assert.NoError(t, err)
		ready <- strings.TrimSpace(line)
	}()
	t.Cleanup(func() {
		if err := input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		// Wait 已完成时 Windows 会释放句柄；此时不再重复终止进程。
		if child.ProcessState == nil {
			if err := child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
		}
		//nolint:errcheck // 主测试断言进程结果；早退收尾可能产生主动终止错误，仍必须等待进程退出。
		_ = waitChild()
		select {
		case <-readDone:
		case <-time.After(time.Second):
			t.Error("诊断 helper 就绪读取未结束")
		}
	})
	var removed string
	select {
	case removed = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("清理子进程未完成首次数据删除握手")
	}
	require.True(t, artifactPattern.MatchString(removed), "握手必须指明已删除的数据文件")
	require.NoFileExists(t, filepath.Join(expired.Path, removed))
	require.FileExists(t, filepath.Join(expired.Path, metadataName))
	entries, err := os.ReadDir(expired.Path)
	require.NoError(t, err)
	remaining := 0
	for _, entry := range entries {
		if artifactPattern.MatchString(entry.Name()) {
			remaining++
		}
	}
	require.Equal(t, 2, remaining, "中断前必须还剩两个真实数据文件")
	require.NoError(t, child.Process.Kill())
	require.Error(t, waitChild())
	cfg.RetentionDays = 1
	require.NoError(t, Cleanup(base, cfg))
	require.NoDirExists(t, expired.Path)
	for _, name := range []string{EventsFile, "events.000001.jsonl", "trace.000001.jsonl", metadataName} {
		require.FileExists(t, filepath.Join(active.Path, name))
		require.FileExists(t, filepath.Join(unknown.Path, name))
	}
	//nolint:gosec // G304：仅读取本测试临时目录的未知文件。
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, "保留", string(data))
}
