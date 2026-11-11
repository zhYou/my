package shellenv

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderShellVariables(t *testing.T) {
	variables := []ResolvedVariable{
		{Name: "EDITOR", Value: "a b"},
		{Name: "QUOTE", Value: "a'b"},
	}
	tests := []struct {
		shell string
		want  string
	}{
		{"bash", "export EDITOR='a b'\nexport QUOTE='a'\\''b'\n"},
		{"zsh", "export EDITOR='a b'\nexport QUOTE='a'\\''b'\n"},
		{"fish", "set -gx EDITOR 'a b'\nset -gx QUOTE 'a\\'b'\n"},
		{"powershell", "$env:EDITOR = 'a b'\n$env:QUOTE = 'a''b'\n"},
	}
	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			got, err := Render(tc.shell, variables)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRenderRejectsUnknownShellAndDangerousNameWithoutPartialOutput(t *testing.T) {
	output, err := Render("nu", []ResolvedVariable{{Name: "EDITOR", Value: "nvim"}})
	require.Error(t, err)
	require.Empty(t, output)
	output, err = Render("bash", []ResolvedVariable{
		{Name: "EDITOR", Value: "nvim"},
		{Name: "PATH", Value: "/tmp"},
	})
	require.Error(t, err)
	require.Empty(t, output)
}

func TestRenderedVariablesRoundTripThroughAvailableShells(t *testing.T) {
	value := "a'b \\ $() `quoted` 中文"
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			program := shell
			if shell == "powershell" {
				program = "pwsh"
			}
			path, err := exec.LookPath(program)
			if err != nil {
				t.Skip("当前宿主未安装 " + program)
			}
			output, err := Render(shell, []ResolvedVariable{{Name: "ROUNDTRIP", Value: value}})
			require.NoError(t, err)
			var args []string
			switch shell {
			case "bash":
				args = []string{"--noprofile", "--norc", "-c", output + `printf '%s' "$ROUNDTRIP"`}
			case "zsh":
				args = []string{"-f", "-c", output + `printf '%s' "$ROUNDTRIP"`}
			case "fish":
				args = []string{"--no-config", "-c", output + `printf '%s' "$ROUNDTRIP"`}
			case "powershell":
				args = []string{
					"-NoLogo",
					"-NoProfile",
					"-NonInteractive",
					"-Command",
					output + `[Console]::Write($env:ROUNDTRIP)`,
				}
			}
			// #nosec G204 -- 测试执行已发现的 shell，仅使用合成 fixture，不读取 profile。
			cmd := exec.CommandContext(t.Context(), path, args...)
			home := t.TempDir()
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if key != "HOME" && key != "USERPROFILE" && key != "MY_HOME" && key != "XDG_CONFIG_HOME" {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "HOME="+home, "USERPROFILE="+home, "MY_HOME="+home, "XDG_CONFIG_HOME="+home)
			data, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", data)
			require.Equal(t, value, string(data))
		})
	}
}
