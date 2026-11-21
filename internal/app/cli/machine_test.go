package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/datarepo"
)

func TestMachineDiscoveryAndValidationPrecedeCredentials(t *testing.T) {
	code, out, stderr := invoke(t, "schema", "machine", "register")
	require.Zero(t, code, stderr)
	require.Contains(t, out, "kind")
	require.Contains(t, out, "note")
	_ = json.Valid([]byte(out))
	code, out, stderr = invoke(t, "schema", "machine", "update")
	require.Zero(t, code, stderr)
	require.Contains(t, out, "kind")
	require.Contains(t, out, "note")
	require.Contains(t, out, "changed")
	require.NotContains(t, out, "bound")
	_ = json.Valid([]byte(out))
	code, out, stderr = invoke(t, "--lang", "en", "machine", "update", "--help")
	require.Zero(t, code, stderr)
	require.Contains(t, out, "testcli machine update workstation --kind server --note")
}

func TestMachineUpdateValidationPrecedesConfiguration(t *testing.T) {
	for name, args := range map[string][]string{
		"no-fields":    {"machine", "update", "fixture"},
		"invalid-kind": {"machine", "update", "fixture", "--kind", "bogus"},
		"empty-note":   {"machine", "update", "fixture", "--note", ""},
	} {
		t.Run(name, func(t *testing.T) {
			code, out, stderr := invoke(t, append(args, "--json")...)
			require.Equal(t, 2, code)
			require.Empty(t, out)
			require.Contains(t, stderr, "machine.validate")
			require.Contains(t, stderr, "supported kind")
		})
	}
}

func TestMachineUpdateChangesProfileAndIsIdempotent(t *testing.T) {
	f := newFixtureGitHub(t)
	home := workflowHome(t)
	code, _, stderr := workflowCall(t, f, home, nil, "data", "init", "--json")
	require.Zero(t, code, stderr)
	code, out, stderr := workflowCall(
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
		"old",
		"--json",
	)
	require.Zero(t, code, stderr)
	var registered machine.ChangeResult
	require.NoError(t, json.Unmarshal([]byte(out), &registered))
	commits := f.commits
	code, out, stderr = workflowCall(t, f, home, nil, "machine", "update", "fixture", "--kind", "server", "--json")
	require.Zero(t, code, stderr)
	var updated machine.UpdateResult
	require.NoError(t, json.Unmarshal([]byte(out), &updated))
	require.True(t, updated.Changed)
	require.Equal(t, "server", updated.Machine.Kind)
	require.Equal(t, "old", updated.Machine.Note)
	require.Equal(t, registered.Machine.MachineID, updated.Machine.MachineID)
	require.Equal(t, registered.Machine.OS, updated.Machine.OS)
	require.Equal(t, registered.Machine.Arch, updated.Machine.Arch)
	require.Equal(t, registered.Machine.Status, updated.Machine.Status)
	require.Equal(t, registered.Machine.RegisteredAt, updated.Machine.RegisteredAt)
	require.NotContains(t, out, `"bound"`)
	require.Equal(t, commits+1, f.commits)
	code, out, stderr = workflowCall(
		t,
		f,
		home,
		nil,
		"machine",
		"update",
		"fixture",
		"--kind",
		"server",
		"--note",
		"old",
		"--json",
	)
	require.Zero(t, code, stderr)
	var again machine.UpdateResult
	require.NoError(t, json.Unmarshal([]byte(out), &again))
	require.False(t, again.Changed)
	require.Equal(t, updated.Snapshot, again.Snapshot)
	require.Equal(t, commits+1, f.commits)
	code, out, stderr = workflowCall(t, f, home, nil, "machine", "update", "fixture", "--note", "text note")
	require.Zero(t, code, stderr)
	require.Contains(t, out, "Note: text note")
	require.Contains(t, out, "Commit:")
	require.Contains(t, out, "Changed: true")
	require.Equal(t, commits+2, f.commits)
	code, out, stderr = workflowCall(t, f, home, nil, "machine", "update", "missing", "--note", "old", "--json")
	require.Equal(t, 1, code)
	require.Empty(t, out)
	require.Contains(t, stderr, "machine.not_found")
	require.Equal(t, commits+2, f.commits)
}

func TestMachineUpdateRetiredRecordStaysRetired(t *testing.T) {
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
		"old",
		"--json",
	)
	require.Zero(t, code, stderr)
	code, _, stderr = workflowCall(t, f, home, nil, "machine", "retire", "fixture", "--json")
	require.Zero(t, code, stderr)
	retired := f.commits
	code, out, stderr := workflowCall(t, f, home, nil, "machine", "update", "fixture", "--note", "corrected", "--json")
	require.Zero(t, code, stderr)
	var updated machine.UpdateResult
	require.NoError(t, json.Unmarshal([]byte(out), &updated))
	require.True(t, updated.Changed)
	require.Equal(t, machine.Retired, updated.Machine.Status)
	require.NotNil(t, updated.Machine.RetiredAt)
	require.Equal(t, "corrected", updated.Machine.Note)
	require.Equal(t, "vm", updated.Machine.Kind)
	require.Equal(t, retired+1, f.commits)
}

func TestInternalRequestTimeoutIsRuntimeButParentCancellationWins(t *testing.T) {
	failure := (fault.Definition{Code: "data.access", Kind: fault.Runtime}).Wrap(
		&datarepo.RequestTimeoutError{Cause: context.DeadlineExceeded},
	)
	require.Equal(t, 1, operationExitCode(context.Background(), failure))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Equal(t, 130, operationExitCode(ctx, failure))
}

func TestBusinessDoctorReportsBadConfigurationWithoutSideEffects(t *testing.T) {
	home := t.TempDir()
	var out, stderr bytes.Buffer
	options := isolatedOptions(home, &out, &stderr)
	options.Dependencies.ProbeGH = func(context.Context) error {
		return nil
	}
	options.Dependencies.Credential = func(context.Context) (string, error) {
		t.Fatal("bad configuration must not read credentials")
		return "", nil
	}
	code := Run(t.Context(), []string{"doctor", "--remote", "--json"}, options)
	require.Equal(t, 1, code)
	require.Empty(t, stderr.String())
	var report struct {
		Required bool  `json:"required_available"`
		Checks   []any `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	require.False(t, report.Required)
	require.Len(t, report.Checks, 9)
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestMachineWorkflowAndReadOnlyDoctorUseRealHTTP(t *testing.T) {
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
		"private-note-marker",
		"--json",
	)
	require.Zero(t, code, stderr)
	code, out, stderr := workflowCall(t, f, home, nil, "machine", "current", "--json")
	require.Zero(t, code, stderr)
	require.Contains(t, out, "fixture")
	reads, commits := f.reads, f.commits
	code, _, stderr = workflowCall(t, f, home, nil, "doctor", "--json")
	require.Zero(t, code, stderr)
	require.Equal(t, reads, f.reads)
	code, out, stderr = workflowCall(t, f, home, nil, "doctor", "--remote", "--json")
	require.Zero(t, code, stderr)
	require.Contains(t, out, `"required_available":true`)
	require.Equal(t, commits, f.commits)
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
		"private-note-marker",
		"--json",
	)
	require.Zero(t, code, stderr)
	require.Equal(t, commits, f.commits)
	fresh := workflowHome(t)
	code, _, stderr = workflowCall(t, f, fresh, nil, "machine", "bind", "fixture", "--json")
	require.Zero(t, code, stderr)
	code, _, stderr = workflowCall(t, f, fresh, nil, "machine", "retire", "fixture", "--json")
	require.Zero(t, code, stderr)
	code, out, stderr = workflowCall(t, f, fresh, nil, "machine", "bind", "fixture", "--json")
	require.Equal(t, 1, code)
	require.Empty(t, out)
	require.Contains(t, stderr, "machine.reactivate_required")
	code, _, stderr = workflowCall(t, f, fresh, nil, "machine", "bind", "fixture", "--reactivate", "--json")
	require.Zero(t, code, stderr)
	logs, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	for _, path := range logs {
		data, err := os.ReadFile(path) // #nosec G304 -- 只读取测试私有目录内的诊断文件。
		require.NoError(t, err)
		require.NotContains(t, string(data), "private-note-marker")
		require.NotContains(t, string(data), "private-token-marker")
		runID := filepath.Base(filepath.Dir(path))
		require.Contains(t, string(data), runID)
		trace, err := os.ReadFile(filepath.Join(filepath.Dir(path), "trace.jsonl"))
		require.NoError(t, err)
		require.Contains(t, string(trace), runID)
		require.NotContains(t, string(trace), "private-note-marker")
		require.NotContains(t, string(trace), "private-token-marker")
	}
}

type workflowFailedState struct{ machine.State }

func (s workflowFailedState) Write(machine.Binding) error {
	return errors.New("private-local-marker")
}

func TestCLIConfirmedAndUnknownRemoteEffectsRecoverSafely(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			f := newFixtureGitHub(t)
			home := workflowHome(t)
			code, _, stderr := workflowCall(t, f, home, nil, "data", "init", "--json")
			require.Zero(t, code, stderr)
			var factory func(string) machine.State
			if unknown {
				f.lost = true
			} else {
				factory = func(path string) machine.State {
					return workflowFailedState{machine.NewFileState(path)}
				}
			}
			code, out, stderr := workflowCall(
				t,
				f,
				home,
				factory,
				"machine",
				"register",
				"fixture",
				"--kind",
				"vm",
				"--note",
				"private-note-marker",
				"--json",
				"--lang",
				"zh-CN",
			)
			require.Equal(t, 1, code)
			require.Empty(t, out)
			require.NotContains(t, stderr, "private-")
			var diagnostic struct {
				Diagnostics []struct {
					Phase   string   `json:"phase"`
					Effects []string `json:"completed_effects"`
				} `json:"diagnostics"`
			}
			require.NoError(t, json.Unmarshal([]byte(stderr), &diagnostic))
			if unknown {
				require.Nil(t, diagnostic.Diagnostics[0].Effects)
			} else {
				require.Equal(t, []string{"data.commit"}, diagnostic.Diagnostics[0].Effects)
			}
			commits := f.commits
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
				"private-note-marker",
				"--json",
			)
			require.Zero(t, code, stderr)
			require.Equal(t, commits, f.commits)
			verifyDiagnosticFiles(t, home)
			if unknown {
				paths, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
				require.NoError(t, err)
				seen := false
				for _, path := range paths {
					data, err := os.ReadFile(path) // #nosec G304 -- 只读取测试私有目录内的诊断文件。
					require.NoError(t, err)
					for line := range strings.SplitSeq(string(data), "\n") {
						var event map[string]any
						if json.Unmarshal([]byte(line), &event) == nil && event["event"] == "data_phase_finished" &&
							event["outcome"] == "unknown" {
							seen = true
							require.Empty(t, event["data_commit"])
							require.NotEmpty(t, event["base_commit"])
						}
					}
				}
				require.True(t, seen)
			}
		})
	}
}
