package toolchain

import (
	"debug/buildinfo"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRejectsUnpinnedRunnerVersion(t *testing.T) {
	for _, version := range []string{"latest", "3", "^3.9.10", "3.9.10-rc.1"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "mise.toml"), []byte(
				"min_version = \"2026.10.21\"\n[tools]\ngo = \"1.27.3\"\n[vars]\nprettier_version = \""+version+"\"\n",
			), 0o600))
			_, err := Load(root)
			require.ErrorContains(t, err, "必须固定稳定的精确版本")
		})
	}
}

func TestGovulncheckBuildInfoExact(t *testing.T) {
	valid := buildinfo.BuildInfo{
		Path: "golang.org/x/vuln/cmd/govulncheck",
		Main: debug.Module{Path: "golang.org/x/vuln", Version: "v1.9.0"},
	}
	require.NoError(t, MatchGovulncheckBuildInfo(&valid, "1.9.0"))
	for _, mutate := range []func(*buildinfo.BuildInfo){
		func(b *buildinfo.BuildInfo) {
			b.Path = "example.org/other"
		},
		func(b *buildinfo.BuildInfo) {
			b.Main.Path = "example.org/other"
		},
		func(b *buildinfo.BuildInfo) {
			b.Main.Version = "v1.9.00"
		},
		func(b *buildinfo.BuildInfo) {
			b.Main.Version = "v1.9.0-rc.1"
		},
		func(b *buildinfo.BuildInfo) {
			b.Main.Replace = &debug.Module{Path: "local"}
		},
	} {
		b := valid
		mutate(&b)
		require.Error(t, MatchGovulncheckBuildInfo(&b, "1.9.0"))
	}
	spec, err := Spec("go:golang.org/x/vuln/cmd/govulncheck")
	require.NoError(t, err)
	require.Equal(t, []string{"-h"}, spec.Args)
}

func TestVersionRejectsPrereleaseOrBuildSuffix(t *testing.T) {
	for _, version := range []string{"1.27.30", "1.27.3-rc.1", "1.27.3+patched"} {
		require.Error(t, Match("go", "go version go"+version+" linux/amd64", "1.27.3"))
	}
}

func TestGoplsVersionMismatchSuggestsSetup(t *testing.T) {
	err := Match("go:golang.org/x/tools/gopls", "golang.org/x/tools/gopls v0.24.1", "0.24.0")
	require.ErrorContains(t, err, "mise run setup")
	require.NotContains(t, err.Error(), ":agent")
}

func TestTextToolVersions(t *testing.T) {
	cases := []struct {
		key     string
		output  string
		version string
	}{
		{"node", "v26.13.0", "26.13.0"},
		{"python", "Python 3.15.2", "3.15.2"},
		{"uv", "uv 0.13.7", "0.13.7"},
		{"taplo", "taplo 0.10.0", "0.10.0"},
	}
	for _, tt := range cases {
		t.Run(tt.key, func(t *testing.T) {
			require.NoError(t, Match(tt.key, tt.output, tt.version))
			require.Error(t, Match(tt.key, tt.output, "99.99.99"))
			require.Error(t, Match(tt.key, strings.Replace(tt.output, tt.version, tt.version+"-rc.1", 1), tt.version))
		})
	}
}

func TestActionlintVersion(t *testing.T) {
	spec, err := Spec("actionlint")
	require.NoError(t, err)
	require.Equal(t, "actionlint", spec.Name)
	require.Equal(t, []string{"-version"}, spec.Args)
	require.NoError(t, Match("actionlint", "1.7.12\ninstalled by downloading from release page\n", "1.7.12"))
	for _, output := range []string{"1.7.13", "1.7.120", "1.7.12-rc.1", "1.7.12+patched", "invalid"} {
		t.Run(output, func(t *testing.T) {
			require.ErrorContains(t, Match("actionlint", output, "1.7.12"), "mise run setup")
		})
	}
}
