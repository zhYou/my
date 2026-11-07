package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/machine"
)

type fixtureGitHub struct {
	mu       sync.Mutex
	server   *httptest.Server
	head     string
	versions map[string]map[string][]byte
	blobs    map[string][]byte
	commits  int
	reads    int
	lost     bool
}

func (f *fixtureGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	send := func(value any) {
		//nolint:errcheck // 测试客户端可主动取消请求。
		_ = json.NewEncoder(w).Encode(value)
	}
	if r.URL.Path == "/graphql" {
		var body struct {
			Variables struct {
				Input struct {
					Expected string `json:"expectedHeadOid"`
					Changes  struct {
						Additions []struct {
							Path     string `json:"path"`
							Contents string `json:"contents"`
						} `json:"additions"`
					} `json:"fileChanges"`
				} `json:"input"`
			} `json:"variables"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		input := body.Variables.Input
		if input.Expected != f.head {
			send(
				map[string]any{
					"errors": []any{map[string]any{"type": "UNPROCESSABLE", "message": "private-response-marker"}},
				},
			)
			return
		}
		files := map[string][]byte{}
		maps.Copy(files, f.versions[f.head])
		for _, addition := range input.Changes.Additions {
			data, err := base64.StdEncoding.DecodeString(addition.Contents)
			if err != nil {
				w.WriteHeader(400)
				return
			}
			files[addition.Path] = data
		}
		f.commits++
		f.head = fmt.Sprintf("%040x", f.commits+1)
		f.versions[f.head] = files
		if f.lost {
			f.lost = false
			w.WriteHeader(502)
			//nolint:errcheck // 断开客户端不影响本测试的远端事实。
			_, _ = w.Write([]byte("private-response-marker"))
			return
		}
		send(
			map[string]any{
				"data": map[string]any{"createCommitOnBranch": map[string]any{"commit": map[string]any{"oid": f.head}}},
			},
		)
		return
	}
	f.reads++
	prefix := "/repos/owner/data"
	switch {
	case r.URL.Path == prefix:
		send(map[string]any{"id": 42, "private": true})
	case r.URL.Path == prefix+"/git/ref/heads/main":
		send(map[string]any{"object": map[string]any{"sha": f.head}})
	case strings.HasPrefix(r.URL.Path, prefix+"/git/trees/"):
		key := strings.TrimPrefix(r.URL.Path, prefix+"/git/trees/")
		machines := strings.HasPrefix(key, "machines-")
		key = strings.TrimPrefix(key, "machines-")
		files, ok := f.versions[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		entries := []map[string]any{}
		hasMachines := false
		for path, data := range files {
			if strings.HasPrefix(path, "machines/") {
				hasMachines = true
			}
			if machines && !strings.HasPrefix(path, "machines/") || !machines && path != "my.toml" {
				continue
			}
			hash := sha256.Sum256(data)
			blob := hex.EncodeToString(hash[:])
			f.blobs[blob] = data
			entries = append(
				entries,
				map[string]any{
					"path": strings.TrimPrefix(path, "machines/"),
					"type": "blob",
					"mode": "100644",
					"sha":  blob,
				},
			)
		}
		if !machines && hasMachines {
			entries = append(
				entries,
				map[string]any{"path": "machines", "type": "tree", "mode": "040000", "sha": "machines-" + key},
			)
		}
		send(map[string]any{"tree": entries, "truncated": false})
	case strings.HasPrefix(r.URL.Path, prefix+"/git/blobs/"):
		blob := strings.TrimPrefix(r.URL.Path, prefix+"/git/blobs/")
		data, ok := f.blobs[blob]
		if !ok {
			w.WriteHeader(404)
			return
		}
		send(map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(data)})
	default:
		w.WriteHeader(404)
	}
}

type fixtureTransport struct{ endpoint string }

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(f.endpoint, "http://")
	return http.DefaultTransport.RoundTrip(clone)
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

func (s workflowFailedState) Write(machine.Binding) error { return errors.New("private-local-marker") }

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
				factory = func(path string) machine.State { return workflowFailedState{machine.NewFileState(path)} }
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
							require.Empty(t, event["commit"])
							require.NotEmpty(t, event["base_commit"])
						}
					}
				}
				require.True(t, seen)
			}
		})
	}
}

func TestBusinessSucceedsWhenDiagnosticDirectoryCannotBeCreated(t *testing.T) {
	f := newFixtureGitHub(t)
	home := workflowHome(t)
	require.NoError(t, os.Mkdir(filepath.Join(home, "state"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, "state", "logs"), []byte("private-diagnostic-marker"), 0o600))
	code, out, stderr := workflowCall(t, f, home, nil, "data", "init", "--json")
	require.Zero(t, code, stderr)
	require.Contains(t, out, `"changed":true`)
	require.Equal(t, 1, f.commits)
	require.NotContains(t, stderr, "private-")
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		var diagnostic map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &diagnostic))
		require.Equal(t, "warning", diagnostic["type"])
	}
}

func newFixtureGitHub(t *testing.T) *fixtureGitHub {
	t.Helper()
	f := &fixtureGitHub{
		head:     fmt.Sprintf("%040x", 1),
		versions: map[string]map[string][]byte{},
		blobs:    map[string][]byte{},
	}
	f.versions[f.head] = map[string][]byte{}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func workflowHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(home, "config.toml"),
			[]byte("[data]\nrepository='owner/data'\n[telemetry]\nexporter='file'\n"),
			0o600,
		),
	)
	return home
}

func workflowCall(
	t *testing.T,
	f *fixtureGitHub,
	home string,
	state func(string) machine.State,
	args ...string,
) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := Run(
		t.Context(),
		args,
		Options{
			Name:      "my",
			Out:       &out,
			ErrOut:    &stderr,
			LookupEnv: func(k string) (string, bool) { return home, k == "MY_HOME" },
			Dependencies: &Dependencies{
				Credential:    func(context.Context) (string, error) { return "private-token-marker", nil },
				DataTransport: fixtureTransport{f.server.URL},
				State:         state,
				ProbeGH:       func(context.Context) error { return nil },
			},
		},
	)
	return code, out.String(), stderr.String()
}
