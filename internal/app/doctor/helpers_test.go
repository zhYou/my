package doctor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/datarepo"
)

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
