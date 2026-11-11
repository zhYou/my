package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cmdexec "github.com/zhYou/my/internal/command"
	"github.com/zhYou/my/tools/internal/fixture"
)

func fixtureGit(ctx context.Context, root string, args ...string) error {
	result := cmdexec.Capture(
		ctx,
		cmdexec.Invocation{Program: "git", Args: args},
		cmdexec.Execution{Dir: root},
		cmdexec.CaptureOptions{},
	)
	if result.Err != nil {
		return fmt.Errorf("fixture git：%s：%w", result.Stderr, result.Err)
	}
	return nil
}

func prepareMCPFixture(ctx context.Context, root, base string) error {
	if err := os.MkdirAll(filepath.Join(base, serenaDirectory), 0o700); err != nil {
		return err
	}
	if err := fixture.CopyFile(
		filepath.Join(root, serenaDirectory, serenaProjectFile),
		filepath.Join(base, serenaDirectory, serenaProjectFile),
	); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(base, "go.mod"),
		[]byte("module example.com/mcp-fixture\n\ngo 1.27.4\n"),
		0o600,
	); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(base, ".gitignore"),
		[]byte("/.worktrees/\n/.serena/*\n!/.serena/project.yml\n"),
		0o600,
	); err != nil {
		return err
	}
	if err := writeMCPFiles(base, baseMarkerPrefix); err != nil {
		return err
	}
	if err := fixtureGit(ctx, base, "-c", "init.templateDir=", "init", quietFlag); err != nil {
		return err
	}
	if err := fixtureGit(ctx, base, gitAddCommand, "."); err != nil {
		return err
	}
	return fixtureGit(
		ctx,
		base,
		"-c",
		"user.name=MCP fixture",
		"-c",
		"user.email=mcp@example.invalid",
		"-c",
		"commit.gpgsign=false",
		"-c",
		"core.hooksPath="+filepath.Join(base, ".no-hooks"),
		"commit",
		quietFlag,
		"-m",
		"fixture",
	)
}

func writeMCPFiles(root, prefix string) error {
	files := map[string]string{
		goFixtureFile:       "package fixture\nfunc " + prefix + "GoMarker() int { return 1 }\nfunc UseMarker() int { return " + prefix + "GoMarker() }\n",
		"sample.cjs":        "function " + prefix + "JsMarker() { return 1; }\nmodule.exports = " + prefix + "JsMarker;\n",
		"sample.json":       "{\"" + prefix + "JsonMarker\": 1}\n",
		"sample.yaml":       prefix + "YamlMarker: 1\n",
		"sample.toml":       prefix + "TomlMarker = 1\n",
		"sample.md":         "# " + prefix + "MarkdownMarker\n",
		".hidden/notes.txt": prefix + "PlainMarker\n",
	}
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func verifyMCPFixture(client *mcpClient, prefix string) error {
	for _, item := range []struct {
		path string
		kind string
	}{
		{goFixtureFile, "Go"},
		{"sample.cjs", "Js"},
		{"sample.json", "Json"},
		{"sample.yaml", "Yaml"},
		{"sample.toml", "Toml"},
		{"sample.md", "Markdown"},
	} {
		if err := client.overview(item.path, prefix+item.kind+"Marker"); err != nil {
			return err
		}
	}
	text, err := client.tool("search_for_pattern", map[string]any{
		"substring_pattern":             prefix + "PlainMarker",
		relativePathField:               ".hidden",
		"restrict_search_to_code_files": false,
	})
	if err != nil {
		return err
	}
	if !strings.Contains(text, prefix+"PlainMarker") {
		return fmt.Errorf("非代码隐藏文件未被检索：%s", text)
	}
	return nil
}

func verifyGoReferencesAndDiagnostics(client *mcpClient, root string) error {
	text, err := client.tool(
		"find_referencing_symbols",
		map[string]any{"name_path": linkedGoMarker, relativePathField: goFixtureFile},
	)
	if err != nil {
		return err
	}
	if !strings.Contains(text, useGoMarker) {
		return fmt.Errorf("未返回 Go 引用调用方：%s", text)
	}
	if _, err := client.tool(
		"rename_symbol",
		map[string]any{"name_path": linkedGoMarker, relativePathField: goFixtureFile, "new_name": "RenamedGoMarker"},
	); err != nil {
		return err
	}
	//nolint:gosec // G304：只读取本次验收创建的临时 worktree 文件。
	data, err := os.ReadFile(filepath.Join(root, goFixtureFile))
	if err != nil {
		return err
	}
	if strings.Contains(string(data), linkedGoMarker) || strings.Count(string(data), "RenamedGoMarker") != 2 {
		return fmt.Errorf("重命名未同时更新 Go 声明与引用：%s", data)
	}
	if err := os.WriteFile(
		filepath.Join(root, "broken.go"),
		[]byte("package fixture\nfunc Broken() int { return \"wrong\" }\n"),
		0o600,
	); err != nil {
		return err
	}
	text, err = client.tool(
		"get_diagnostics_for_file",
		map[string]any{relativePathField: "broken.go", "min_severity": 1},
	)
	if err != nil {
		return err
	}
	if !strings.Contains(text, "cannot use") {
		return fmt.Errorf("未诊断出已知 Go 类型错误：%s", text)
	}
	return nil
}
