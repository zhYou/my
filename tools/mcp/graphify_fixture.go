package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	cmdexec "github.com/zhYou/my/internal/command"
	"github.com/zhYou/my/tools/internal/fixture"
)

func graphifyJSON(project string) string {
	return filepath.Join(project, "graphify-out", "graph.json")
}

func prepareGraphifyFiles(root, base string) error {
	if err := fixture.CopyFile(
		filepath.Join(root, ".graphifyignore"),
		filepath.Join(base, ".graphifyignore"),
	); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(base, "testdata"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(base, "testdata", "ignored.go"),
		[]byte("package fixture\nfunc IgnoredGoMarker() int { return 1 }\n"), 0o600,
	); err != nil {
		return err
	}
	// Graphify 更新会读取 Git 状态，生成索引不得混入待分析的源码。
	//nolint:gosec // G304：只追加本次验收创建的临时 Git 夹具中的固定忽略文件。
	file, err := os.OpenFile(filepath.Join(base, ".gitignore"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString("/graphify-out/\n")
	return errors.Join(writeErr, file.Close())
}

func runGraphify(ctx context.Context, root string, args ...string) (string, error) {
	result := cmdexec.Capture(ctx, cmdexec.Invocation{
		Program: miseProgram,
		Args:    append([]string{"run", quietFlag, "graphify", "--"}, args...),
	}, cmdexec.Execution{Dir: root}.WithEnv("GRAPHIFY_QUERY_LOG_DISABLE=1"), cmdexec.CaptureOptions{})
	if result.Err != nil {
		return "", fmt.Errorf("运行 Graphify CLI：%s：%w", result.Stderr, result.Err)
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return "", errors.New("验收 Graphify CLI 时输出超过捕获上限")
	}
	return string(result.Stdout), nil
}
