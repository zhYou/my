package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func checkGraphifyMCP(ctx context.Context, root string) (resultErr error) {
	temporary, err := os.MkdirTemp("", "graphify-mcp-check-*")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(temporary)) }()
	base := filepath.Join(temporary, "fixture with spaces")
	if err := prepareMCPFixture(ctx, root, base); err != nil {
		return err
	}
	if err := prepareGraphifyFiles(root, base); err != nil {
		return err
	}
	// 在建图前连接，服务不得因尚无默认图而退出。
	client, closeClient, err := startMCP(ctx, root, "graphify:serve", "GRAPHIFY_QUERY_LOG_DISABLE=1")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeClient()) }()
	if err := client.initialize(
		"query_graph",
		graphifyNodeTool,
		"get_neighbors",
		"get_community",
		"god_nodes",
		graphifyStatsTool,
		"shortest_path",
	); err != nil {
		return err
	}
	if err := verifyGraphifyFailure(client, base); err != nil {
		return err
	}
	if _, err := runGraphify(ctx, root, graphifyExtract, base, codeOnlyFlag); err != nil {
		return err
	}
	if err := verifyGraphifyFixture(client, base, baseGoMarker); err != nil {
		return err
	}
	if err := verifyGraphifyMissingNode(ctx, client, root, base, "IgnoredGoMarker"); err != nil {
		return err
	}
	// 同一连接后创建新的嵌套 checkout，不能复用原 checkout 的图。
	linked := filepath.Join(base, ".worktrees", "new checkout")
	if err := fixtureGit(ctx, base, "worktree", gitAddCommand, quietFlag, "--detach", linked, "HEAD"); err != nil {
		return err
	}
	if err := writeMCPFiles(linked, linkedPrefix); err != nil {
		return err
	}
	if _, err := runGraphify(ctx, root, graphifyExtract, linked, codeOnlyFlag); err != nil {
		return err
	}
	if err := verifyGraphifyFixture(client, linked, linkedGoMarker); err != nil {
		return err
	}
	if err := verifyGraphifyMissingNode(ctx, client, root, linked, baseGoMarker); err != nil {
		return err
	}
	if err := verifyGraphifyMissingNode(ctx, client, root, base, linkedGoMarker); err != nil {
		return err
	}
	// 未提交的代码更新也必须可见；HEAD 比较本身不能证明索引新鲜。
	if err := writeMCPFiles(base, "Updated"); err != nil {
		return err
	}
	if _, err := runGraphify(ctx, root, "update", base); err != nil {
		return err
	}
	if err := verifyGraphifyFixture(client, base, "UpdatedGoMarker"); err != nil {
		return err
	}
	if err := verifyGraphifyMissingNode(ctx, client, root, base, baseGoMarker); err != nil {
		return err
	}
	if err := os.WriteFile(graphifyJSON(base), []byte("{broken"), 0o600); err != nil {
		return err
	}
	if err := verifyGraphifyFailure(client, base); err != nil {
		return err
	}
	if err := verifyGraphifyFixture(client, linked, linkedGoMarker); err != nil {
		return err
	}
	// 上游 --force 仍会读取旧图；只移除本验收刚写入的损坏夹具，再完整重建。
	if err := os.Remove(graphifyJSON(base)); err != nil {
		return err
	}
	if _, err := runGraphify(ctx, root, graphifyExtract, base, codeOnlyFlag, "--force"); err != nil {
		return err
	}
	if err := verifyGraphifyFixture(client, base, "UpdatedGoMarker"); err != nil {
		return err
	}
	fmt.Println("Graphify 已通过：本地代码图、CLI 退出码与排除规则、MCP 查询、未提交更新热加载、坏图恢复及同连接 checkout 隔离。")
	return nil
}

func verifyGraphifyFixture(client *mcpClient, project, marker string) error {
	for _, check := range []struct {
		tool string
		args map[string]any
		want []string
	}{
		{graphifyStatsTool, map[string]any{}, []string{"Nodes:", "Edges:"}},
		{graphifyNodeTool, map[string]any{labelField: marker}, []string{"Node: " + marker + "()", "sample.go"}},
		{"get_neighbors", map[string]any{labelField: marker}, []string{useGoMarker}},
		{"query_graph", map[string]any{"question": marker, "token_budget": 1000}, []string{marker, useGoMarker}},
		{"shortest_path", map[string]any{"source": useGoMarker, "target": marker}, []string{marker, useGoMarker}},
	} {
		check.args[projectPathField] = project
		text, err := client.tool(check.tool, check.args)
		if err != nil {
			return err
		}
		for _, want := range check.want {
			if !strings.Contains(text, want) {
				return fmt.Errorf("%s 未返回 %s：%s", check.tool, want, text)
			}
		}
	}
	return nil
}

func verifyGraphifyFailure(client *mcpClient, project string) error {
	text, err := client.tool(graphifyStatsTool, map[string]any{projectPathField: project})
	// 固定上游版本可能把载入失败作为普通文本返回，不能只检查 isError。
	if err == nil && !strings.HasPrefix(text, "Error executing graph_stats:") {
		return fmt.Errorf("无图或坏图未返回明确错误：%s", text)
	}
	if err != nil {
		// 协议或进程错误不能冒充可恢复的图载入错误。
		return err
	}
	return nil
}

func verifyGraphifyMissingNode(ctx context.Context, client *mcpClient, root, project, marker string) error {
	text, err := client.tool(graphifyNodeTool, map[string]any{projectPathField: project, labelField: marker})
	if err != nil {
		return err
	}
	if !strings.HasPrefix(text, "No node matching '") {
		return fmt.Errorf("MCP 混入应不存在的节点 %s：%s", marker, text)
	}
	_, err = runGraphify(ctx, root, "explain", marker, "--graph", graphifyJSON(project))
	if err == nil {
		return fmt.Errorf("CLI 错误地匹配到应不存在的节点 %s", marker)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 4 {
		return fmt.Errorf("CLI 未匹配节点应透传退出码 4，节点 %s：%w", marker, err)
	}
	return nil
}
