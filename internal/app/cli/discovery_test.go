package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChineseVersionAndSchema(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		code, out, errOut := invoke(t, append([]string{"--lang", "zh"}, args...)...)
		if code != 0 || !strings.Contains(out, "版本") || !strings.Contains(out, "v1.2.3") || errOut != "" {
			t.Fatalf("参数=%v 退出码=%d 标准输出=%q 标准错误=%q", args, code, out, errOut)
		}
	}
	code, out, errOut := invoke(t, "--lang", "zh", "schema", "greet")
	var got struct {
		Commands []commandSchema `json:"commands"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if code != 0 || errOut != "" || len(got.Commands) != 1 || !strings.Contains(got.Commands[0].Description, "问候") {
		t.Fatalf("退出码=%d 标准输出=%q 标准错误=%q", code, out, errOut)
	}
}

func TestDiscoveryBypassesBrokenConfiguration(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(bad, []byte("[broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{},
		{"--help"},
		{"greet", "--help"},
		{"version", "--json"},
		{"--version"},
		{"schema"},
		{"help", "greet"},
		{"completion", "bash"},
		{"completion", "zsh"},
		{"completion", "fish"},
		{"completion", "powershell"},
	} {
		code, out, errOut := invoke(t, append([]string{"--config", bad}, args...)...)
		if code != 0 || out == "" || errOut != "" {
			t.Errorf("args=%v code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
	code, out, errOut := invoke(t, "greet", "--name", "Codex", "--config", bad)
	if code != 1 || out != "" || errOut == "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestCompletionArityUsesPluralCatalog(t *testing.T) {
	code, out, stderr := invoke(t, "completion", "bash", "extra")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	require.Contains(t, stderr, "received 2 arguments")
	require.NotContains(t, stderr, "Operation failed")
}

func TestCompletionCandidatesAndQuietProtocol(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"language", []string{"__complete", "--lang", ""}, []string{"en\n", "zh-CN\n", "zh\n", ":4\n"}},
		{"level", []string{"__complete", "--log-level", ""}, []string{"debug\n", "info\n", "warn\n", "error\n", ":4\n"}},
		{"help", []string{"__complete", "help", ""}, []string{"completion\t", "greet\t", "version\t", ":4\n"}},
		{"nested", []string{"__complete", "help", "completion", ""}, []string{"bash\t", "zsh\t", "fish\t", "powershell\t", ":4\n"}},
		{"partial", []string{"__complete", "--lang", "e", "--log-level", ""}, []string{"debug\n", ":4\n"}},
		{"partial-equals", []string{"__complete", "--lang=e", "--log-level", ""}, []string{"debug\n", ":4\n"}},
		{"no-description", []string{"__completeNoDesc", "help", "completion", ""}, []string{"bash\n", "powershell\n", ":4\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := invoke(t, tc.args...)
			require.Zero(t, code, stderr)
			require.Empty(t, stderr)
			for _, want := range tc.want {
				require.Contains(t, out, want)
			}
			if tc.name == "no-description" {
				require.NotContains(t, out, "\t")
			}
		})
	}
}

func TestShellSubcommandsHaveHelpSchemaAndScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			for _, args := range [][]string{{"help", "completion", shell}, {"completion", shell, "--help"}} {
				code, out, stderr := invoke(t, args...)
				require.Zero(t, code, stderr)
				require.Empty(t, stderr)
				require.Contains(t, out, "testcli completion "+shell)
				require.Contains(t, out, "Examples:")
			}
			code, out, stderr := invoke(t, "schema", "completion", shell)
			require.Zero(t, code, stderr)
			var doc struct {
				Commands []commandSchema `json:"commands"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &doc))
			require.Len(t, doc.Commands, 1)
			require.Equal(t, "testcli completion "+shell, doc.Commands[0].Path)
			code, out, stderr = invoke(t, "completion", shell)
			require.Zero(t, code, stderr)
			require.Empty(t, stderr)
			require.Contains(t, out, "testcli")
			legacyCode, legacyOut, legacyErr := invoke(t, "completion", "--", shell)
			require.Zero(t, legacyCode, legacyErr)
			require.Empty(t, legacyErr)
			require.Equal(t, out, legacyOut)

			code, out, stderr = invoke(t, "completion", shell, "extra")
			require.Equal(t, 2, code)
			require.Empty(t, out)
			require.NotEmpty(t, stderr)
		})
	}
}

func TestCompletionNeverLoadsConfigurationOrCreatesState(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{{"completion", "bash"}, {"help", "completion", "bash"}, {"__complete", "help", ""}, {"__complete", "--lang=e", "--log-level", ""}} {
		var out, stderr bytes.Buffer
		code := Run(
			context.Background(),
			append([]string{"--config", filepath.Join(home, "missing.toml")}, args...),
			Options{
				Name:   "sample",
				Out:    &out,
				ErrOut: &stderr,
				LookupEnv: func(key string) (string, bool) {
					return home, key == "SAMPLE_HOME"
				},
			},
		)
		require.Zero(t, code, stderr.String())
		require.Empty(t, stderr.String())
		_, err := os.Stat(home)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}
