package datarepo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
)

type routeTransport struct{ url string }

func (r routeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = strings.TrimPrefix(r.url, "http://")
	return http.DefaultTransport.RoundTrip(cloned)
}

func TestReadSnapshotPinsTreeAndCommitUsesExpectedHead(t *testing.T) {
	base := strings.Repeat("a", 40)
	next := strings.Repeat("b", 40)
	commits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "token fixture-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/data":
			//nolint:errcheck // 测试 HTTP 客户端可主动断开连接。
			_, _ = w.Write([]byte(`{"id":42,"private":true}`))
		case "/repos/owner/data/git/ref/heads/main":
			//nolint:errcheck // 测试 HTTP 客户端可主动断开连接。
			_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": base}})
		case "/repos/owner/data/git/trees/" + base:
			//nolint:errcheck // 测试 HTTP 客户端可主动断开连接。
			_, _ = w.Write(
				[]byte(
					`{"tree":[{"path":"my.toml","type":"blob","mode":"100644","sha":"manifest"}],"truncated":false}`,
				),
			)
		case "/repos/owner/data/git/blobs/manifest":
			//nolint:errcheck // 测试 HTTP 客户端可主动断开连接。
			_ = json.NewEncoder(w).
				Encode(map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("schema_version=1\n"))})
		case "/graphql":
			commits++
			var body struct {
				Variables struct {
					Input struct {
						ExpectedHead string            `json:"expectedHeadOid"`
						Branch       map[string]string `json:"branch"`
						Changes      struct {
							Additions []struct {
								Path     string `json:"path"`
								Contents string `json:"contents"`
							} `json:"additions"`
						} `json:"fileChanges"`
					} `json:"input"`
				} `json:"variables"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, base, body.Variables.Input.ExpectedHead)
			assert.Equal(
				t,
				map[string]string{"repositoryNameWithOwner": "owner/data", "branchName": "main"},
				body.Variables.Input.Branch,
			)
			assert.Equal(t, "machines/fixture.toml", body.Variables.Input.Changes.Additions[0].Path)
			decoded, err := base64.StdEncoding.DecodeString(body.Variables.Input.Changes.Additions[0].Contents)
			assert.NoError(t, err)
			assert.Equal(t, "fixture", string(decoded))
			//nolint:errcheck // 测试 HTTP 客户端可主动断开连接。
			_, _ = w.Write([]byte(`{"data":{"createCommitOnBranch":{"commit":{"oid":"` + next + `"}}}}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	snapshot, err := c.Read(t.Context())
	require.NoError(t, err)
	assert.True(t, snapshot.Initialized)
	assert.Equal(t, int64(42), snapshot.Metadata.RepositoryID)
	result, err := c.Commit(t.Context(), snapshot, map[string][]byte{"machines/fixture.toml": []byte("fixture")})
	require.NoError(t, err)
	assert.Equal(t, next, result.Commit)
	assert.Equal(t, 1, commits)
}

func TestReadAtIncludesStrictConfigurationTree(t *testing.T) {
	commit := strings.Repeat("a", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/data/git/trees/" + commit:
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]string{
					{"path": "my.toml", "type": "blob", "mode": "100644", "sha": "manifest"},
					{"path": "configs", "type": "tree", "mode": "040000", "sha": "configs"},
				},
				"truncated": false,
			}))
		case "/repos/owner/data/git/trees/configs":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]string{
					{"path": "manifest.toml", "type": "blob", "mode": "100644", "sha": "config-manifest"},
					{"path": "targets", "type": "tree", "mode": "040000", "sha": "targets"},
				},
				"truncated": false,
			}))
		case "/repos/owner/data/git/trees/targets":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]string{
					{"path": "mise", "type": "tree", "mode": "040000", "sha": "mise"},
				},
				"truncated": false,
			}))
		case "/repos/owner/data/git/trees/mise":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]string{
					{"path": "target.toml", "type": "blob", "mode": "100644", "sha": "target"},
					{"path": "data", "type": "tree", "mode": "040000", "sha": "mise-data"},
				},
				"truncated": false,
			}))
		case "/repos/owner/data/git/trees/mise-data":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]string{
					{"path": "values.toml", "type": "blob", "mode": "100644", "sha": "values"},
				},
				"truncated": false,
			}))
		case "/repos/owner/data/git/blobs/manifest":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"encoding": "base64",
				"content":  base64.StdEncoding.EncodeToString([]byte("schema_version=1\n")),
			}))
		case "/repos/owner/data/git/blobs/config-manifest":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"encoding": "base64",
				"content":  base64.StdEncoding.EncodeToString([]byte("schema_version=1\n")),
			}))
		case "/repos/owner/data/git/blobs/target":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"encoding": "base64",
				"content":  base64.StdEncoding.EncodeToString([]byte("schema_version=1\ntarget=\"mise\"\n")),
			}))
		case "/repos/owner/data/git/blobs/values":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"encoding": "base64",
				"content":  base64.StdEncoding.EncodeToString([]byte("[tools]\ngo=\"1.27.3\"\n")),
			}))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	snapshot, err := c.ReadAt(t.Context(), Metadata{
		Repository:   "owner/data",
		RepositoryID: 42,
		Branch:       "main",
		Commit:       commit,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("schema_version=1\n"), snapshot.Files["configs/manifest.toml"])
	assert.Equal(t, []byte("schema_version=1\ntarget=\"mise\"\n"), snapshot.Files["configs/targets/mise/target.toml"])
	assert.Equal(t, "[tools]\ngo=\"1.27.3\"\n", string(snapshot.Files["configs/targets/mise/data/values.toml"]))
}

func TestReadAtRejectsUnexpectedConfigurationEntry(t *testing.T) {
	commit := strings.Repeat("a", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/data/git/trees/" + commit:
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]string{
					{"path": "configs", "type": "tree", "mode": "040000", "sha": "configs"},
				},
				"truncated": false,
			}))
		case "/repos/owner/data/git/trees/configs":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]string{
					{"path": "unexpected.toml", "type": "blob", "mode": "100644", "sha": "unexpected"},
				},
				"truncated": false,
			}))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	_, err = c.ReadAt(t.Context(), Metadata{
		Repository:   "owner/data",
		RepositoryID: 42,
		Branch:       "main",
		Commit:       commit,
	})
	require.Error(t, err)
}

func TestCommitChangesSupportsConfigurationAdditionsAndDeletions(t *testing.T) {
	base := strings.Repeat("a", 40)
	next := strings.Repeat("b", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, "/graphql", r.URL.Path) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Variables struct {
				Input struct {
					ExpectedHead string `json:"expectedHeadOid"`
					Message      struct {
						Headline string `json:"headline"`
					} `json:"message"`
					FileChanges struct {
						Additions []struct {
							Path     string `json:"path"`
							Contents string `json:"contents"`
						} `json:"additions"`
						Deletions []struct {
							Path string `json:"path"`
						} `json:"deletions"`
					} `json:"fileChanges"`
				} `json:"input"`
			} `json:"variables"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, base, body.Variables.Input.ExpectedHead)
		assert.Equal(t, "my: capture configuration", body.Variables.Input.Message.Headline)
		if !assert.Len(t, body.Variables.Input.FileChanges.Additions, 1) ||
			!assert.Len(t, body.Variables.Input.FileChanges.Deletions, 1) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "configs/targets/mise/data/values.toml", body.Variables.Input.FileChanges.Additions[0].Path)
		assert.Equal(t, "configs/targets/old/target.toml", body.Variables.Input.FileChanges.Deletions[0].Path)
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"createCommitOnBranch": map[string]any{"commit": map[string]any{"oid": next}},
			},
		}))
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	result, err := c.CommitChanges(t.Context(), Snapshot{Metadata: Metadata{
		Repository:   "owner/data",
		RepositoryID: 42,
		Branch:       "main",
		Commit:       base,
	}}, Changes{
		Message:   "my: capture configuration",
		Files:     map[string][]byte{"configs/targets/mise/data/values.toml": []byte("[tools]\n")},
		Deletions: []string{"configs/targets/old/target.toml"},
	})
	require.NoError(t, err)
	require.Equal(t, next, result.Commit)
}

func TestCommitChangesRejectsUnsafeConfigurationPath(t *testing.T) {
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{Token: func(context.Context) (string, error) { return "fixture-token", nil }},
	)
	require.NoError(t, err)
	_, err = c.CommitChanges(t.Context(), Snapshot{Metadata: Metadata{Commit: strings.Repeat("a", 40)}}, Changes{
		Files: map[string][]byte{"configs/targets/mise/../escape": []byte("secret")},
	})
	require.Error(t, err)
	details := fault.Details(err)
	require.NotEmpty(t, details)
	require.Equal(t, fault.Code("data.format"), details[0].Code)
}

func TestAmbiguousCommitHasUnknownEffectsAndHidesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		//nolint:errcheck // 测试 HTTP 客户端可主动断开连接。
		_, _ = w.Write([]byte("private-response-marker"))
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	_, err = c.Commit(
		t.Context(),
		Snapshot{Metadata: Metadata{Repository: "owner/data", Branch: "main", Commit: strings.Repeat("a", 40)}},
		map[string][]byte{"my.toml": []byte("schema_version=1")},
	)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-response-marker")
	d, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Nil(t, d.CompletedEffects)
	require.Equal(t, "data.commit", d.Phase)
	require.Equal(t, "data.recover_verify", d.Recovery.Code)
}

func TestRequestBudgetPreservesDeadlineButMarksInternalTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: 20 * time.Millisecond},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	_, err = c.Read(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var timeout *RequestTimeoutError
	require.ErrorAs(t, err, &timeout)
}

func TestRejectedCommitAndConcurrentHeadHaveConfirmedNoEffects(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			base := strings.Repeat("a", 40)
			head := base
			if changed {
				head = strings.Repeat("b", 40)
			}
			attempts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/graphql":
					attempts++
					assert.NoError(
						t,
						json.NewEncoder(w).
							Encode(map[string]any{"errors": []any{map[string]any{"type": "UNPROCESSABLE", "message": "private-response-marker"}}}),
					)
				case "/repos/owner/data":
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": 42, "private": true}))
				default:
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": head}}))
				}
			}))
			defer srv.Close()
			c, err := New(
				t.Context(),
				Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
				Options{
					Token:     func(context.Context) (string, error) { return "fixture-token", nil },
					Transport: routeTransport{srv.URL},
				},
			)
			require.NoError(t, err)
			_, err = c.Commit(
				t.Context(),
				Snapshot{Metadata: Metadata{Repository: "owner/data", Branch: "main", Commit: base}},
				map[string][]byte{"my.toml": []byte("schema_version=1")},
			)
			d, ok := fault.DiagnosticOf(err)
			require.True(t, ok)
			require.Equal(t, []string{}, d.CompletedEffects)
			want := "data.commit_rejected"
			if changed {
				want = "data.commit_conflict"
			}
			require.Equal(t, fault.Code(want), fault.Details(err)[0].Code)
			require.NotContains(t, err.Error(), "private-response-marker")
			require.Equal(t, 1, attempts)
		})
	}
}

func TestPublicRepositoryIsRejectedBeforeReadingData(t *testing.T) {
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count++
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": 42, "private": false}))
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	_, err = c.Read(t.Context())
	require.Error(t, err)
	require.Equal(t, 1, count)
}

func TestHeadEscapesValidBranchCharacters(t *testing.T) {
	branch := "test/fixture#100%"
	expected := strings.Repeat("a", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/data/git/ref/heads/"+branch {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": expected}}))
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: branch, RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	metadata, err := c.Head(t.Context(), Metadata{Repository: "owner/data", Branch: branch})
	require.NoError(t, err)
	require.Equal(t, expected, metadata.Commit)
}

func TestUnclassifiedGraphQLErrorDoesNotClaimNoEffects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assert.NoError(
			t,
			json.NewEncoder(w).
				Encode(map[string]any{"errors": []any{map[string]any{"type": "INTERNAL", "path": []string{"createCommitOnBranch"}, "message": "private-response-marker"}}}),
		)
	}))
	defer srv.Close()
	c, err := New(
		t.Context(),
		Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
		Options{
			Token:     func(context.Context) (string, error) { return "fixture-token", nil },
			Transport: routeTransport{srv.URL},
		},
	)
	require.NoError(t, err)
	_, err = c.Commit(
		t.Context(),
		Snapshot{Metadata: Metadata{Commit: strings.Repeat("a", 40)}},
		map[string][]byte{"my.toml": []byte("schema_version=1")},
	)
	require.Error(t, err)
	d, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Nil(t, d.CompletedEffects)
	require.Equal(t, fault.Code("data.commit_unknown"), fault.Details(err)[0].Code)
}
