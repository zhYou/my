package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/locales"
)

func TestChineseHelpRegardlessOfLocale(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	for _, args := range [][]string{{}, {"--help"}, {"greet", "--help"}, {"help", "greet"}, {"version", "--help"}, {"schema", "--help"}, {"completion", "--help"}, {"help", "--help"}} {
		code, out, errOut := invoke(t, append([]string{"--lang", "zh"}, args...)...)
		if code != 0 || errOut != "" || !strings.Contains(out, "用法：") || !strings.Contains(out, "显示帮助") ||
			!strings.Contains(out, "日志级别") ||
			!strings.Contains(out, "默认值") {
			t.Errorf("参数=%v 退出码=%d 标准输出=%q 标准错误=%q", args, code, out, errOut)
		}
		for _, english := range []string{"Usage:", "Available Commands:", "Flags:", "Global Flags:", "help for", "(default", "[command]", "NAME"} {
			if strings.Contains(out, english) {
				t.Errorf("参数=%v 帮助仍包含 %q：%s", args, english, out)
			}
		}
	}
}

func TestCorruptCatalogStopsBeforeHelpOutput(t *testing.T) {
	for name, data := range map[string]string{
		"invalid TOML":     "[broken",
		"invalid template": "[\"fixture.message\"]\nother = \"{{\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			loader := func() (*messages.Bundle, error) {
				return messages.NewBundle(fstest.MapFS{"active.en.toml": {Data: []byte(data)}})
			}
			_, _, err := buildRoot(defaults(Options{LookupEnv: noFixtureEnv}), loader)
			require.ErrorContains(t, err, "active.en.toml")
			var out, stderr bytes.Buffer
			code := run(
				context.Background(),
				[]string{"--help"},
				Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
				loader,
			)
			require.Equal(t, 1, code, stderr.String())
			require.Empty(t, out.String())
			require.Contains(t, stderr.String(), "active.en.toml")
		})
	}
}

func TestHelpRenderingFailureIsRuntimeAndLeavesStdoutEmpty(t *testing.T) {
	for _, tc := range []struct {
		name        string
		language    string
		replacement string
	}{
		{"missing translation", "en", ""},
		{"template execution error", "en", "other = \"prefix {{index . 1}} suffix\"\n"},
		{"missing template parameter", "en", "other = \"prefix {{.Missing}} suffix\"\n"},
		{"missing Chinese translation", "zh-CN", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bundle := catalogFixture(t, tc.language, "help.usage", tc.replacement)
			loader := func() (*messages.Bundle, error) {
				return bundle, nil
			}
			var out, stderr bytes.Buffer
			code := run(
				context.Background(),
				[]string{"--lang", tc.language, "--help"},
				Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
				loader,
			)
			require.Equal(t, 1, code, stderr.String())
			require.Empty(t, out.String())
			require.Contains(t, stderr.String(), "help.usage")
		})
	}
}

func TestAllHelpCallbacksPropagateRenderingFailure(t *testing.T) {
	bundle := catalogFixture(t, "en", "help.usage", "other = \"prefix {{.Missing}} suffix\"\n")
	loader := func() (*messages.Bundle, error) {
		return bundle, nil
	}
	for _, args := range [][]string{{}, {"--help"}, {"greet", "--help"}, {"help", "greet"}, {"completion"}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), args, Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv}, loader)
		require.Equal(t, 1, code, "%v: %s", args, stderr.String())
		require.Empty(t, out.String(), "%v", args)
		require.Contains(t, stderr.String(), "help.usage")
	}
}

func TestExistingHelpAndVersionFlagsKeepTheirDescriptions(t *testing.T) {
	for _, flag := range []string{"help", "version"} {
		for _, scope := range []string{"local", "persistent", "inherited"} {
			t.Run(flag+"/"+scope, func(t *testing.T) {
				var out bytes.Buffer
				root, call, err := buildRoot(defaults(Options{Out: &out, LookupEnv: noFixtureEnv}), locales.LoadBundle)
				require.NoError(t, err)
				cmd := &cobra.Command{
					Use: "extra",
					RunE: func(*cobra.Command, []string) error {
						return nil
					},
				}
				if flag == "version" {
					cmd.Version = "dev"
					cmd.Flags().Bool("help", false, "custom help description")
				}
				description := "custom " + flag + " description"
				var registered *pflag.Flag
				args := []string{"extra", "--help"}
				switch scope {
				case "local":
					cmd.Flags().Bool(flag, false, description)
					registered = cmd.Flags().Lookup(flag)
					root.AddCommand(cmd)
				case "persistent":
					cmd.PersistentFlags().Bool(flag, false, description)
					registered = cmd.PersistentFlags().Lookup(flag)
					root.AddCommand(cmd)
				case "inherited":
					parent := &cobra.Command{Use: "parent"}
					parent.PersistentFlags().Bool(flag, false, description)
					registered = parent.PersistentFlags().Lookup(flag)
					parent.AddCommand(cmd)
					root.AddCommand(parent)
					args = []string{"parent", "extra", "--help"}
				}
				configureHelp(cmd, call)
				require.NoError(t, call.localizeTree(root, "en"))
				root.SetArgs(args)
				require.NoError(t, root.Execute())
				require.Contains(t, out.String(), description)
				require.Equal(t, description, registered.Usage)
			})
		}
	}
}

func TestDefaultHelpAndVersionFlagsAreLocalized(t *testing.T) {
	for _, tc := range []struct {
		lang    string
		help    string
		version string
	}{
		{"en", "Show help", "Show version information"},
		{"zh", "显示帮助", "显示版本信息"},
	} {
		code, out, stderr := invoke(t, "--lang", tc.lang, "--help")
		require.Zero(t, code, stderr)
		require.Contains(t, out, tc.help)
		require.Contains(t, out, tc.version)
	}
}

func TestAllCommandsHaveLocalizedExamplesWithInvocationName(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		for _, path := range [][]string{{}, {"help"}, {"version"}, {"schema"}, {"completion"}, {"greet"}} {
			args := append([]string{"--lang", lang}, path...)
			args = append(args, "--help")
			code, out, stderr := invoke(t, args...)
			require.Zero(t, code, stderr)
			require.Empty(t, stderr)
			heading := "Examples:"
			if lang == "zh-CN" {
				heading = "示例："
			}
			require.Contains(t, out, heading)
			require.Contains(t, out, "testcli ")
		}
	}
}
