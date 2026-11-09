package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultTargetDefinitions(t *testing.T) {
	targets := DefaultTargets()
	require.Len(t, targets, 10)
	seen := map[TargetID]bool{}
	for _, target := range targets {
		require.NoError(t, target.Validate())
		require.False(t, seen[target.ID])
		seen[target.ID] = true
	}
	require.True(t, targetsByID()[TargetCodexAuth].Secret)
	require.False(t, targetsByID()[TargetCodexAuth].CaptureAll)
	require.True(t, targetsByID()[TargetShellEnv].RemoteAuthoritative)
}

func TestReadMiseFieldUnitsIgnoresUnmanagedContent(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "mise", "config.toml"), `[tools]
node = "24"
java = ["21", "25"]

[settings]
http_timeout = "120s"

[tasks.unmanaged]
run = "echo keep"
`)
	definition := targetsByID()[TargetMise]
	snapshot, err := definition.ReadLocal(home, "linux")
	require.NoError(t, err)
	require.Equal(t, stringValue("24").Digest, snapshot.Units[FieldUnitID("tools", "node")].Digest)
	require.True(t, snapshot.Units[FieldUnitID("tools", "java")].Exists)
	require.Equal(t, stringValue("120s").Digest, snapshot.Units[FieldUnitID("settings", "http_timeout")].Digest)
	_, exists := snapshot.Units[FieldUnitID("tasks", "unmanaged")]
	require.False(t, exists)
}

func TestReadCodexRulesAndDetectLegacyFile(t *testing.T) {
	home := t.TempDir()
	rules := filepath.Join(home, ".codex", "rules")
	writeFixture(t, filepath.Join(rules, "go.rules"), "prefix_rule(pattern=[\"go\"], decision=\"allow\")\n")
	writeFixture(
		t,
		filepath.Join(rules, "git-rtk.rules"),
		"prefix_rule(pattern=[\"rtk\", \"git\"], decision=\"allow\")\n",
	)
	definition := targetsByID()[TargetCodexRules]
	_, err := definition.ReadLocal(home, "linux")
	require.Error(t, err)
	require.Contains(t, err.Error(), "configuration.legacy")
}

func TestCodexFilesystemLogicalPathsAreStableAcrossPlatforms(t *testing.T) {
	linux := codexFilesystemUnits("linux")
	darwin := codexFilesystemUnits("darwin")
	windows := codexFilesystemUnits("windows")
	require.Equal(t, "~/.m2", linux["m2"])
	require.Equal(t, "~/Library/Caches/Coursier", darwin["coursier-cache"])
	require.Equal(t, "~/AppData/Local/go-build", windows["go-build-cache"])
	require.NotContains(t, linux, "machine-config")
}

func TestTargetDataRejectsMalformedJSONAndYAML(t *testing.T) {
	require.Error(t, validateTargetData(TargetFile{Format: FormatJSON}, []byte("{broken")))
	require.Error(t, validateTargetData(TargetFile{Format: FormatYAML}, []byte("key: [unterminated\n")))
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
