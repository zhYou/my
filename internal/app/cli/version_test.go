package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/messages"
)

func TestVersionJSON(t *testing.T) {
	code, out, errOut := invoke(t, "version", "--json")
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if code != 0 || got["version"] != "v1.2.3" || got["commit"] != "abc123" || got["name"] != "testcli" ||
		errOut != "" {
		t.Fatalf("code=%d result=%v stderr=%q", code, got, errOut)
	}
}

func TestVersionRenderingFailureLeavesStdoutEmpty(t *testing.T) {
	bundle := catalogFixture(t, "en", "cli.version_text", "other = \"prefix {{.Missing}} suffix\"\n")
	loader := func() (*messages.Bundle, error) {
		return bundle, nil
	}
	var out, stderr bytes.Buffer
	code := run(
		context.Background(),
		[]string{"version"},
		Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
		loader,
	)
	require.Equal(t, 1, code, stderr.String())
	require.Empty(t, out.String())
	require.Contains(t, stderr.String(), "cli.version_text")
}

// 翻译中的合法字面模板分隔符及元数据必须只渲染一次。
func TestVersionFlagRendersLiteralTextOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		template  string
		nameValue string
		version   string
		want      string
	}{
		{"opening delimiter", `prefix {{"{{"}}`, "app", "v1", "prefix {{\n"},
		{"literal field", `prefix {{"{{.Missing}}"}}`, "app", "v1", "prefix {{.Missing}}\n"},
		{"actual metadata", `{{.Name}} {{.Version}}`, "app{{.Name}}", "v{{.Missing}}", "app{{.Name}} v{{.Missing}}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replacement := "other = " + strconv.Quote(tc.template) + "\n"
			bundle := catalogFixture(t, "en", "cli.version_template", replacement)
			var out, stderr bytes.Buffer
			var code int
			require.NotPanics(t, func() {
				code = run(
					context.Background(),
					[]string{"--version"},
					Options{
						Name:      tc.nameValue,
						Version:   tc.version,
						Out:       &out,
						ErrOut:    &stderr,
						LookupEnv: noFixtureEnv,
					},
					func() (*messages.Bundle, error) {
						return bundle, nil
					},
				)
			})
			require.Zero(t, code, stderr.String())
			require.Equal(t, tc.want, out.String())
			require.Empty(t, stderr.String())
		})
	}
}

func TestVersionFlagRenderingFailureLeavesStdoutEmpty(t *testing.T) {
	bundle := catalogFixture(t, "en", "cli.version_template", "other = \"prefix {{.Missing}}\"\n")
	var out, stderr bytes.Buffer
	code := run(
		context.Background(),
		[]string{"--version"},
		Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
		func() (*messages.Bundle, error) {
			return bundle, nil
		},
	)
	require.Equal(t, 1, code, stderr.String())
	require.Empty(t, out.String())
	require.Contains(t, stderr.String(), "cli.version_template")
}

// 独立版本的每个命令使用自身身份，并保留翻译和元数据的字面模板标记。
func TestVersionUsesExecutingCommandMetadata(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--version"}, "prefix {{ parent parent-v1\n"},
		{[]string{"child{{.Name}}", "--version"}, "prefix {{ child{{.Name}} v{{.Missing}}\n"},
		{[]string{"child{{.Name}}", "grandchild", "--version"}, "prefix {{ grandchild grand-v3\n"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			bundle := catalogFixture(
				t,
				"en",
				"cli.version_template",
				"other = "+strconv.Quote(`prefix {{"{{"}} {{.Name}} {{.Version}}`)+"\n",
			)
			var out bytes.Buffer
			root, call, err := buildRoot(
				defaults(Options{Name: "parent", Version: "parent-v1", Out: &out, LookupEnv: noFixtureEnv}),
				func() (*messages.Bundle, error) {
					return bundle, nil
				},
			)
			require.NoError(t, err)
			child := &cobra.Command{
				Use:     "child{{.Name}}",
				Version: "v{{.Missing}}",
				RunE: func(*cobra.Command, []string) error {
					return nil
				},
			}
			grandchild := &cobra.Command{
				Use:     "grandchild",
				Version: "grand-v3",
				RunE: func(*cobra.Command, []string) error {
					return nil
				},
			}
			child.AddCommand(grandchild)
			root.AddCommand(child)
			configureHelp(child, call)
			require.NoError(t, call.localizeTree(root, "en"))
			root.SetArgs(tc.args)
			require.NotPanics(t, func() {
				require.NoError(t, root.Execute())
			})
			require.Equal(t, tc.want, out.String())
		})
	}
}
