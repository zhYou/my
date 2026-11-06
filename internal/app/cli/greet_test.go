package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGreetOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"text", []string{"greet", "--name", "Codex"}, "Hello, Codex!\n"},
		{"json", []string{"greet", "--name", "Codex", "--json"}, "{\"message\":\"Hello, Codex!\"}\n"},
		{"unicode", []string{"--json", "greet", "--name", "周游"}, "{\"message\":\"Hello, 周游!\"}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := invoke(t, tc.args...)
			if code != 0 || out != tc.want || errOut != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
			}
		})
	}
}

func TestGreetExampleUsesInvocationName(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		code, out, stderr := invoke(t, "--lang", lang, "greet", "--help")
		require.Zero(t, code, stderr)
		require.Contains(t, out, "testcli greet --name Codex")
		require.Contains(t, out, "testcli greet --name Codex --json")
	}
}
