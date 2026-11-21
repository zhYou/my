package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhYou/my/tools/internal/scaffold"
)

// checkMCP 与两个开发客户端复用服务 task，真实调用不能由配置存在或 CLI 回退替代。
func checkMCP() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	return runMCPChecks(ctx, root, []mcpCheck{
		{name: "Serena MCP", run: checkSerenaMCP},
		{name: "OpenCode Serena MCP", run: checkOpenCodeSerenaMCP},
		{name: "Graphify MCP", run: checkGraphifyMCP},
	})
}

type mcpCheck struct {
	name string
	run  func(context.Context, string, io.Writer) error
}

type mcpCheckError struct {
	err    error
	logDir string
}

func (e *mcpCheckError) Error() string {
	return fmt.Sprintf("%v；完整日志目录：%s", e.err, e.logDir)
}

func (e *mcpCheckError) Unwrap() error {
	return e.err
}

// runMCPChecks 并行运行互不共享状态的客户端验收，并在返回前汇总全部失败。
func runMCPChecks(ctx context.Context, root string, checks []mcpCheck) error {
	logDir, err := os.MkdirTemp("", mcpLogDirectoryPattern)
	if err != nil {
		return fmt.Errorf("创建 MCP 验收日志目录：%w", err)
	}
	logs := make([]*os.File, len(checks))
	for i := range checks {
		logs[i], err = os.CreateTemp(logDir, fmt.Sprintf("check-%d-*.log", i+1))
		if err != nil {
			for _, log := range logs {
				if log != nil {
					//nolint:errcheck // 已在返回创建日志失败；继续清理其余文件。
					_ = log.Close()
				}
			}
			return errors.Join(fmt.Errorf("创建 MCP 验收日志：%w", err), os.RemoveAll(logDir))
		}
	}
	results := make(chan error, len(checks))
	for i, check := range checks {
		go func(check mcpCheck, log *os.File) {
			runErr := check.run(ctx, root, log)
			closeErr := log.Close()
			if runErr != nil {
				runErr = fmt.Errorf("验收 %s：%w", check.name, runErr)
			}
			results <- errors.Join(runErr, closeErr)
		}(check, logs[i])
	}
	var result error
	for range checks {
		result = errors.Join(result, <-results)
	}
	if result == nil {
		if err := os.RemoveAll(logDir); err != nil {
			return fmt.Errorf("清理 MCP 验收日志目录 %s：%w", logDir, err)
		}
		return nil
	}
	return &mcpCheckError{err: result, logDir: logDir}
}

func checkSerenaMCP(ctx context.Context, root string, stderr io.Writer) (resultErr error) {
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
	client, closeClient, err := startMCP(
		ctx,
		root,
		"mcp:serve",
		stderr,
		"SERENA_HOME="+filepath.Join(temporary, "serena-home"),
	)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeClient())
	}()
	if err := client.initialize(
		initialInstructionsTool, "activate_project", "get_current_config", findSymbolTool, symbolsOverviewTool,
		"find_referencing_symbols", "rename_symbol", "get_diagnostics_for_file", "search_for_pattern",
	); err != nil {
		return err
	}
	instructions, err := client.tool(initialInstructionsTool, map[string]any{})
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
		findSymbolTool,
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

func checkOpenCodeSerenaMCP(ctx context.Context, root string, stderr io.Writer) (resultErr error) {
	temporary, err := os.MkdirTemp("", "serena-opencode-mcp-check-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(temporary))
	}()
	client, closeClient, err := startMCP(
		ctx,
		root,
		openCodeMCPTask,
		stderr,
		"SERENA_HOME="+filepath.Join(temporary, "serena-home"),
	)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeClient())
	}()
	if err := client.initialize(
		initialInstructionsTool,
		findSymbolTool,
		symbolsOverviewTool,
	); err != nil {
		return err
	}
	instructions, err := client.tool(initialInstructionsTool, map[string]any{})
	if err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if !strings.Contains(instructions, canonical) &&
		!strings.Contains(filepath.ToSlash(instructions), filepath.ToSlash(canonical)) {
		return fmt.Errorf("启动上下文未绑定当前 checkout %s：%s", canonical, instructions)
	}
	if _, err := sessionFromInstructions(instructions); err != nil {
		return err
	}
	project, err := scaffold.Inspect(root)
	if err != nil {
		return err
	}
	if err := client.overview(filepath.ToSlash(filepath.Join(project.CommandPath, "main.go")), "main"); err != nil {
		return err
	}
	fmt.Println("OpenCode Serena MCP 已通过：ide context 绑定当前 checkout，并完成代表性符号查询。")
	return nil
}
