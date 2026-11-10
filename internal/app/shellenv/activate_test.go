package shellenv

import (
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
	_, err := Render("nu", []ResolvedVariable{{Name: "EDITOR", Value: "nvim"}})
	require.Error(t, err)
	_, err = Render("bash", []ResolvedVariable{
		{Name: "EDITOR", Value: "nvim"},
		{Name: "PATH", Value: "/tmp"},
	})
	require.Error(t, err)
}
