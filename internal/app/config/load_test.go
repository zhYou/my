package config

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/locales"

	"github.com/stretchr/testify/require"
)

func TestConfigurationPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[logging]\nlevel = \"info\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
		env  string
		flag string
		want string
	}{
		{"默认值", "", "", "", "info"},
		{"文件", path, "", "", "info"},
		{"环境变量", path, "error", "", "error"},
		{"显式选项", path, "error", "debug", "debug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var override *string
			if tc.flag != "" {
				override = &tc.flag
			}
			cfg, err := Load(t.Context(), Options{
				AppName:       "my-cli",
				Path:          tc.path,
				UserConfigDir: dir,
				LogLevel:      override,
				LookupEnv: func(key string) (string, bool) {
					return tc.env, key == "MY_CLI_LOG_LEVEL" && tc.env != ""
				},
			})
			if err != nil || cfg.Logging.Level != tc.want {
				t.Fatalf("cfg=%+v err=%v", cfg, err)
			}
		})
	}
}

func TestInvalidConfigurationFails(t *testing.T) {
	for _, content := range []string{"[broken", "unknown: value\n", "log_level: noisy\n", "log_level: [debug]\n", "log_level: warn\n---\nlog_level: error\n"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(t.Context(), Options{AppName: "testcli", Path: path}); err == nil {
			t.Errorf("错误地接受了无效配置 %q", content)
		}
	}
	if _, err := Load(
		t.Context(),
		Options{AppName: "testcli", Path: filepath.Join(t.TempDir(), "missing")},
	); err == nil {
		t.Fatal("错误地接受了不存在的显式配置文件")
	}
}

func TestLoadsDefaultUserConfiguration(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "mycli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "mycli", "config.toml"),
		[]byte("[logging]\nlevel = \"debug\"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(t.Context(), Options{
		AppName:       "mycli",
		UserConfigDir: dir,
		LookupEnv: func(string) (string, bool) {
			return "", false
		},
	})
	if err != nil || cfg.Logging.Level != "debug" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestExplicitDefaultOverridesEnvironment(t *testing.T) {
	dir := t.TempDir()
	warn := "warn"
	for _, override := range []*string{nil, &warn} {
		cfg, err := Load(t.Context(), Options{
			AppName:       "my-cli",
			UserConfigDir: dir,
			LogLevel:      override,
			LookupEnv: func(k string) (string, bool) {
				return "debug", k == "MY_CLI_LOG_LEVEL"
			},
		})
		require.NoError(t, err)
		want := "debug"
		if override != nil {
			want = "warn"
		}
		require.Equal(t, want, cfg.Logging.Level)
	}
}

func TestStrictTOML(t *testing.T) {
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{"", true},
		{"[logging]\nlevel = \"info\"", true},
		{"[logging]\nlevel = \"warn\"\nlevel = \"error\"", false},
		{"[logging]\nunknown = 1", false},
		{"unknown = 1", false},
		{"[logging]\nlevel = 123", false},
		{"[logging]\nlevel = [\"info\"]", false},
		{"[logging]\noutput = \"stdout\"", false},
		{"[diagnostics]\nmax_size_mib = 0", false},
		{"[diagnostics]\nmax_backups = 1.2", false},
		{"[diagnostics]\nmax_backups = \"5\"", false},
		{"[telemetry]\nexport_timeout = 1", false},
		{"[telemetry]\nexport_timeout = \"0s\"", false},
		{"[telemetry]\nqueue_size = 3\nbatch_size = 4", false},
		{"[telemetry]\nexporter = \"otlp\"", false},
		{"log_level: info", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tc.text), 0o600))
			_, err := Load(t.Context(), Options{
				AppName:       "testcli",
				Path:          path,
				UserConfigDir: t.TempDir(),
				LookupEnv: func(string) (string, bool) {
					return "", false
				},
			})
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestModuleDefaultsAndEnvironmentTypes(t *testing.T) {
	env := map[string]string{
		"TOOL_LOGGING_OUTPUT":           "none",
		"TOOL_LOGGING_LEVEL":            "warn",
		"TOOL_LOG_LEVEL":                "error",
		"TOOL_DIAGNOSTICS_MAX_BACKUPS":  "8",
		"TOOL_TELEMETRY_EXPORT_TIMEOUT": "250ms",
	}
	load := func() (Config, error) {
		return Load(t.Context(), Options{
			AppName:       "tool",
			UserConfigDir: t.TempDir(),
			LookupEnv: func(k string) (string, bool) {
				v, ok := env[k]
				return v, ok
			},
		})
	}
	cfg, err := load()
	require.NoError(t, err)
	require.Equal(t, "error", cfg.Logging.Level)
	require.Equal(t, "none", cfg.Logging.Output)
	require.Equal(t, 8, cfg.Diagnostics.MaxBackups)
	require.Equal(t, 10, cfg.Diagnostics.MaxSizeMiB)
	require.Equal(t, 14, cfg.Diagnostics.RetentionDays)
	require.Equal(t, 200, cfg.Diagnostics.MaxTotalSizeMiB)
	require.Equal(t, 250*time.Millisecond, cfg.Telemetry.ExportTimeout)
	require.Equal(t, "none", cfg.Telemetry.Exporter)
	require.Equal(t, 2048, cfg.Telemetry.QueueSize)
	require.Equal(t, 512, cfg.Telemetry.BatchSize)
	require.Equal(t, 5*time.Second, cfg.Telemetry.BatchInterval)
	require.Equal(t, time.Second, cfg.Telemetry.ShutdownTimeout)
	for _, value := range []string{"1.2", "five", "-1"} {
		env["TOOL_DIAGNOSTICS_MAX_BACKUPS"] = value
		_, err := load()
		require.Error(t, err)
	}
}

func TestAppHomeOverridesDefaultAndRejectsRelativeEvenWithExplicitFile(t *testing.T) {
	home := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(home, "config.toml"), []byte("[logging]\nlevel = \"debug\"\n"), 0o600),
	)
	load := func(file string) (Config, error) {
		return Load(t.Context(), Options{
			AppName:       "tool",
			Path:          file,
			UserConfigDir: t.TempDir(),
			LookupEnv: func(k string) (string, bool) {
				return home, k == "TOOL_HOME"
			},
		})
	}
	cfg, err := load("")
	require.NoError(t, err)
	require.Equal(t, "debug", cfg.Logging.Level)
	file := filepath.Join(home, "config.toml")
	home = "relative"
	_, err = load(file)
	require.Error(t, err)
}

func TestTOMLKeyStructureBeforeMerging(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		valid bool
	}{
		{"点号语法", `logging.level = "debug"
logging.output = "stderr"`, true},
		{"引用分段语法", `"logging"."level" = "debug"
"logging"."output" = "stderr"`, true},
		{"字面点号键", `"logging.level" = "debug"`, false},
		{"字面键与嵌套字段冲突", `"logging.level" = "noisy"
[logging]
level = "debug"
output = "stderr"`, false},
		{"字面表名", `["logging.level"]
value = "debug"`, false},
		{"嵌套字面键", `[logging]
"level.part" = "debug"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(file, []byte(tc.text), 0o600))
			for i := range 32 {
				cfg, err := Load(t.Context(), Options{
					Path: file,
					LookupEnv: func(string) (string, bool) {
						return "", false
					},
				})
				if tc.valid {
					require.NoError(t, err)
					require.Equal(t, "debug", cfg.Logging.Level)
					require.Equal(t, "stderr", cfg.Logging.Output)
				} else {
					require.Error(t, err, "第 %d 次读取不得接受字面点号键", i)
				}
			}
		})
	}
}

func TestCanceledLoadReturnsBeforeReadingEnvironmentOrFile(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cfg, err := Load(ctx, Options{
		Path: filepath.Join(t.TempDir(), "missing.toml"), Explicit: true,
		LookupEnv: func(string) (string, bool) {
			t.Fatal("取消后不应读取环境或配置")
			return "", false
		},
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, Defaults(), cfg)
}

// 配置解析原因必须可遍历，但输入值不能进入可显示的错误。
func TestInvalidInputDoesNotLeakValues(t *testing.T) {
	bundle, err := locales.LoadBundle()
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		content string
		key     string
	}{
		{"integer_env", "", "TESTCLI_DIAGNOSTICS_MAX_BACKUPS"},
		{"duration_env", "", "TESTCLI_TELEMETRY_EXPORT_TIMEOUT"},
		{"duration_file", "[telemetry]\nexport_timeout = \"fake-private-marker\"\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(file, []byte(tc.content), 0o600))
			_, err := Load(
				t.Context(),
				Options{
					AppName:   "testcli",
					Path:      file,
					LookupEnv: func(key string) (string, bool) { return "fake-private-marker", tc.key != "" && key == tc.key },
				},
			)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "fake-private-marker")
			if tc.key != "" {
				diagnostic, ok := fault.DiagnosticOf(err)
				require.True(t, ok)
				require.Equal(t, "config.environment", diagnostic.Phase)
			}
			for _, lang := range []string{"en", "zh-CN"} {
				require.NotContains(t, bundle.Localizer(lang).Render(err), "fake-private-marker")
			}
			if tc.name == "integer_env" {
				var numeric *strconv.NumError
				require.ErrorAs(t, err, &numeric)
			}
		})
	}
}

func TestDataConfigurationPrecedenceAndValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(
		t,
		os.WriteFile(
			file,
			[]byte("[data]\nrepository='owner/file'\nbranch='from-file'\nrequest_timeout='3s'\n"),
			0o600,
		),
	)
	env := map[string]string{}
	load := func() (Config, error) {
		return Load(
			t.Context(),
			Options{
				AppName:   "my",
				Path:      file,
				LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
			},
		)
	}
	cfg, err := load()
	require.NoError(t, err)
	require.Equal(t, "owner/file", cfg.Data.Repository)
	require.Equal(t, 3*time.Second, cfg.Data.RequestTimeout)
	env["MY_DATA_REPOSITORY"] = "owner/env"
	env["MY_DATA_BRANCH"] = "from-env"
	env["MY_DATA_REQUEST_TIMEOUT"] = "750ms"
	cfg, err = load()
	require.NoError(t, err)
	require.Equal(t, "owner/env", cfg.Data.Repository)
	require.Equal(t, "from-env", cfg.Data.Branch)
	require.Equal(t, 750*time.Millisecond, cfg.Data.RequestTimeout)
	env["MY_DATA_REPOSITORY"] = ""
	cfg, err = load()
	require.NoError(t, err)
	require.Equal(t, "owner/file", cfg.Data.Repository)
	for key, values := range map[string][]string{"MY_DATA_REPOSITORY": {"../private-marker", "https://github.com/owner/data"}, "MY_DATA_BRANCH": {"../private-marker", "a.lock/b", "a?b"}, "MY_DATA_REQUEST_TIMEOUT": {"0s", "-1s", "private-marker"}} {
		for _, value := range values {
			env[key] = value
			_, err = load()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-marker")
			delete(env, key)
		}
	}
}
