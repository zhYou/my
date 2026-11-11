package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/doctor"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/locales"
)

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return invokeContext(context.Background(), t, args...)
}

func invokeContext(ctx context.Context, t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(ctx, args, Options{
		Name:         "testcli",
		Dependencies: &Dependencies{Doctor: emptyDoctorFixture},
		Version:      "v1.2.3",
		Commit:       "abc123",
		Date:         "2026-10-28",
		Out:          &out,
		ErrOut:       &errOut,
		ConfigDir:    t.TempDir(),
		Directories:  &paths.Directories{State: t.TempDir()},
		LookupEnv: func(string) (string, bool) {
			return "", false
		},
	})
	return code, out.String(), errOut.String()
}

// metadataRoot 仅用于无业务执行的命令元数据测试。业务测试统一通过 Run 托管资源。
func metadataRoot(opts Options) *cobra.Command {
	root, _, err := buildRoot(defaults(opts), locales.LoadBundle)
	if err != nil {
		panic(err)
	}
	return root
}

// isolatedOptions 显式注入配置、目录与环境；任何 argv 都不读取或写入用户状态。
func isolatedOptions(home string, out, stderr io.Writer) Options {
	dirs := paths.Directories{
		Config: home,
		Data:   filepath.Join(home, "data"),
		State:  filepath.Join(home, "state"),
		Cache:  filepath.Join(home, "cache"),
	}
	return Options{
		Directories: &dirs,
		Out:         out,
		ErrOut:      stderr,
		LookupEnv: func(key string) (string, bool) {
			if key == "MY_HOME" || key == "HOME" || key == "USERPROFILE" {
				return home, true
			}
			return "", false
		},
		Dependencies: &Dependencies{LoadConfig: func(context.Context, config.Options) (config.Config, error) {
			cfg := config.Defaults()
			cfg.Logging.Output = "none"
			return cfg, nil
		}},
	}
}

func catalogFixture(t *testing.T, language, id, replacement string) *messages.Bundle {
	t.Helper()
	files := fstest.MapFS{}
	for _, lang := range []string{"en", "zh-CN"} {
		name := "active." + lang + ".toml"
		data, err := fs.ReadFile(locales.Catalog(), name)
		require.NoError(t, err)
		if lang == language {
			content := string(data)
			header := "[\"" + id + "\"]\n"
			start := strings.Index(content, header)
			require.NotEqual(t, -1, start)
			end := strings.Index(content[start+len(header):], "\n[")
			require.NotEqual(t, -1, end)
			end += start + len(header) + 1
			section := ""
			if replacement != "" {
				section = header + replacement + "\n"
			}
			data = []byte(content[:start] + section + content[end:])
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	bundle, err := messages.NewBundle(files)
	require.NoError(t, err)
	return bundle
}

func catalogWithCommandFixtures(t *testing.T) *messages.Bundle {
	t.Helper()
	extra := map[string]string{
		"en": `
["fixture.alpha.short"]
other = "Alpha short"
["fixture.alpha.long"]
other = "Alpha long description"
["fixture.alpha.use"]
other = "alpha <item>"
["fixture.alpha.example"]
other = "Example alpha"
["fixture.alpha.flag"]
other = "Alpha name"
["fixture.beta.short"]
other = "Beta short"
["fixture.beta.long"]
other = "Beta long description"
["fixture.beta.use"]
other = "beta <item>"
["fixture.beta.example"]
other = "Example beta"
["fixture.beta.flag"]
other = "Beta name"
`,
		"zh-CN": `
["fixture.alpha.short"]
other = "甲命令"
["fixture.alpha.long"]
other = "甲命令长说明"
["fixture.alpha.use"]
other = "alpha <项目>"
["fixture.alpha.example"]
other = "甲命令示例"
["fixture.alpha.flag"]
other = "甲名称"
["fixture.beta.short"]
other = "乙命令"
["fixture.beta.long"]
other = "乙命令长说明"
["fixture.beta.use"]
other = "beta <项目>"
["fixture.beta.example"]
other = "乙命令示例"
["fixture.beta.flag"]
other = "乙名称"
`,
	}
	files := fstest.MapFS{}
	for lang, addition := range extra {
		name := "active." + lang + ".toml"
		data, err := fs.ReadFile(locales.Catalog(), name)
		require.NoError(t, err)
		files[name] = &fstest.MapFile{Data: append(data, []byte(addition)...)}
	}
	bundle, err := messages.NewBundle(files)
	require.NoError(t, err)
	return bundle
}

func noFixtureEnv(string) (string, bool) {
	return "", false
}

func emptyDoctorFixture(ctx context.Context, opts doctor.Options) (doctor.Result, error) {
	if err := opts.Validate(ctx); err != nil {
		return doctor.Result{}, err
	}
	return doctor.Result{RequiredAvailable: true, Checks: []doctor.Check{}}, nil
}

type fixtureGitHub struct {
	mu        sync.Mutex
	server    *httptest.Server
	head      string
	versions  map[string]map[string][]byte
	blobs     map[string][]byte
	commits   int
	reads     int
	headReads int
	lost      bool
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
						Deletions []struct {
							Path string `json:"path"`
						} `json:"deletions"`
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
		for _, deletion := range input.Changes.Deletions {
			delete(files, deletion.Path)
		}
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
		f.headReads++
		send(map[string]any{"object": map[string]any{"sha": f.head}})
	case strings.HasPrefix(r.URL.Path, prefix+"/git/trees/"):
		key := strings.TrimPrefix(r.URL.Path, prefix+"/git/trees/")
		directory := ""
		if strings.HasPrefix(key, "tree:") {
			parts := strings.SplitN(key, ":", 3)
			key, directory = parts[1], parts[2]
		}
		files, ok := f.versions[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		children := map[string]map[string]any{}
		for name, data := range files {
			relative := name
			if directory != "" {
				var found bool
				relative, found = strings.CutPrefix(name, directory+"/")
				if !found {
					continue
				}
			}
			child, _, nested := strings.Cut(relative, "/")
			if nested {
				full := child
				if directory != "" {
					full = directory + "/" + child
				}
				children[child] = map[string]any{
					"path": child,
					"type": "tree",
					"mode": "040000",
					"sha":  "tree:" + key + ":" + full,
				}
			} else {
				hash := sha256.Sum256(data)
				blob := hex.EncodeToString(hash[:])
				f.blobs[blob] = data
				children[child] = map[string]any{"path": child, "type": "blob", "mode": "100644", "sha": blob}
			}
		}
		entries := make([]map[string]any, 0, len(children))
		for _, entry := range children {
			entries = append(entries, entry)
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

func workflowInput(
	t *testing.T,
	f *fixtureGitHub,
	home string,
	state func(string) machine.State,
	input string,
	args ...string,
) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := Run(
		t.Context(),
		args,
		Options{
			Name:   "my",
			In:     strings.NewReader(input),
			Out:    &out,
			ErrOut: &stderr,
			LookupEnv: func(k string) (string, bool) {
				return home, k == "MY_HOME" || k == "HOME" || k == "USERPROFILE"
			},
			Dependencies: &Dependencies{
				Credential: func(context.Context) (string, error) {
					return "private-token-marker", nil
				},
				DataTransport: fixtureTransport{f.server.URL},
				State:         state,
				ProbeGH: func(context.Context) error {
					return nil
				},
			},
		},
	)
	return code, out.String(), stderr.String()
}

func workflowCall(
	t *testing.T,
	f *fixtureGitHub,
	home string,
	state func(string) machine.State,
	args ...string,
) (int, string, string) {
	t.Helper()
	return workflowInput(t, f, home, state, "", args...)
}

func (f *fixtureGitHub) replaceRemote(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	files := maps.Clone(f.versions[f.head])
	files[path] = append([]byte(nil), data...)
	f.head = fmt.Sprintf("%040x", 1000+len(f.versions))
	f.versions[f.head] = files
}

type changedBindingState struct {
	machine.State
	reads int
}

func (s *changedBindingState) Read() (machine.Binding, bool, error) {
	s.reads++
	binding, exists, err := s.State.Read()
	if s.reads > 1 {
		binding.MachineID = "another-machine"
	}
	return binding, exists, err
}
