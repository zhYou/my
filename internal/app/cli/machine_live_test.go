package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/command"
	"github.com/zhYou/my/internal/datarepo"
)

// TestGitHubMachineAcceptance 仅在显式指定正式私有仓与专用 test 分支时运行。
// 合成记录不会写入默认分支；认证由外部 gh 提供，失败时保留验收分支供核查。
func TestGitHubMachineAcceptance(t *testing.T) {
	repo := os.Getenv("MY_ACCEPTANCE_REPOSITORY")
	branch := os.Getenv("MY_ACCEPTANCE_BRANCH")
	if repo == "" && branch == "" {
		t.Skip("真实 GitHub 验收须显式选择私有仓和 test/ 分支")
	}
	require.Equal(t, "zhYou/my-data", repo)
	require.True(t, strings.HasPrefix(branch, "test/"), "只允许专用验收分支")
	suffix := time.Now().UTC().Format("150405.000000")
	suffix = strings.ReplaceAll(suffix, ".", "")
	primary := "accept-primary-" + suffix
	home := liveHome(t, repo, branch)
	run := func(home string, deps *Dependencies, want int, args ...string) liveOutput {
		t.Helper()
		var out, stderr bytes.Buffer
		args = append(args, "--json")
		code := Run(
			t.Context(),
			args,
			Options{
				Name:   "my",
				Out:    &out,
				ErrOut: &stderr,
				LookupEnv: func(k string) (string, bool) {
					return home, k == "MY_HOME"
				},
				Dependencies: deps,
			},
		)
		require.Equal(t, want, code, "args=%v stderr=%s", args, stderr.String())
		require.NotContains(t, stderr.String(), "private-note-marker")
		require.NotContains(t, stderr.String(), "private-local-marker")
		require.NotContains(t, stderr.String(), "private-response-marker")
		var value liveOutput
		if want == 0 {
			require.Empty(t, stderr.String())
			require.NoError(t, json.Unmarshal(out.Bytes(), &value))
		} else {
			require.Empty(t, out.String())
			require.NoError(t, json.Unmarshal(stderr.Bytes(), &value))
		}
		return value
	}
	first := run(home, nil, 0, "machine", "register", primary, "--kind", "vm", "--note", "private-note-marker")
	snapshot := first.Snapshot
	base := snapshot.Commit
	again := run(home, nil, 0, "machine", "register", primary, "--kind", "vm", "--note", "private-note-marker")
	require.Equal(t, base, again.Snapshot.Commit)
	run(home, nil, 0, "machine", "current")
	run(home, nil, 0, "machine", "list")
	run(home, nil, 0, "machine", "show", primary)
	run(home, nil, 0, "doctor")
	run(home, nil, 0, "doctor", "--remote")
	fresh := liveHome(t, repo, branch)
	run(fresh, nil, 0, "machine", "bind", primary)
	retired := run(fresh, nil, 0, "machine", "retire", primary)
	require.Equal(t, "retired", retired.Machine.Status)
	run(fresh, nil, 1, "machine", "bind", primary)
	cfg := datarepo.Config{Repository: repo, Branch: branch, RequestTimeout: 30 * time.Second}
	client, err := datarepo.New(
		t.Context(),
		cfg,
		datarepo.Options{Token: (datarepo.CredentialSource{Execution: command.Execution{}}).Read},
	)
	require.NoError(t, err)
	remote, err := client.Read(t.Context())
	require.NoError(t, err)
	records, err := machine.Records(remote)
	require.NoError(t, err)
	record := records[primary]
	record.OS = "windows"
	if runtime.GOOS == "windows" {
		record.OS = "linux"
	}
	encoded, err := toml.Marshal(record)
	require.NoError(t, err)
	_, err = client.Commit(t.Context(), remote, map[string][]byte{"machines/" + primary + ".toml": encoded})
	require.NoError(t, err)
	run(fresh, nil, 1, "machine", "bind", primary, "--reactivate")
	restored := run(fresh, nil, 0, "machine", "bind", primary, "--reactivate", "--update-platform")
	require.Equal(
		t,
		first.Machine.RegisteredAt,
		restored.Machine.RegisteredAt,
	)
	run(fresh, nil, 0, "doctor", "--remote")
	for _, unknown := range []bool{false, true} {
		isolated := liveHome(t, repo, branch)
		id := "accept-partial-" + suffix
		deps := &Dependencies{}
		if unknown {
			id = "accept-unknown-" + suffix
			deps.DataTransport = &lostLiveResponse{next: http.DefaultTransport}
		} else {
			deps.State = func(path string) machine.State {
				return workflowFailedState{machine.NewFileState(path)}
			}
		}
		failed := run(isolated, deps, 1, "machine", "register", id, "--kind", "vm", "--note", "private-note-marker")
		effects := failed.Diagnostics[0].Effects
		if unknown {
			require.Nil(t, effects)
		} else {
			require.Equal(t, []string{"data.commit"}, effects)
		}
		verified := run(isolated, nil, 0, "machine", "show", id)
		recovered := run(isolated, nil, 0, "machine", "register", id, "--kind", "vm", "--note", "private-note-marker")
		require.Equal(
			t,
			verified.Snapshot.Commit,
			recovered.Snapshot.Commit,
		)
		run(isolated, nil, 0, "doctor", "--remote")
		verifyDiagnosticFiles(t, isolated)
	}
	verifyDiagnosticFiles(t, home)
	verifyDiagnosticFiles(t, fresh)
	t.Logf("真实私有仓验收通过 repository=%s branch=%s initial_machine_commit=%s", repo, branch, base)
}

type lostLiveResponse struct {
	next    http.RoundTripper
	dropped bool
}

func (r *lostLiveResponse) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := r.next.RoundTrip(req)
	if err == nil && req.URL.Path == "/graphql" && response.StatusCode == 200 && !r.dropped {
		r.dropped = true
		if closeErr := response.Body.Close(); closeErr != nil {
			return nil, closeErr
		}
		return nil, errors.New("private-response-marker")
	}
	return response, err
}

func liveHome(t *testing.T, repo, branch string) string {
	t.Helper()
	home := t.TempDir()
	content := fmt.Sprintf("[data]\nrepository=%q\nbranch=%q\n[telemetry]\nexporter='file'\n", repo, branch)
	// #nosec G703 -- 路径仅由 t.TempDir 与固定文件名组成。
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(content), 0o600))
	return home
}

func verifyDiagnosticFiles(t *testing.T, home string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		data, err := os.ReadFile(file) // #nosec G304 -- 测试私有目录的固定模式。
		require.NoError(t, err)
		require.NotContains(t, string(data), "private-note-marker")
		require.NotContains(t, string(data), "private-response-marker")
		require.NotContains(t, string(data), "private-local-marker")
		runID := filepath.Base(filepath.Dir(file))
		require.Contains(t, string(data), runID)
		trace, err := os.ReadFile(filepath.Join(filepath.Dir(file), "trace.jsonl")) // #nosec G304 -- 测试私有目录。
		require.NoError(t, err)
		require.Contains(t, string(trace), runID)
		require.NotContains(t, string(trace), "private-note-marker")
		require.NotContains(t, string(trace), "private-response-marker")
		require.NotContains(t, string(trace), "private-local-marker")
		t.Logf("观测关联通过 run_id=%s", runID)
	}
}

type liveOutput struct {
	Snapshot    datarepo.Metadata `json:"snapshot"`
	Machine     machine.Record    `json:"machine"`
	Diagnostics []struct {
		Effects []string `json:"completed_effects"`
	} `json:"diagnostics"`
}
