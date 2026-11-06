package cli

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/locales"
)

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return invokeContext(context.Background(), t, args...)
}

func invokeContext(ctx context.Context, t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(ctx, args, Options{
		Name:        "testcli",
		Version:     "v1.2.3",
		Commit:      "abc123",
		Date:        "2026-10-28",
		Out:         &out,
		ErrOut:      &errOut,
		ConfigDir:   t.TempDir(),
		Directories: &paths.Directories{State: t.TempDir()},
		LookupEnv: func(string) (string, bool) {
			return "", false
		},
	})
	return code, out.String(), errOut.String()
}

// metadataRoot 仅用于无业务执行的命令元数据测试。业务测试统一通过 Run 托管资源。
func metadataRoot(opts Options) *cobra.Command {
	root, _, err := buildRoot(defaults(opts), locales.LoadBundle)
	if err != nil {
		panic(err)
	}
	return root
}

// isolatedOptions 显式注入配置、目录与环境；任何 argv 都不读取或写入用户状态。
func isolatedOptions(home string, out, stderr io.Writer) Options {
	dirs := paths.Directories{
		Config: home,
		Data:   filepath.Join(home, "data"),
		State:  filepath.Join(home, "state"),
		Cache:  filepath.Join(home, "cache"),
	}
	return Options{
		Directories: &dirs,
		Out:         out,
		ErrOut:      stderr,
		LookupEnv: func(key string) (string, bool) {
			if key == "MY_HOME" {
				return home, true
			}
			return "", false
		},
		Dependencies: &Dependencies{LoadConfig: func(context.Context, config.Options) (config.Config, error) {
			cfg := config.Defaults()
			cfg.Logging.Output = "none"
			return cfg, nil
		}},
	}
}

func catalogFixture(t *testing.T, language, id, replacement string) *messages.Bundle {
	t.Helper()
	files := fstest.MapFS{}
	for _, lang := range []string{"en", "zh-CN"} {
		name := "active." + lang + ".toml"
		data, err := fs.ReadFile(locales.Catalog(), name)
		require.NoError(t, err)
		if lang == language {
			content := string(data)
			header := "[\"" + id + "\"]\n"
			start := strings.Index(content, header)
			require.NotEqual(t, -1, start)
			end := strings.Index(content[start+len(header):], "\n[")
			require.NotEqual(t, -1, end)
			end += start + len(header) + 1
			section := ""
			if replacement != "" {
				section = header + replacement + "\n"
			}
			data = []byte(content[:start] + section + content[end:])
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	bundle, err := messages.NewBundle(files)
	require.NoError(t, err)
	return bundle
}

func catalogWithCommandFixtures(t *testing.T) *messages.Bundle {
	t.Helper()
	extra := map[string]string{
		"en": `
["fixture.alpha.short"]
other = "Alpha short"
["fixture.alpha.long"]
other = "Alpha long description"
["fixture.alpha.use"]
other = "alpha <item>"
["fixture.alpha.example"]
other = "Example alpha"
["fixture.alpha.flag"]
other = "Alpha name"
["fixture.beta.short"]
other = "Beta short"
["fixture.beta.long"]
other = "Beta long description"
["fixture.beta.use"]
other = "beta <item>"
["fixture.beta.example"]
other = "Example beta"
["fixture.beta.flag"]
other = "Beta name"
`,
		"zh-CN": `
["fixture.alpha.short"]
other = "甲命令"
["fixture.alpha.long"]
other = "甲命令长说明"
["fixture.alpha.use"]
other = "alpha <项目>"
["fixture.alpha.example"]
other = "甲命令示例"
["fixture.alpha.flag"]
other = "甲名称"
["fixture.beta.short"]
other = "乙命令"
["fixture.beta.long"]
other = "乙命令长说明"
["fixture.beta.use"]
other = "beta <项目>"
["fixture.beta.example"]
other = "乙命令示例"
["fixture.beta.flag"]
other = "乙名称"
`,
	}
	files := fstest.MapFS{}
	for lang, addition := range extra {
		name := "active." + lang + ".toml"
		data, err := fs.ReadFile(locales.Catalog(), name)
		require.NoError(t, err)
		files[name] = &fstest.MapFile{Data: append(data, []byte(addition)...)}
	}
	bundle, err := messages.NewBundle(files)
	require.NoError(t, err)
	return bundle
}

func noFixtureEnv(string) (string, bool) {
	return "", false
}
