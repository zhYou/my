package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/paths"
)

func TestDefaultLanguageIsEnglishAndIgnoresSystemLocale(t *testing.T) {
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	code, out, stderr := invoke(t, "greet", "--name", "Ada")
	require.Zero(t, code, stderr)
	require.Equal(t, "Hello, Ada!\n", out)
	require.Empty(t, stderr)
}

func TestLanguageOverrideDoesNotRenderUnselectedMetadata(t *testing.T) {
	for _, tc := range []struct {
		missing  string
		selected string
		want     string
	}{
		{"en", "zh", "用法："},
		{"zh-CN", "en", "Usage:"},
	} {
		t.Run(tc.selected, func(t *testing.T) {
			bundle := catalogFixture(t, tc.missing, "cli.greet", "")
			loader := func() (*messages.Bundle, error) {
				return bundle, nil
			}
			var out, stderr bytes.Buffer
			code := run(
				context.Background(),
				[]string{"--lang", tc.missing, "--lang", tc.selected, "greet", "--help"},
				Options{
					Out:       &out,
					ErrOut:    &stderr,
					Language:  tc.missing,
					LookupEnv: noFixtureEnv,
				},
				loader,
			)
			require.Zero(t, code, stderr.String())
			require.Contains(t, out.String(), tc.want)
			require.Empty(t, stderr.String())
		})
	}
}

func TestEnglishInvocation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--lang", "en", "greet", "--name", "Ada"}, 0, "Hello, Ada!"},
		{[]string{"--lang=en", "greet", "--name", "Ada", "--json"}, 0, "Hello, Ada!"},
		{[]string{"greet", "--lang=en", "--help"}, 0, "Usage:"},
		{[]string{"--lang=en", "schema", "greet"}, 0, "Greeting"},
		{[]string{"--lang=en", "version"}, 0, "version"},
		{[]string{"--lang=en", "greet"}, 2, "--name"},
		{[]string{"--lang=en", "greet", "--unknown"}, 2, "Unknown option"},
	} {
		code, out, errOut := invoke(t, tc.args...)
		require.Equal(t, tc.code, code, "%v: %s", tc.args, errOut)
		require.Contains(t, out+errOut, tc.want)
		if tc.code != 0 {
			require.True(t, strings.HasPrefix(errOut, "Error:"), errOut)
			require.Empty(t, out)
		}
	}
}

func TestLanguageScanRespectsFlagValuesAndSeparator(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"greet", "--name", "--lang", "--json"}, 0, "Hello, --lang!"},
		{[]string{"greet", "--name", "--lang=en"}, 0, "Hello, --lang=en!"},
		{[]string{"greet", "--name=x", "--", "--lang=en"}, 2, "Error:"},
		{[]string{"--lang", "zh", "greet", "--name=x"}, 0, "你好，x！"},
		{[]string{"__complete", "greet", "--lang", ""}, 0, ":4"},
	} {
		code, out, errOut := invoke(t, tc.args...)
		require.Equal(t, tc.code, code, "%v: %s", tc.args, errOut)
		require.Contains(t, out+errOut, tc.want)
	}
}

func TestRequiredAndGroupsBeforeConfiguration(t *testing.T) {
	for _, group := range []bool{false, true} {
		count := 0
		root := metadataRoot(Options{
			Out:    &bytes.Buffer{},
			ErrOut: &bytes.Buffer{},
			Dependencies: &Dependencies{
				LoadConfig: func(context.Context, config.Options) (config.Config, error) {
					count++
					return config.Config{}, nil
				},
			},
		})
		cmd := &cobra.Command{
			Use: "extra",
			RunE: func(*cobra.Command, []string) error {
				count++
				return nil
			},
		}
		cmd.Flags().String("a", "", "a")
		cmd.Flags().String("b", "", "b")
		if group {
			cmd.MarkFlagsRequiredTogether("a", "b")
		} else {
			require.NoError(t, cmd.MarkFlagRequired("a"))
		}
		root.AddCommand(cmd)
		args := []string{"extra"}
		if group {
			args = append(args, "--a=x")
		}
		root.SetArgs(args)
		require.Error(t, root.ExecuteContext(context.Background()))
		require.Zero(t, count)
	}
}

func TestLanguagePrecedenceAndIsolation(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			var out, stderr bytes.Buffer
			args := []string{"greet", "--name", "Ada"}
			lang := "en"
			want := "Hello, Ada!"
			if i%2 == 0 {
				args = append(args, "--lang", "zh")
				want = "你好，Ada！"
			}
			code := Run(context.Background(), args, Options{
				Name:        "sample",
				ConfigDir:   t.TempDir(),
				Directories: &paths.Directories{State: t.TempDir()},
				Out:         &out,
				ErrOut:      &stderr,
				LookupEnv: func(k string) (string, bool) {
					return lang, k == "SAMPLE_LANG"
				},
			})
			require.Zero(t, code, stderr.String())
			require.Contains(t, out.String(), want)
		})
	}
	wg.Wait()
	code, out, errOut := invoke(t, "--lang=de", "version")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	require.Contains(t, errOut, "de")
}

func TestMetadataLanguageScanForNewFlags(t *testing.T) {
	root := metadataRoot(Options{})
	cmd := &cobra.Command{Use: "extra"}
	cmd.Flags().StringP("source", "s", "", "来源")
	root.AddCommand(cmd)
	for _, args := range [][]string{
		{"extra", "--source", "--lang=en"},
		{"extra", "-s", "--lang=en"},
		{"extra", "-s--lang=en"},
		{"greet", "--name", "__complete", "--lang=en"},
	} {
		lang, err := selectLanguage(root, args, "zh-CN")
		require.NoError(t, err)
		want := "zh-CN"
		if args[0] == "greet" {
			want = "en"
		}
		require.Equal(t, want, lang, "%v", args)
	}
}

func TestInvalidExplicitValuesNeverLoadConfig(t *testing.T) {
	count := 0
	deps := Dependencies{LoadConfig: func(context.Context, config.Options) (config.Config, error) {
		count++
		return config.Defaults(), nil
	}}
	var out, stderr bytes.Buffer
	code := Run(
		context.Background(),
		[]string{"greet", "--name", "Ada", "--log-level", "noisy"},
		Options{Dependencies: &deps, Out: &out, ErrOut: &stderr},
	)
	require.Equal(t, 2, code)
	require.Zero(t, count)
	require.Empty(t, out.String())
}

func TestNoneLoggingAndEnglishDiagnostics(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[logging]\nlevel = \"debug\"\noutput = \"none\"\n"), 0o600))
	code, out, stderr := invoke(t, "greet", "--name", "Ada", "--lang=en", "--config", configPath)
	require.Zero(t, code)
	require.Contains(t, out, "Hello, Ada!")
	require.Empty(t, stderr)
	require.NoError(t, os.WriteFile(configPath, []byte("[logging]\noutput = \"stdout\"\n"), 0o600))
	code, out, stderr = invoke(t, "greet", "--name", "Ada", "--lang=en", "--config", configPath)
	require.Equal(t, 1, code)
	require.Empty(t, out)
	require.Contains(t, stderr, "Invalid configuration")
	require.NotContains(t, stderr, "日志")
}

func TestLanguageSelectionWithFlagsBeforeCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		code int
	}{
		{"值为语言", []string{"--name", "--lang=en", "greet"}, "Hello, --lang=en!", 0},
		{"值为未知语言", []string{"--name", "--lang=de", "greet"}, "Hello, --lang=de!", 0},
		{"布尔选项穿插", []string{"--json", "--name", "--lang=de", "greet"}, `{"message":"Hello, --lang=de!"}`, 0},
		{"显式布尔值", []string{"--json=false", "--name", "--lang=en", "greet"}, "Hello, --lang=en!", 0},
		{"实际语言选项优先", []string{"--name", "--lang=de", "--lang", "en", "greet"}, "Hello, --lang=de!", 0},
		{"实际语言在前", []string{"--lang=en", "--name", "--lang=de", "greet"}, "Hello, --lang=de!", 0},
		{"分隔符后的语言不生效", []string{"--name", "Ada", "greet", "--", "--lang=en"}, "Error:", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := invoke(t, tc.args...)
			require.Equal(t, tc.code, code, stderr)
			require.Contains(t, out+stderr, tc.want)
			if code == 0 {
				require.Empty(t, stderr)
			}
		})
	}
}

func TestLanguageSelectionMatchesTargetFlagMetadata(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		source     string
		lang       string
		toggle     bool
		positional []string
	}{
		{"前置长选项", []string{"--source", "--lang=de", "extra"}, "--lang=de", "zh-CN", false, nil},
		{"前置短选项", []string{"-s", "--lang=de", "extra"}, "--lang=de", "zh-CN", false, nil},
		{"短选项附值", []string{"-s--lang=de", "extra"}, "--lang=de", "zh-CN", false, nil},
		{"短选项组合", []string{"-bs", "--lang=de", "extra"}, "--lang=de", "zh-CN", true, nil},
		{"布尔等号赋值", []string{"--toggle=true", "--source", "--lang=de", "extra"}, "--lang=de", "zh-CN", true, nil},
		{"前置和后置选项", []string{"-bs", "--lang=de", "extra", "--lang=en"}, "--lang=de", "en", true, nil},
		{"分隔符", []string{"-s", "value", "extra", "--", "--lang=en"}, "value", "zh-CN", false, []string{"--lang=en"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := metadataRoot(Options{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
			var source string
			var toggle bool
			var positional []string
			cmd := &cobra.Command{
				Use:         "extra",
				Annotations: map[string]string{"skip-config": "true"},
				Args:        cobra.ArbitraryArgs,
				RunE: func(_ *cobra.Command, args []string) error {
					positional = args
					return nil
				},
			}
			cmd.Flags().StringVarP(&source, "source", "s", "", "来源")
			cmd.Flags().BoolVarP(&toggle, "toggle", "b", false, "切换")
			root.AddCommand(cmd)
			lang, err := selectLanguage(root, tc.args, "zh-CN")
			require.NoError(t, err)
			require.Equal(t, tc.lang, lang)
			root.SetArgs(tc.args)
			require.NoError(t, root.Execute())
			require.Equal(t, tc.source, source)
			require.Equal(t, tc.toggle, toggle)
			require.Equal(t, tc.positional, append([]string(nil), positional...))
		})
	}
}

func TestCompletionLanguageFallbackAndOrdinaryValidation(t *testing.T) {
	for _, tc := range []struct {
		env  string
		args []string
		want string
	}{
		{"invalid", []string{"__complete", ""}, "version\tShow"},
		{"zh-CN", []string{"__complete", "--lang=e", ""}, "version\t显示"},
		{"en", []string{"__complete", "--lang", "zh", "--log-level", ""}, "info\n"},
		{"en", []string{"__complete", "--lang", "zh", ""}, "version\t显示"},
	} {
		var out, stderr bytes.Buffer
		code := Run(context.Background(), tc.args, Options{
			Name:   "sample",
			Out:    &out,
			ErrOut: &stderr,
			LookupEnv: func(key string) (string, bool) {
				return tc.env, key == "SAMPLE_LANG"
			},
		})
		require.Zero(t, code, stderr.String())
		require.Empty(t, stderr.String())
		require.Contains(t, out.String(), tc.want)
	}
	for _, args := range [][]string{{"--lang=e", "version"}, {"--config", "__complete", "--lang=e", "version"}, {"greet", "--name", "__complete", "--lang=e"}, {"--", "__complete", "--lang=e"}} {
		code, out, stderr := invoke(t, args...)
		require.Equal(t, 2, code)
		require.Empty(t, out)
		require.NotEmpty(t, stderr)
	}
}

func TestConcurrentCompletionLanguageIsolation(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		for _, lang := range []string{"en", "zh-CN"} {
			wg.Go(func() {
				var out, stderr bytes.Buffer
				code := Run(context.Background(), []string{"__complete", "--lang", lang, ""}, Options{
					Name:   "sample",
					Out:    &out,
					ErrOut: &stderr,
					LookupEnv: func(string) (string, bool) {
						return "", false
					},
				})
				if code != 0 || stderr.Len() != 0 {
					t.Errorf("%s: code=%d stderr=%s", lang, code, stderr.String())
					return
				}
				want := "version\tShow"
				if lang == "zh-CN" {
					want = "version\t显示"
				}
				if !strings.Contains(out.String(), want) {
					t.Errorf("%s: %s", lang, out.String())
				}
			})
		}
	}
	wg.Wait()
}
