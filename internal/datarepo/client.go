package datarepo

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
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

const (
	configurationManifestPath = "configs/manifest.toml"
	maxConfigurationFiles     = 256
	maxConfigurationDepth     = 8
)

// Metadata 标识一次一致的远端快照。
type Metadata struct {
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Branch       string `json:"branch"`
	Commit       string `json:"commit"`
}

// Snapshot 只包含本轮纳管的格式标记和机器记录。
type Snapshot struct {
	Metadata          Metadata
	Files             map[string][]byte
	Initialized       bool
	ConfigInitialized bool
	HasMachines       bool
}

// Changes 描述一次配置或机器资料提交中的新增、更新和删除。
type Changes struct {
	Message   string
	Files     map[string][]byte
	Deletions []string
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
		case "configs":
			if entry.Type != "tree" || entry.Mode != "040000" {
				return snapshot, failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			if err := c.readConfiguration(ctx, &snapshot, entry.SHA); err != nil {
				return snapshot, err
			}
		}
	}
	return snapshot, nil
}

// Commit 使用一次 CAS mutation 提交；不确定结果保留未知副作用状态。
func (c *Client) Commit(ctx context.Context, snapshot Snapshot, files map[string][]byte) (result Metadata, err error) {
	return c.CommitChanges(ctx, snapshot, Changes{
		Message: "my: update managed inventory",
		Files:   files,
	})
}

// CommitChanges 使用一次 CAS mutation 同时提交新增、更新和删除。
func (c *Client) CommitChanges(ctx context.Context, snapshot Snapshot, changes Changes) (result Metadata, err error) {
	result = snapshot.Metadata
	ctx, finish := c.observe(ctx, phaseCommit, snapshot.Metadata.Commit)
	defer func() { finish(err, result) }()
	paths := make([]string, 0, len(changes.Files))
	for configPath := range changes.Files {
		if !validCommitPath(configPath) {
			return result, failure(codeFormat, phaseCommit, nil, []string{}, recoverFormat)
		}
		paths = append(paths, configPath)
	}
	seenDeletion := make(map[string]struct{}, len(changes.Deletions))
	deletions := make([]string, 0, len(changes.Deletions))
	for _, configPath := range changes.Deletions {
		if !validCommitPath(configPath) || configPath == "my.toml" || strings.HasPrefix(configPath, "machines/") {
			return result, failure(codeFormat, phaseCommit, nil, []string{}, recoverFormat)
		}
		if _, duplicate := seenDeletion[configPath]; duplicate {
			return result, failure(codeFormat, phaseCommit, nil, []string{}, recoverFormat)
		}
		if _, added := changes.Files[configPath]; added {
			return result, failure(codeFormat, phaseCommit, nil, []string{}, recoverFormat)
		}
		seenDeletion[configPath] = struct{}{}
		deletions = append(deletions, configPath)
	}
	if len(paths) == 0 && len(deletions) == 0 || snapshot.Metadata.Commit == "" {
		return result, failure(codeFormat, phaseCommit, nil, []string{}, recoverFormat)
	}
	sort.Strings(paths)
	sort.Strings(deletions)
	additions := make([]map[string]string, 0, len(paths))
	for _, configPath := range paths {
		additions = append(
			additions,
			map[string]string{
				"path":     configPath,
				"contents": base64.StdEncoding.EncodeToString(changes.Files[configPath]),
			},
		)
	}
	deleted := make([]map[string]string, 0, len(deletions))
	for _, configPath := range deletions {
		deleted = append(deleted, map[string]string{"path": configPath})
	}
	message := changes.Message
	if message == "" {
		message = "my: update managed inventory"
	}
	input := map[string]any{
		"branch": map[string]string{
			"repositoryNameWithOwner": c.config.Repository,
			"branchName":              c.config.Branch,
		},
		"expectedHeadOid": snapshot.Metadata.Commit,
		"message":         map[string]string{"headline": message},
		"fileChanges":     map[string]any{"additions": additions, "deletions": deleted},
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

func (c *Client) readConfiguration(ctx context.Context, snapshot *Snapshot, sha string) error {
	entries, err := c.tree(ctx, sha)
	if err != nil {
		return err
	}
	var manifestSHA, targetsSHA string
	for _, entry := range entries {
		switch entry.Path {
		case "manifest.toml":
			if entry.Type != "blob" || entry.Mode != "100644" {
				return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			manifestSHA = entry.SHA
		case "targets":
			if entry.Type != "tree" || entry.Mode != "040000" {
				return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			targetsSHA = entry.SHA
		default:
			return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
		}
	}
	if manifestSHA == "" || targetsSHA == "" {
		return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
	}
	manifest, err := c.blob(ctx, manifestSHA)
	if err != nil {
		return err
	}
	var header struct {
		SchemaVersion int `toml:"schema_version"`
	}
	decoder := toml.NewDecoder(bytes.NewReader(manifest))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil || header.SchemaVersion != 1 {
		return failure(codeFormat, phaseRead, err, []string{}, recoverFormat)
	}
	snapshot.ConfigInitialized = true
	snapshot.Files[configurationManifestPath] = manifest
	targets, err := c.tree(ctx, targetsSHA)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.Type != "tree" || target.Mode != "040000" || !validTargetID(target.Path) {
			return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
		}
		if err := c.readConfigurationTarget(ctx, snapshot, target.Path, target.SHA); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) readConfigurationTarget(ctx context.Context, snapshot *Snapshot, targetID, sha string) error {
	entries, err := c.tree(ctx, sha)
	if err != nil {
		return err
	}
	var targetSHA, dataSHA string
	for _, entry := range entries {
		switch entry.Path {
		case "target.toml":
			if entry.Type != "blob" || entry.Mode != "100644" {
				return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			targetSHA = entry.SHA
		case "data":
			if entry.Type != "tree" || entry.Mode != "040000" {
				return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			dataSHA = entry.SHA
		default:
			return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
		}
	}
	if targetSHA == "" {
		return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
	}
	target, err := c.blob(ctx, targetSHA)
	if err != nil {
		return err
	}
	var metadata struct {
		SchemaVersion int    `toml:"schema_version"`
		Target        string `toml:"target"`
	}
	if err := toml.Unmarshal(target, &metadata); err != nil ||
		metadata.SchemaVersion != 1 ||
		metadata.Target != targetID {
		return failure(codeFormat, phaseRead, err, []string{}, recoverFormat)
	}
	targetPath := path.Join("configs", "targets", targetID, "target.toml")
	snapshot.Files[targetPath] = target
	if dataSHA == "" {
		return nil
	}
	return c.readConfigurationData(ctx, snapshot, path.Join("configs", "targets", targetID, "data"), dataSHA, 0)
}

func (c *Client) readConfigurationData(
	ctx context.Context,
	snapshot *Snapshot,
	parent string,
	sha string,
	depth int,
) error {
	if depth >= maxConfigurationDepth {
		return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
	}
	entries, err := c.tree(ctx, sha)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !safePathComponent(entry.Path) {
			return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
		}
		switch {
		case entry.Type == "blob" && entry.Mode == "100644":
			if configurationFileCount(snapshot.Files) >= maxConfigurationFiles {
				return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
			}
			data, err := c.blob(ctx, entry.SHA)
			if err != nil {
				return err
			}
			snapshot.Files[path.Join(parent, entry.Path)] = data
		case entry.Type == "tree" && entry.Mode == "040000":
			if err := c.readConfigurationData(
				ctx,
				snapshot,
				path.Join(parent, entry.Path),
				entry.SHA,
				depth+1,
			); err != nil {
				return err
			}
		default:
			return failure(codeFormat, phaseRead, nil, []string{}, recoverFormat)
		}
	}
	return nil
}

func configurationFileCount(files map[string][]byte) int {
	count := 0
	for name := range files {
		if strings.HasPrefix(name, "configs/") {
			count++
		}
	}
	return count
}

func validCommitPath(value string) bool {
	if value == "my.toml" {
		return true
	}
	if strings.HasPrefix(value, "machines/") {
		remainder := strings.TrimPrefix(value, "machines/")
		return safePathComponent(strings.TrimSuffix(remainder, ".toml")) &&
			strings.HasSuffix(remainder, ".toml")
	}
	if value == configurationManifestPath {
		return true
	}
	parts := strings.Split(value, "/")
	if len(parts) == 4 &&
		parts[0] == "configs" &&
		parts[1] == "targets" &&
		validTargetID(parts[2]) &&
		parts[3] == "target.toml" {
		return true
	}
	if len(parts) < 5 ||
		parts[0] != "configs" ||
		parts[1] != "targets" ||
		!validTargetID(parts[2]) ||
		parts[3] != "data" {
		return false
	}
	for _, component := range parts[4:] {
		if !safePathComponent(component) {
			return false
		}
	}
	return true
}

func validTargetID(value string) bool {
	if value == "" || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func safePathComponent(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if r == '/' || r == '\\' || r == 0 || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
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
