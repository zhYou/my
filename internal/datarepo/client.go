package datarepo

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/pelletier/go-toml/v2"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const responseLimit = 1 << 20

// Metadata 标识一次一致的远端快照。
type Metadata struct {
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Branch       string `json:"branch"`
	Commit       string `json:"commit"`
}

// Snapshot 只包含本轮纳管的格式标记和机器记录。
type Snapshot struct {
	Metadata    Metadata
	Files       map[string][]byte
	Initialized bool
	HasMachines bool
}

// Options 注入调用级认证、HTTP 和观测依赖。
type Options struct {
	Token     func(context.Context) (string, error)
	Transport http.RoundTripper
	Logger    *logging.Logger
	Tracer    trace.Tracer
	Localizer *messages.Localizer
	CacheDir  string
}

// Client 访问一个已显式选择的数据仓，不自动重试提交。
type Client struct {
	config  Config
	rest    *api.RESTClient
	graphql *api.GraphQLClient
	options Options
}

// New 在配置校验后获取本次调用的内存凭据并构造私有客户端。
func New(ctx context.Context, cfg Config, opts Options) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if opts.Token == nil {
		return nil, failure(codeAuthentication, phaseAuthenticate, nil, []string{}, recoverAuth)
	}
	probe, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
	token, err := opts.Token(probe)
	cancel()
	if err != nil || strings.TrimSpace(token) == "" {
		return nil, failure(
			codeAuthentication,
			phaseAuthenticate,
			requestError(ctx, err),
			[]string{},
			recoverAuth,
		)
	}
	if opts.Transport == nil {
		opts.Transport = http.DefaultTransport
	}
	if opts.Logger == nil {
		opts.Logger = logging.NewNop()
	}
	if opts.Tracer == nil {
		opts.Tracer = noop.NewTracerProvider().Tracer("my.data")
	}
	if opts.CacheDir == "" {
		opts.CacheDir = "."
	}
	clientOptions := api.ClientOptions{
		Host:          "github.com",
		APIHost:       "api.github.com",
		AuthToken:     token,
		Transport:     boundedTransport{opts.Transport},
		Timeout:       cfg.RequestTimeout,
		LogIgnoreEnv:  true,
		EnableCache:   false,
		CacheDir:      opts.CacheDir,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	rest, err := api.NewRESTClient(clientOptions)
	if err != nil {
		return nil, failure(codeAuthentication, phaseAuthenticate, err, []string{}, recoverAuth)
	}
	graphql, err := api.NewGraphQLClient(clientOptions)
	if err != nil {
		return nil, failure(codeAuthentication, phaseAuthenticate, err, []string{}, recoverAuth)
	}
	return &Client{config: cfg, rest: rest, graphql: graphql, options: opts}, nil
}

// Inspect 校验仓库私有性和分支，返回固定提交。
func (c *Client) Inspect(ctx context.Context) (Metadata, error) {
	metadata, err := c.RepositoryInfo(ctx)
	if err != nil {
		return metadata, err
	}
	return c.Head(ctx, metadata)
}

// RepositoryInfo 只读取并校验仓库元信息，供只读检查独立限时。
func (c *Client) RepositoryInfo(ctx context.Context) (Metadata, error) {
	result := Metadata{Repository: c.config.Repository, Branch: c.config.Branch}
	var repo struct {
		ID      int64 `json:"id"`
		Private bool  `json:"private"`
	}
	if err := c.rest.DoWithContext(ctx, http.MethodGet, "repos/"+c.config.Repository, nil, &repo); err != nil {
		return result, failure(
			codeAccess,
			"data.repository",
			requestError(ctx, err),
			[]string{},
			recoverAccess,
		)
	}
	if !repo.Private || repo.ID <= 0 {
		return result, failure("data.private_required", "data.repository", nil, []string{}, recoverConfig)
	}
	result.RepositoryID = repo.ID
	return result, nil
}

// Head 将仓库元信息绑定到当前分支提交。
func (c *Client) Head(ctx context.Context, result Metadata) (Metadata, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.rest.DoWithContext(
		ctx,
		http.MethodGet,
		"repos/"+c.config.Repository+"/git/ref/heads/"+url.PathEscape(c.config.Branch),
		nil,
		&ref,
	); err != nil {
		return result, failure(phaseBranch, phaseBranch, requestError(ctx, err), []string{}, recoverConfig)
	}
	if ref.Object.SHA == "" {
		return result, failure(phaseBranch, phaseBranch, nil, []string{}, recoverConfig)
	}
	result.Commit = ref.Object.SHA
	return result, nil
}

// Read 读取同一个提交的全部机器资料，不读取其他文件正文。
func (c *Client) Read(ctx context.Context) (snapshot Snapshot, err error) {
	ctx, finish := c.observe(ctx, phaseRead, "")
	defer func() { finish(err, snapshot.Metadata) }()
	metadata, err := c.Inspect(ctx)
	if err != nil {
		return Snapshot{Metadata: metadata}, err
	}
	return c.ReadAt(ctx, metadata)
}

// ReadAt 在已确认的提交上读取，避免检查期间 HEAD 变化混入快照。
func (c *Client) ReadAt(ctx context.Context, metadata Metadata) (Snapshot, error) {
	snapshot := Snapshot{Metadata: metadata, Files: map[string][]byte{}}
	entries, err := c.tree(ctx, metadata.Commit)
	if err != nil {
		return snapshot, err
	}
	for _, entry := range entries {
		switch entry.Path {
		case "my.toml":
			if entry.Type != "blob" || entry.Mode != "100644" {
				return snapshot, failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			data, err := c.blob(ctx, entry.SHA)
			if err != nil {
				return snapshot, err
			}
			var manifest struct {
				Version int `toml:"schema_version"`
			}
			decoder := toml.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&manifest); err != nil || manifest.Version != 1 {
				return snapshot, failure(codeFormat, phaseRead, err, []string{}, recoverFormat)
			}
			snapshot.Initialized = true
			snapshot.Files[entry.Path] = data
		case "machines":
			snapshot.HasMachines = true
			if entry.Type != "tree" || entry.Mode != "040000" {
				return snapshot, failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			machines, err := c.tree(ctx, entry.SHA)
			if err != nil {
				return snapshot, err
			}
			for _, machine := range machines {
				if machine.Type != "blob" || machine.Mode != "100644" || !strings.HasSuffix(machine.Path, ".toml") ||
					strings.ContainsAny(machine.Path, "/\\") {
					return snapshot, failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
				}
				data, err := c.blob(ctx, machine.SHA)
				if err != nil {
					return snapshot, err
				}
				snapshot.Files["machines/"+machine.Path] = data
			}
		}
	}
	return snapshot, nil
}

// Commit 使用一次 CAS mutation 提交；不确定结果保留未知副作用状态。
func (c *Client) Commit(ctx context.Context, snapshot Snapshot, files map[string][]byte) (result Metadata, err error) {
	result = snapshot.Metadata
	ctx, finish := c.observe(ctx, phaseCommit, snapshot.Metadata.Commit)
	defer func() { finish(err, result) }()
	paths := make([]string, 0, len(files))
	for path := range files {
		if path != "my.toml" &&
			(!strings.HasPrefix(path, "machines/") || !strings.HasSuffix(path, ".toml") || strings.Contains(path, "..") || strings.ContainsAny(strings.TrimPrefix(path, "machines/"), "/\\")) {
			return result, failure(codeFormat, phaseCommit, nil, []string{}, recoverFormat)
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 || snapshot.Metadata.Commit == "" {
		return result, failure(codeFormat, phaseCommit, nil, []string{}, recoverFormat)
	}
	sort.Strings(paths)
	additions := make([]map[string]string, 0, len(paths))
	for _, path := range paths {
		additions = append(
			additions,
			map[string]string{"path": path, "contents": base64.StdEncoding.EncodeToString(files[path])},
		)
	}
	input := map[string]any{
		"branch": map[string]string{
			"repositoryNameWithOwner": c.config.Repository,
			"branchName":              c.config.Branch,
		},
		"expectedHeadOid": snapshot.Metadata.Commit,
		"message":         map[string]string{"headline": "my: update managed inventory"},
		"fileChanges":     map[string]any{"additions": additions},
	}
	var response struct {
		Mutation struct {
			Commit struct {
				OID string `json:"oid"`
			} `json:"commit"`
		} `json:"createCommitOnBranch"`
	}
	err = c.graphql.DoWithContext(
		ctx,
		`mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }`,
		map[string]any{"input": input},
		&response,
	)
	if err != nil {
		effects := []string(nil)
		code := "data.commit_unknown"
		if confirmedRejection(err) {
			effects = []string{}
			code = "data.commit_rejected"
			current, probeErr := c.Inspect(ctx)
			if probeErr == nil && current.Commit != snapshot.Metadata.Commit {
				code = "data.commit_conflict"
			}
		}
		return result, failure(code, phaseCommit, requestError(ctx, err), effects, recoverVerify)
	}
	if response.Mutation.Commit.OID == "" {
		return result, failure("data.commit_unknown", phaseCommit, nil, nil, recoverVerify)
	}
	result.Commit = response.Mutation.Commit.OID
	return result, nil
}

type treeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
	SHA  string `json:"sha"`
}

func (c *Client) tree(ctx context.Context, sha string) ([]treeEntry, error) {
	var response struct {
		Tree      []treeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	err := c.rest.DoWithContext(ctx, http.MethodGet, "repos/"+c.config.Repository+"/git/trees/"+sha, nil, &response)
	if err != nil || response.Truncated {
		return nil, failure(codeAccess, phaseRead, requestError(ctx, err), []string{}, recoverAccess)
	}
	return response.Tree, nil
}

func (c *Client) blob(ctx context.Context, sha string) ([]byte, error) {
	var response struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	err := c.rest.DoWithContext(ctx, http.MethodGet, "repos/"+c.config.Repository+"/git/blobs/"+sha, nil, &response)
	if err != nil {
		return nil, failure(codeAccess, phaseRead, requestError(ctx, err), []string{}, recoverAccess)
	}
	if response.Encoding != "base64" {
		return nil, failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	if err != nil {
		return nil, failure(codeFormat, phaseRead, err, []string{}, recoverFormat)
	}
	return data, nil
}

type boundedTransport struct{ next http.RoundTripper }

func (t boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	closeErr := resp.Body.Close()
	if len(data) > responseLimit {
		return nil, errors.New("response exceeds size limit")
	}
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}

// confirmedRejection 仅将明确的请求拒绝视为无副作用，内部执行错误仍可能已提交。
func confirmedRejection(err error) bool {
	if graph, ok := errors.AsType[*api.GraphQLError](err); ok {
		if len(graph.Errors) == 0 {
			return false
		}
		for _, item := range graph.Errors {
			switch item.Type {
			case "UNPROCESSABLE", "FORBIDDEN", "NOT_FOUND", "UNAUTHORIZED", "GRAPHQL_VALIDATION_FAILED":
			default:
				return false
			}
		}
		return true
	}
	httpError, ok := errors.AsType[*api.HTTPError](err)
	return ok && httpError.StatusCode >= 400 && httpError.StatusCode < 500 &&
		httpError.StatusCode != http.StatusRequestTimeout
}
