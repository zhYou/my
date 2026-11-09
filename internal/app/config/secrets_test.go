package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretsIdentityFileUsesEnvironmentAndValidatesAbsolutePath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "identity.txt")
	configPath := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))
	cfg, err := Load(t.Context(), Options{
		AppName: "my",
		Path:    configPath,
		LookupEnv: func(key string) (string, bool) {
			if key == "MY_HOME" {
				return home, true
			}
			if key == "MY_SECRETS_IDENTITY_FILE" {
				return path, true
			}
			return "", false
		},
	})
	require.NoError(t, err)
	require.Equal(t, path, cfg.Secrets.IdentityFile)

	_, err = Load(t.Context(), Options{
		AppName: "my",
		Path:    configPath,
		LookupEnv: func(key string) (string, bool) {
			if key == "MY_HOME" {
				return home, true
			}
			if key == "MY_SECRETS_IDENTITY_FILE" {
				return "relative.txt", true
			}
			return "", false
		},
	})
	require.Error(t, err)
}
