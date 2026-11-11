package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/app/shellenv"
	"github.com/zhYou/my/internal/datarepo"

	"github.com/stretchr/testify/require"
)

func TestTargetCommandsAreDiscoverable(t *testing.T) {
	code, out, stderr := invoke(t, "schema", "target", "capture")
	require.Zero(t, code, stderr)
	require.Contains(t, out, `"all"`)
	require.Contains(t, out, `"machine"`)

	code, out, stderr = invoke(t, "schema", "target", "resolve")
	require.Zero(t, code, stderr)
	require.Contains(t, out, `"unit"`)
	require.Contains(t, out, `"from"`)
}

func TestActivateRejectsJSONWithoutReadingCache(t *testing.T) {
	code, out, stderr := invoke(t, "activate", "bash", "--json")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	require.Contains(t, stderr, "cli.usage")
}

func TestTargetAndEnvironmentValidationPrecedesConfiguration(t *testing.T) {
	for _, test := range []struct {
		name  string
		args  []string
		input string
		code  string
	}{
		{"resolve-source", []string{"target", "resolve", "codex-settings", "--unit", "field:features.network_proxy", "--from", "invalid", "--json"}, "", "target.validate"},
		{"resolve-unit", []string{"target", "resolve", "codex-settings", "--unit", "file:../bad", "--from", "remote", "--json"}, "", "target.validate"},
		{"unknown-target", []string{"target", "diff", "unknown-target", "--json"}, "", "target.validate"},
		{"machine-syntax", []string{"target", "capture", "mise", "--machine", "INVALID", "--json"}, "", "target.validate"},
		{"dangerous-variable", []string{"env", "set", "PATH", "--json"}, "synthetic-value", "shellenv.validate"},
		{"invalid-variable-value", []string{"env", "set", "EDITOR", "--json"}, "line\nfeed", "shellenv.validate"},
		{"remove-variable", []string{"env", "remove", "bad-name", "--json"}, "", "shellenv.validate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			var out, stderr bytes.Buffer
			opts := isolatedOptions(home, &out, &stderr)
			loaded := false
			opts.In = strings.NewReader(test.input)
			opts.Dependencies.LoadConfig = func(context.Context, config.Options) (config.Config, error) {
				loaded = true
				return config.Config{}, errors.New("configuration should not be read")
			}
			code := Run(t.Context(), test.args, opts)
			require.Equal(t, 2, code, stderr.String())
			require.False(t, loaded)
			require.Empty(t, out.String())
			require.Contains(t, stderr.String(), `"code":"cli.usage"`)
			require.Contains(t, stderr.String(), test.code)
			require.NotContains(t, stderr.String(), "\"run_id\"")
			entries, err := os.ReadDir(home)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestOldConfigCommandIsRemoved(t *testing.T) {
	code, out, stderr := invoke(t, "config", "diff", "--json")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	require.Contains(t, stderr, "cli.usage")
}

func TestEnvironmentRepositorySnapshotIsPinned(t *testing.T) {
	f := newFixtureGitHub(t)
	home := workflowHome(t)
	code, _, stderr := workflowCall(t, f, home, nil, "data", "init", "--json")
	require.Zero(t, code, stderr)
	code, _, stderr = workflowCall(
		t,
		f,
		home,
		nil,
		"machine",
		"register",
		"fixture",
		"--kind",
		"vm",
		"--note",
		"Synthetic fixture",
		"--json",
	)
	require.Zero(t, code, stderr)
	before := f.headReads
	code, _, stderr = workflowCall(t, f, home, nil, "env", "list", "--json")
	require.Zero(t, code, stderr)
	require.Equal(t, 1, f.headReads-before)
}

func TestBusinessCommandsRejectOtherRepositoryIdentity(t *testing.T) {
	f := newFixtureGitHub(t)
	home := workflowHome(t)
	code, _, stderr := workflowCall(t, f, home, nil, "data", "init", "--json")
	require.Zero(t, code, stderr)
	code, _, stderr = workflowCall(
		t,
		f,
		home,
		nil,
		"machine",
		"register",
		"fixture",
		"--kind",
		"vm",
		"--note",
		"Synthetic fixture",
		"--json",
	)
	require.Zero(t, code, stderr)
	statePath := filepath.Join(home, "state", "machine.json")
	data, err := os.ReadFile(statePath) // #nosec G304 -- 测试临时绑定文件。
	require.NoError(t, err)
	var binding machine.Binding
	require.NoError(t, json.Unmarshal(data, &binding))
	binding.RepositoryID = 999
	data, err = json.Marshal(binding)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(statePath, data, 0o600))
	code, out, stderr := workflowCall(t, f, home, nil, "env", "list", "--json")
	require.Equal(t, 1, code, stderr)
	require.Empty(t, out)
	require.Contains(t, stderr, "machine.binding_conflict")
}

func TestOrdinaryActivateIgnoresUnusedIdentityAndUsesExplicitOverride(t *testing.T) {
	f := newFixtureGitHub(t)
	home := workflowHome(t)
	code, _, stderr := workflowCall(t, f, home, nil, "data", "init", "--json")
	require.Zero(t, code, stderr)
	code, _, stderr = workflowCall(
		t,
		f,
		home,
		nil,
		"machine",
		"register",
		"fixture",
		"--kind",
		"vm",
		"--note",
		"Synthetic fixture",
		"--json",
	)
	require.Zero(t, code, stderr)
	cache := shellenv.NewFileCache(filepath.Join(home, "state", shellenvStateDirectory), "fixture")
	require.NoError(t, cache.Write(shellenv.CacheState{
		MachineID: "fixture",
		Snapshot:  datarepo.Metadata{Repository: "owner/data", RepositoryID: 42, Branch: "main", Commit: f.head},
		Ordinary:  []byte("schema_version=1\n[env.EDITOR]\nvalue=\"nvim\"\nmachines=[\"*\"]\n"),
	}))
	for _, test := range []struct {
		name     string
		identity string
		flags    []string
	}{
		{"missing-file", filepath.Join(home, "missing-identity"), nil},
		{"flag-override", "relative-invalid", []string{"--age-identity", filepath.Join(home, "unused-override")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := "[data]\nrepository=\"owner/data\"\n[secrets]\nidentity_file=" + strconv.Quote(
				test.identity,
			) + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(content), 0o600))
			logsBefore, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
			require.NoError(t, err)
			readsBefore := f.reads
			args := append([]string{"activate", "bash"}, test.flags...)
			code, out, stderr := workflowCall(t, f, home, nil, args...)
			require.Zero(t, code, stderr)
			require.Equal(t, "export EDITOR='nvim'\n", out)
			require.Empty(t, stderr)
			require.Equal(t, readsBefore, f.reads)
			logsAfter, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
			require.NoError(t, err)
			require.Equal(t, logsBefore, logsAfter)
		})
	}
}

func TestTargetAndEnvironmentCommandsCompleteHTTPWorkflow(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			f := newFixtureGitHub(t)
			home := workflowHome(t)
			for _, args := range [][]string{{"data", "init"}, {"machine", "register", "fixture", "--kind", "vm", "--note", "fixture"}} {
				code, _, stderr := workflowCall(t, f, home, nil, append(args, "--json")...)
				require.Zero(t, code, stderr)
			}
			path := filepath.Join(home, ".config", "topgrade.toml")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte("verbose = true\n"), 0o600))
			call := func(args ...string) string {
				code, out, stderr := workflowCall(t, f, home, nil, append(args, "--json", "--lang", lang)...)
				require.Zero(t, code, stderr)
				require.True(t, json.Valid([]byte(out)))
				return out
			}
			call("target", "capture", "topgrade")
			call("target", "diff")
			call("target", "sync")
			f.replaceRemote("configs/targets/topgrade/data/topgrade.toml", []byte("verbose = false\n"))
			call("target", "sync")
			data, err := os.ReadFile(path) // #nosec G304 -- 测试临时文件。
			require.NoError(t, err)
			require.Equal(t, "verbose = false\n", string(data))
			require.NoError(t, os.WriteFile(path, []byte("verbose = 1\n"), 0o600))
			f.replaceRemote("configs/targets/topgrade/data/topgrade.toml", []byte("verbose = 2\n"))
			code, out, stderr := workflowCall(t, f, home, nil, "target", "sync", "--json", "--lang", lang)
			require.Equal(t, 1, code, stderr)
			require.Empty(t, out)
			require.NotContains(t, stderr, "Operation failed [")
			require.Contains(t, stderr, "target.conflicts")
			call("target", "resolve", "topgrade", "--unit", "file:topgrade.toml", "--from", "remote")
			call("target", "remove", "topgrade")
			code, _, stderr = workflowInput(
				t,
				f,
				home,
				nil,
				"synthetic-env-value",
				"env",
				"set",
				"EDITOR",
				"--json",
				"--lang",
				lang,
			)
			require.Zero(t, code, stderr)
			listed := call("env", "list")
			require.Contains(t, listed, "EDITOR")
			require.NotContains(t, listed, "synthetic-env-value")
			call("env", "remove", "EDITOR")
			verifyDiagnosticFiles(t, home)
		})
	}
}

func TestDiagnosticBuildAndDataCommitsHaveDistinctKeys(t *testing.T) {
	f := newFixtureGitHub(t)
	home := workflowHome(t)
	code, _, stderr := workflowCall(t, f, home, nil, "data", "init", "--json")
	require.Zero(t, code, stderr)
	paths, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	seen := false
	for _, path := range paths {
		data, err := os.ReadFile(path) // #nosec G304 -- 测试私有日志。
		require.NoError(t, err)
		for line := range strings.SplitSeq(string(data), "\n") {
			if line == "" {
				continue
			}
			decoder := json.NewDecoder(strings.NewReader(line))
			_, err := decoder.Token()
			require.NoError(t, err)
			keys := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				require.NoError(t, err)
				key, ok := token.(string)
				require.True(t, ok)
				require.False(t, keys[key], "重复 JSON 键：%s", key)
				keys[key] = true
				var value json.RawMessage
				require.NoError(t, decoder.Decode(&value))
			}
			if keys["data_commit"] {
				seen = true
				require.True(t, keys["commit"])
			}
		}
	}
	require.True(t, seen)
}

func TestBusinessWriteRechecksBindingWhileLocked(t *testing.T) {
	f := newFixtureGitHub(t)
	home := workflowHome(t)
	for _, args := range [][]string{{"data", "init"}, {"machine", "register", "fixture", "--kind", "vm", "--note", "fixture"}} {
		code, _, stderr := workflowCall(t, f, home, nil, append(args, "--json")...)
		require.Zero(t, code, stderr)
	}
	state := &changedBindingState{State: machine.NewFileState(filepath.Join(home, "state"))}
	commits := f.commits
	code, out, stderr := workflowInput(t, f, home, func(string) machine.State {
		return state
	}, "nvim", "env", "set", "EDITOR", "--json")
	require.Equal(t, 1, code, stderr)
	require.Empty(t, out)
	require.Contains(t, stderr, "machine.binding_conflict")
	require.Equal(t, commits, f.commits)
	require.Equal(t, 2, state.reads)
}

func TestSecretIdentityFailureDoesNotExposeSensitiveMarkers(t *testing.T) {
	for _, lang := range []string{"en", "zh-CN"} {
		t.Run(lang, func(t *testing.T) {
			f := newFixtureGitHub(t)
			home := workflowHome(t)
			for _, args := range [][]string{{"data", "init"}, {"machine", "register", "fixture", "--kind", "vm", "--note", "fixture"}} {
				code, _, stderr := workflowCall(t, f, home, nil, append(args, "--json")...)
				require.Zero(t, code, stderr)
			}
			identityPath := filepath.Join(home, "sensitive-identity-marker")
			for _, structured := range []bool{false, true} {
				args := []string{"env", "set", "API_KEY", "--secret", "--age-identity", identityPath, "--lang", lang}
				if structured {
					args = append(args, "--json")
				}
				code, out, stderr := workflowInput(t, f, home, nil, "sensitive-value-marker", args...)
				require.Equal(t, 1, code, stderr)
				require.Empty(t, out)
				require.NotContains(t, stderr, "sensitive-")
				require.NotContains(t, stderr, "Operation failed [")
			}
			files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "*.jsonl"))
			require.NoError(t, err)
			require.NotEmpty(t, files)
			for _, file := range files {
				data, err := os.ReadFile(file) // #nosec G304 -- 只读取临时 HOME 内的日志与 Trace。
				require.NoError(t, err)
				require.NotContains(t, string(data), "sensitive-")
				require.NotContains(t, string(data), "private-token-marker")
			}
		})
	}
}

func TestEnvironmentFlagsAreValidatedBeforeStdin(t *testing.T) {
	var out, stderr bytes.Buffer
	opts := isolatedOptions(t.TempDir(), &out, &stderr)
	opts.In = iotest.ErrReader(errors.New("stdin must not be read"))
	code := Run(t.Context(), []string{"env", "set", "EDITOR", "--log-level", "invalid", "--json"}, opts)
	require.Equal(t, 2, code, stderr.String())
	require.Empty(t, out.String())
	require.Contains(t, stderr.String(), "debug|info|warn|error")
}
