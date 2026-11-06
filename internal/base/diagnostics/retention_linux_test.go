//go:build linux

package diagnostics

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCleanupPreservesUnknownFileAddedDuringRevalidation(t *testing.T) {
	base := t.TempDir()
	cfg := Defaults()
	cfg.RetentionDays = 0
	cfg.MaxTotalSizeMiB = 0
	run, err := Open(base, cfg, nil)
	require.NoError(t, err)
	writer, err := run.Writer(EventsFile)
	require.NoError(t, err)
	_, err = writer.Write([]byte("{}\n"))
	require.NoError(t, err)
	require.NoError(t, run.Close())
	old := time.Now().Add(-48 * time.Hour)
	// 扩大元数据读取窗口，让文件系统通知在第二次目录内容扫描后稳定插入用户文件。
	data, err := json.Marshal(
		map[string]any{
			"version":  metadataVersion,
			"run_id":   run.ID,
			"started":  old,
			"finished": old,
			"padding":  strings.Repeat("x", 16*1024*1024),
		},
	)
	require.NoError(t, err)
	marker := filepath.Join(run.Path, "user.txt")
	metadataPath := filepath.Join(run.Path, metadataName)
	require.NoError(t, os.WriteFile(metadataPath, data, 0o600))
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, syscall.Close(fd))
	}()
	_, err = syscall.InotifyAddWatch(fd, metadataPath, syscall.IN_OPEN)
	require.NoError(t, err)
	inserted := make(chan error, 1)
	go func() {
		count := 0
		buf := make([]byte, 4096)
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			n, err := syscall.Read(fd, buf)
			if err != nil && !errors.Is(err, syscall.EAGAIN) {
				inserted <- err
				return
			}
			for offset := 0; offset < n; {
				mask := binary.NativeEndian.Uint32(buf[offset+4 : offset+8])
				length := binary.NativeEndian.Uint32(buf[offset+12 : offset+16])
				if mask&syscall.IN_OPEN != 0 {
					count++
					if count == 2 {
						inserted <- os.WriteFile(marker, []byte("保留用户内容"), 0o600)
						return
					}
				}
				offset += syscall.SizeofInotifyEvent + int(length)
			}
			select {
			case <-deadline.C:
				inserted <- errors.New("未观察到第二次元数据读取")
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	cfg.RetentionDays = 1
	cleanupErr := Cleanup(base, cfg)
	require.NoError(t, <-inserted)
	require.NoError(t, cleanupErr)
	//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
	content, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, "保留用户内容", string(content))
	content, err = os.ReadFile(filepath.Join(run.Path, EventsFile))
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(content))
	_, err = os.Stat(metadataPath)
	require.NoError(t, err)
}
