package target

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultTargetDefinitions(t *testing.T) {
	targets := DefaultDefinitions()
	require.Len(t, targets, 10)
	seen := map[ID]bool{}
	for _, target := range targets {
		require.NoError(t, target.Validate())
		require.False(t, seen[target.ID])
		seen[target.ID] = true
	}
	require.True(t, targetsByID()[CodexAuth].Secret)
	require.False(t, targetsByID()[CodexAuth].CaptureAll)
	require.True(t, targetsByID()[ShellEnv].RemoteAuthoritative)
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
	definition := targetsByID()[Mise]
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
	definition := targetsByID()[CodexRules]
	_, err := definition.ReadLocal(home, "linux")
	require.Error(t, err)
	require.Contains(t, err.Error(), "target.legacy")
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
	require.Error(t, validateTargetData(File{Format: FormatJSON}, []byte("{broken")))
	require.Error(t, validateTargetData(File{Format: FormatYAML}, []byte("key: [unterminated\n")))
}
