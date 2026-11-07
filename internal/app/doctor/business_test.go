package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/datarepo"
)

func TestBusinessDoctorIsOfflineAndDoesNotCreateState(t *testing.T) {
	options := businessOptions(t)
	dirs, err := options.Paths()
	require.NoError(t, err)
	result, err := Run(t.Context(), options)
	require.NoError(t, err)
	require.True(t, result.RequiredAvailable)
	require.Len(t, result.Checks, 5)
	files, err := os.ReadDir(dirs.Config)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestBusinessCheckTimeoutReturnsCompletedFailure(t *testing.T) {
	options := businessOptions(t)
	options.Timeout = 10 * time.Millisecond
	options.CheckGH = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	result, err := Run(t.Context(), options)
	require.NoError(t, err)
	require.False(t, result.RequiredAvailable)
	require.Equal(t, StatusFailed, result.Checks[2].Status)
	require.Equal(t, StatusSkipped, result.Checks[3].Status)
}

func businessOptions(t *testing.T) Options {
	t.Helper()
	home := t.TempDir()
	cfg := config.Defaults()
	cfg.Data.Repository = "owner/data"
	return Options{Timeout: time.Second, Paths: func() (paths.Directories, error) {
		return paths.Directories{
			Config: home,
			Data:   filepath.Join(home, "data"),
			State:  filepath.Join(home, "state"),
			Cache:  filepath.Join(home, "cache"),
		}, nil
	}, LoadConfig: func(context.Context) (config.Config, error) { return cfg, nil }, CheckGH: func(context.Context) error { return nil }, Credential: func(context.Context) (string, error) { return "fixture-token", nil }, NewClient: func(context.Context, datarepo.Config, string) (*datarepo.Client, error) {
		t.Fatal("offline doctor contacted network")
		return nil, nil
	}}
}
