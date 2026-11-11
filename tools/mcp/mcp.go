package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhYou/my/tools/internal/scaffold"
)

// checkMCP 与 Codex 复用两个服务 task，真实调用不能由配置存在或 CLI 回退替代。
func checkMCP() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := checkSerenaMCP(ctx, root); err != nil {
		return fmt.Errorf("验收 Serena MCP：%w", err)
	}
	if err := checkGraphifyMCP(ctx, root); err != nil {
		return fmt.Errorf("验收 Graphify MCP：%w", err)
	}
	return nil
}

func checkSerenaMCP(ctx context.Context, root string) (resultErr error) {
	configPath := filepath.Join(root, serenaDirectory, serenaProjectFile)
	//nolint:gosec // G304：只读取当前 checkout 的固定 Serena 项目配置，检测意外迁移。
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	defer func() {
		//nolint:gosec // G304：同一固定路径的验收后只读快照。
		configAfter, readErr := os.ReadFile(configPath)
		if readErr != nil {
			resultErr = errors.Join(resultErr, readErr)
		} else if !bytes.Equal(configBefore, configAfter) {
			resultErr = errors.Join(resultErr, errors.New("验收时 Serena 改写了项目配置，请补齐固定版本要求的配置字段"))
		}
	}()
	temporary, err := os.MkdirTemp("", "serena-mcp-check-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(temporary))
	}()
	client, closeClient, err := startMCP(ctx, root, "mcp:serve", "SERENA_HOME="+filepath.Join(temporary, "serena-home"))
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeClient())
	}()
	if err := client.initialize(
		"initial_instructions", "activate_project", "get_current_config", "find_symbol", "get_symbols_overview",
		"find_referencing_symbols", "rename_symbol", "get_diagnostics_for_file", "search_for_pattern",
	); err != nil {
		return err
	}
	instructions, err := client.tool("initial_instructions", map[string]any{})
	if err != nil {
		return err
	}
	session, err := sessionFromInstructions(instructions)
	if err != nil {
		return err
	}
	project, err := scaffold.Inspect(root)
	if err != nil {
		return err
	}
	if err := client.overview(filepath.ToSlash(filepath.Join(project.CommandPath, "main.go")), "main"); err != nil {
		return err
	}
	base := filepath.Join(temporary, "fixture with spaces")
	if err := prepareMCPFixture(ctx, root, base); err != nil {
		return err
	}
	if err := client.activate(base, session); err != nil {
		return err
	}
	if err := verifyMCPFixture(client, baseMarkerPrefix); err != nil {
		return err
	}
	// 必须在 MCP 连接建立后创建；路径嵌套在已激活项目内，且含空格。
	linked := filepath.Join(base, ".worktrees", "new checkout")
	if err := fixtureGit(ctx, base, "worktree", gitAddCommand, quietFlag, "--detach", linked, "HEAD"); err != nil {
		return err
	}
	if err := writeMCPFiles(linked, linkedPrefix); err != nil {
		return err
	}
	if err := client.activate(linked, session); err != nil {
		return err
	}
	if err := verifyMCPFixture(client, linkedPrefix); err != nil {
		return err
	}
	result, err := client.tool(
		"find_symbol",
		map[string]any{"name_path_pattern": baseGoMarker, relativePathField: goFixtureFile},
	)
	if err != nil {
		return err
	}
	if strings.Contains(result, baseGoMarker) {
		return fmt.Errorf("worktree 查询混入原 checkout：%s", result)
	}
	if err := verifyGoReferencesAndDiagnostics(client, linked); err != nil {
		return err
	}
	if err := client.activate(base, session); err != nil {
		return err
	}
	if err := verifyMCPFixture(client, baseMarkerPrefix); err != nil {
		return err
	}
	fmt.Println("Serena MCP 已通过：真实 checkout、六类语言服务、文本检索、引用/重命名/诊断，以及同连接新建 worktree 与切回。")
	return nil
}
