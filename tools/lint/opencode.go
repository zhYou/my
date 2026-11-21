package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

const openCodeConfigPath = ".opencode/opencode.json"

const (
	openCodeActionShell              = "shell"
	openCodeEffectAllow              = "allow"
	openCodeEffectAsk                = "ask"
	openCodeEffectDeny               = "deny"
	openCodeGraphifyActionPattern    = "graphify_*"
	openCodeGraphifyQueryGraphAction = "graphify_query_graph"
	openCodeGraphifyGetNodeAction    = "graphify_get_node"
	openCodeGraphifyGetNeighbors     = "graphify_get_neighbors"
	openCodeGraphifyGetCommunity     = "graphify_get_community"
	openCodeGraphifyGodNodesAction   = "graphify_god_nodes"
	openCodeGraphifyStatsAction      = "graphify_graph_stats"
	openCodeGraphifyShortestPath     = "graphify_shortest_path"
	openCodeMCPExecutionTimeoutMS    = 180000
	openCodeMCPStartupTimeoutMS      = 300000
	openCodeResourceAny              = "*"
	openCodeSchemaURL                = "https://opencode.ai/config.json"
	openCodeServerDirectory          = "."
	openCodeServerTypeLocal          = "local"
	openCodeWorktreeDirectory        = ".worktrees"
)

type openCodeConfig struct {
	Schema      string               `json:"$schema"`
	Worktree    openCodeWorktree     `json:"worktree"`
	MCP         openCodeMCP          `json:"mcp"`
	Permissions []openCodePermission `json:"permissions"`
}

type openCodeWorktree struct {
	Directory string `json:"directory"`
}

type openCodeMCP struct {
	Timeout openCodeMCPTimeout           `json:"timeout"`
	Servers map[string]openCodeMCPServer `json:"servers"`
}

type openCodeMCPTimeout struct {
	Startup   int `json:"startup"`
	Execution int `json:"execution"`
}

type openCodeMCPServer struct {
	Type      string          `json:"type"`
	Command   []string        `json:"command"`
	Directory string          `json:"cwd"`
	Disabled  bool            `json:"disabled"`
	Pool      bool            `json:"pool"`
	CodeMode  *bool           `json:"codemode"`
	Timeout   json.RawMessage `json:"timeout"`
}

type openCodePermission struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// checkOpenCodeConfig 校验项目级 OpenCode 适配层的固定边界，避免权限或 MCP 配置静默漂移。
func checkOpenCodeConfig(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf(resolveRepositoryError, err)
	}
	path := filepath.Join(root, filepath.FromSlash(openCodeConfigPath))
	if err := checkRegularFile(path); err != nil {
		if errors.Is(err, errNotRegularFile) {
			return fmt.Errorf(notRegularError, path)
		}
		return fmt.Errorf("检查 %s: %w", path, err)
	}
	config, err := loadOpenCodeConfig(path)
	if err != nil {
		return err
	}
	return validateOpenCodeConfig(path, config)
}

func loadOpenCodeConfig(path string) (openCodeConfig, error) {
	//nolint:gosec // G304：只读取调用者指定 checkout 的固定项目配置。
	data, err := os.ReadFile(path)
	if err != nil {
		return openCodeConfig{}, fmt.Errorf(readPathError, path, err)
	}
	var config openCodeConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return openCodeConfig{}, fmt.Errorf("%s 的 JSON 无效: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("存在多余 JSON 文档")
		}
		return openCodeConfig{}, fmt.Errorf("%s 存在多余内容: %w", path, err)
	}
	return config, nil
}

func validateOpenCodeConfig(path string, config openCodeConfig) error {
	if config.Schema != openCodeSchemaURL {
		return fmt.Errorf("%s 的 $schema 不一致", path)
	}
	if config.Worktree.Directory != openCodeWorktreeDirectory {
		return fmt.Errorf("%s 的 worktree.directory 必须为 .worktrees", path)
	}
	if config.MCP.Timeout.Startup != openCodeMCPStartupTimeoutMS ||
		config.MCP.Timeout.Execution != openCodeMCPExecutionTimeoutMS {
		return fmt.Errorf("%s 的 MCP 启动或执行超时不一致", path)
	}
	expectedServers := map[string][]string{
		"serena":   {"mise", "run", "--quiet", "mcp:serve:opencode"},
		"graphify": {"mise", "run", "--quiet", "graphify:serve"},
	}
	for name, command := range expectedServers {
		server, ok := config.MCP.Servers[name]
		if !ok {
			return fmt.Errorf("%s 缺少 MCP 服务 %s", path, name)
		}
		if err := validateOpenCodeServer(path, name, server, command); err != nil {
			return err
		}
	}
	var unknownServers []string
	for name := range config.MCP.Servers {
		if _, ok := expectedServers[name]; !ok {
			unknownServers = append(unknownServers, name)
		}
	}
	if len(unknownServers) != 0 {
		sort.Strings(unknownServers)
		return fmt.Errorf("%s 包含未登记的 MCP 服务 %v", path, unknownServers)
	}
	if !slices.Equal(config.Permissions, expectedOpenCodePermissions()) {
		return fmt.Errorf("%s 的 permissions 与受检边界不一致", path)
	}
	return nil
}

func validateOpenCodeServer(path, name string, server openCodeMCPServer, command []string) error {
	if server.Type != openCodeServerTypeLocal {
		return fmt.Errorf("%s 的 MCP 服务 %s 必须使用 local", path, name)
	}
	if !slices.Equal(server.Command, command) {
		return fmt.Errorf("%s 的 MCP 服务 %s 启动命令不一致", path, name)
	}
	if server.Directory != openCodeServerDirectory {
		return fmt.Errorf("%s 的 MCP 服务 %s 必须从 workspace 启动", path, name)
	}
	if server.Disabled || server.Pool || server.CodeMode != nil || len(server.Timeout) != 0 {
		return fmt.Errorf("%s 的 MCP 服务 %s 不得禁用、共享连接或覆盖全局超时和 Code Mode", path, name)
	}
	return nil
}

func expectedOpenCodePermissions() []openCodePermission {
	return []openCodePermission{
		{Action: openCodeActionShell, Resource: openCodeResourceAny, Effect: openCodeEffectAsk},
		{Action: openCodeActionShell, Resource: "mise run *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "mise exec -- *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "go test *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "go vet *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "go build *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "go install *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "go list *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "go version *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "go mod tidy -diff *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "golangci-lint fmt *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "golangci-lint run *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gopls *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "actionlint *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "govulncheck *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "goreleaser check *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "goreleaser release --snapshot *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "taplo *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh pr view *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh pr list *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh pr checks *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh pr diff *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh pr status *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh issue view *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh issue list *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh repo view *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh label list *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh run list *", Effect: openCodeEffectAllow},
		{Action: openCodeActionShell, Resource: "gh event listen *", Effect: openCodeEffectAllow},
		{Action: openCodeGraphifyActionPattern, Resource: openCodeResourceAny, Effect: openCodeEffectDeny},
		{Action: openCodeGraphifyQueryGraphAction, Resource: openCodeResourceAny, Effect: openCodeEffectAllow},
		{Action: openCodeGraphifyGetNodeAction, Resource: openCodeResourceAny, Effect: openCodeEffectAllow},
		{Action: openCodeGraphifyGetNeighbors, Resource: openCodeResourceAny, Effect: openCodeEffectAllow},
		{Action: openCodeGraphifyGetCommunity, Resource: openCodeResourceAny, Effect: openCodeEffectAllow},
		{Action: openCodeGraphifyGodNodesAction, Resource: openCodeResourceAny, Effect: openCodeEffectAllow},
		{Action: openCodeGraphifyStatsAction, Resource: openCodeResourceAny, Effect: openCodeEffectAllow},
		{Action: openCodeGraphifyShortestPath, Resource: openCodeResourceAny, Effect: openCodeEffectAllow},
	}
}
