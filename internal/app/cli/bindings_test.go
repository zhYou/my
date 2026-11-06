package cli

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/locales"
)

func TestUnboundFlagWithSameNameKeepsItsDescription(t *testing.T) {
	var out bytes.Buffer
	root, call, err := buildRoot(defaults(Options{Out: &out, LookupEnv: noFixtureEnv}), locales.LoadBundle)
	require.NoError(t, err)
	foreign := &cobra.Command{
		Use:   "extra",
		Short: "extra",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}
	foreign.Flags().String("name", "", "unrelated name")
	root.AddCommand(foreign)
	require.NoError(t, call.localizeTree(root, call.localizer.Language()))
	root.SetArgs([]string{"extra", "--help"})
	require.NoError(t, root.Execute())
	require.Contains(t, out.String(), "unrelated name")
}

func TestMetadataRenderingFailureStopsBeforeOutput(t *testing.T) {
	for _, id := range []string{"cli.greet", "cli.greet_use", "cli.greet_example", "cli.name", "cli.version_template"} {
		t.Run(id, func(t *testing.T) {
			bundle := catalogFixture(t, "zh-CN", id, "other = \"prefix {{.Missing}} suffix\"\n")
			loader := func() (*messages.Bundle, error) {
				return bundle, nil
			}
			var out, stderr bytes.Buffer
			code := run(
				context.Background(),
				[]string{"--lang", "zh", "greet", "--help"},
				Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
				loader,
			)
			require.Equal(t, 1, code, stderr.String())
			require.Empty(t, out.String())
			require.Contains(t, stderr.String(), id)
		})
	}
}

func TestCommandAndFlagBindingsRenderIndependently(t *testing.T) {
	bundle := catalogWithCommandFixtures(t)
	for _, tc := range []struct {
		language    string
		command     string
		description string
		usage       string
		example     string
		flag        string
		otherFlag   string
	}{
		{"en", "alpha", "Alpha long description", "alpha <item>", "Example alpha", "Alpha name", "Beta name"},
		{"en", "beta", "Beta long description", "beta <item>", "Example beta", "Beta name", "Alpha name"},
		{"zh", "alpha", "甲命令长说明", "alpha <项目>", "甲命令示例", "甲名称", "乙名称"},
		{"zh", "beta", "乙命令长说明", "beta <项目>", "乙命令示例", "乙名称", "甲名称"},
	} {
		t.Run(tc.language+"/"+tc.command, func(t *testing.T) {
			var out bytes.Buffer
			loader := func() (*messages.Bundle, error) {
				return bundle, nil
			}
			root, call, err := buildRoot(
				defaults(Options{Name: "testcli", Language: tc.language, Out: &out, LookupEnv: noFixtureEnv}),
				loader,
			)
			require.NoError(t, err)
			for _, name := range []string{"alpha", "beta"} {
				cmd := &cobra.Command{
					Use:   name,
					Short: name,
					RunE: func(*cobra.Command, []string) error {
						return nil
					},
				}
				cmd.Flags().String("name", "", "original")
				root.AddCommand(cmd)
				call.bindCommand(
					cmd,
					commandTexts{
						short:   message("fixture." + name + ".short"),
						long:    message("fixture." + name + ".long"),
						use:     message("fixture." + name + ".use"),
						example: message("fixture." + name + ".example"),
					},
				)
				call.bindFlag(cmd.Flags().Lookup("name"), message("fixture."+name+".flag"))
				configureHelp(cmd, call)
			}
			require.NoError(t, call.localizeTree(root, tc.language))
			root.SetArgs([]string{tc.command, "--help"})
			require.NoError(t, root.Execute())
			require.NoError(t, call.helpErr)
			require.Contains(t, out.String(), tc.description)
			require.Contains(t, out.String(), tc.usage)
			require.Contains(t, out.String(), tc.example)
			require.Contains(t, out.String(), tc.flag)
			require.NotContains(t, out.String(), tc.otherFlag)
		})
	}
}

func TestTranslatedUsageCannotChangeCommandName(t *testing.T) {
	for _, tc := range []struct {
		name string
		use  string
	}{
		{"different name", "salute --name NAME"},
		{"leading space", " greet --name NAME"},
		{"tab in name", "greet\t--name NAME"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bundle := catalogFixture(t, "en", "cli.greet_use", "other = "+strconv.Quote(tc.use)+"\n")
			loader := func() (*messages.Bundle, error) {
				return bundle, nil
			}
			t.Run("construct", func(t *testing.T) {
				root, _, err := buildRoot(defaults(Options{LookupEnv: noFixtureEnv}), loader)
				require.ErrorContains(t, err, "cli.greet_use")
				require.Equal(t, "供用户和自动化工具使用的 CLI 模板", root.Short)
				greet, _, findErr := root.Find([]string{"greet"})
				require.NoError(t, findErr)
				require.Equal(t, "greet --name 姓名", greet.Use)
			})
			t.Run("run", func(t *testing.T) {
				var out, stderr bytes.Buffer
				code := run(
					context.Background(),
					[]string{"greet", "--help"},
					Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
					loader,
				)
				require.Equal(t, 1, code, stderr.String())
				require.Empty(t, out.String())
				require.Contains(t, stderr.String(), "cli.greet_use")
			})
		})
	}
}

func TestLongDescriptionRenderingFailureIsReturned(t *testing.T) {
	var out bytes.Buffer
	root, call, err := buildRoot(defaults(Options{Out: &out, LookupEnv: noFixtureEnv}), locales.LoadBundle)
	require.NoError(t, err)
	cmd := &cobra.Command{Use: "extra", Long: "original description"}
	root.AddCommand(cmd)
	call.bindCommand(cmd, commandTexts{long: message("cli.version_text")})
	err = call.localizeTree(root, "en")
	require.ErrorContains(t, err, "cli.version_text")
	require.Equal(t, "original description", cmd.Long)
	require.Empty(t, out.String())
}

// 子命令版本格式化失败时，命令树和输出均不应被部分提交。
func TestChildVersionRenderFailureDoesNotMutateTree(t *testing.T) {
	bundle := catalogFixture(t, "en", "cli.version_template", `other = '{{index .Name 1}}'`+"\n")
	var out bytes.Buffer
	root, call, err := buildRoot(
		defaults(Options{Name: "parent", Version: "v1", Out: &out, LookupEnv: noFixtureEnv}),
		func() (*messages.Bundle, error) {
			return bundle, nil
		},
	)
	require.NoError(t, err)
	before := root.VersionTemplate()
	child := &cobra.Command{Use: "x", Version: "v2"}
	root.AddCommand(child)
	require.ErrorContains(t, call.localizeTree(root, "en"), "cli.version_template")
	require.Equal(t, before, root.VersionTemplate())
	require.Empty(t, out.String())
}
