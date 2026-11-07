//go:build unix

package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/config"
	"golang.org/x/sys/unix"
)

func TestConfigFIFOIsRejectedBeforeDoctorBudgetOrParentDeadline(t *testing.T) {
	options := businessOptions(t)
	dirs, err := options.Paths()
	require.NoError(t, err)
	path := filepath.Join(dirs.Config, "config.toml")
	require.NoError(t, unix.Mkfifo(path, 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	timer := time.AfterFunc(800*time.Millisecond, func() {
		defer close(done)
		writer, openErr := os.OpenFile(
			path,
			os.O_WRONLY|unix.O_NONBLOCK,
			0o600,
		) // #nosec G304 -- 测试私有FIFO，非阻塞解除旧实现的读等待。
		if openErr == nil {
			//nolint:errcheck // 仅为RED回归解除原有读取阻塞；不影响断言。
			_ = writer.Close()
		}
	})
	defer func() {
		if !timer.Stop() {
			<-done
		}
	}()
	options.Timeout = 100 * time.Millisecond
	options.LoadConfig = func(probe context.Context) (config.Config, error) {
		return config.Load(
			probe,
			config.Options{Path: path, Explicit: true, LookupEnv: func(string) (string, bool) { return "", false }},
		)
	}
	started := time.Now()
	result, err := Run(ctx, options)
	require.Less(t, time.Since(started), 500*time.Millisecond, "配置FIFO不得等待写端，阻塞检查超时与父取消")
	require.NoError(t, err)
	require.False(t, result.RequiredAvailable)
	require.Equal(t, StatusFailed, result.Checks[1].Status)
	require.Equal(t, "config.file_type", result.Checks[1].Code)
	require.NoError(t, os.Remove(path))
	regular := filepath.Join(dirs.Config, "regular.toml")
	require.NoError(t, os.WriteFile(regular, []byte("[data]\nrepository='owner/data'\n"), 0o600))
	require.NoError(t, os.Symlink(regular, path))
	result, err = Run(t.Context(), options)
	require.NoError(t, err)
	require.True(t, result.RequiredAvailable, "指向普通文件的配置符号链接保持可用")
}
