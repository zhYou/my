package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhYou/my/tools/internal/fixture"
	"github.com/zhYou/my/tools/internal/scaffold"

	cmdexec "github.com/zhYou/my/internal/command"
)

// checkRules 使用真实类型分析及仓库配置验证边界；fixture 不参与产品构建。
func checkRules(ctx context.Context) error {
	for _, kind := range []string{"violations", "compliant"} {
		if err := checkRuleFixture(ctx, kind); err != nil {
			return err
		}
	}
	return nil
}

func checkRuleFixture(ctx context.Context, kind string) (resultErr error) {
	module, _, err := scaffold.Identity(".")
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "starter-rules-")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(dir))
	}()
	for _, name := range []string{moduleFile, "go.sum", ".golangci.yml"} {
		if err := fixture.CopyFile(name, filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	expected := map[string]int{}
	source := filepath.Join("tools", "lint", testdataDirectory, "rules", kind)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, strings.TrimSuffix(rel, ".txt"))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		//nolint:gosec // G304：路径来自受版本管理的 fixture 清单，相对路径仅写入本次临时目录。
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = []byte(strings.ReplaceAll(string(data), "{{MODULE}}", module))
		for line, text := range strings.Split(string(data), "\n") {
			if _, linter, ok := strings.Cut(text, "// want "); ok {
				expected[fmt.Sprintf("%s:%d:%s", filepath.ToSlash(strings.TrimSuffix(rel, ".txt")), line+1, strings.TrimSpace(linter))]++
			}
		}
		//nolint:gosec // G703：路径来自受版本管理的 fixture 清单，相对路径仅写入本次临时目录。
		return os.WriteFile(dest, data, 0o600)
	})
	if err != nil {
		return err
	}
	result := cmdexec.Capture(ctx, cmdexec.Invocation{
		Program: lintProgram,
		Args: []string{
			runCommand,
			"--enable-only=depguard,forbidigo,loggercheck,errcheck,revive",
			"--allow-serial-runners",
			"--issues-exit-code=0",
			"--show-stats=false",
			"--max-issues-per-linter=0",
			"--max-same-issues=0",
			"--output.json.path=stdout",
			"--output.text.path=",
			allPackages,
		},
	}, cmdexec.Execution{Dir: dir}, cmdexec.CaptureOptions{})
	if result.Err != nil {
		return fmt.Errorf("%s fixture 分析失败：%w\n%s", kind, result.Err, result.Stderr)
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return fmt.Errorf("%s fixture 分析输出截断", kind)
	}
	var report struct {
		Issues []struct {
			FromLinter string
			Pos        struct {
				Filename string
				Line     int
			}
		}
	}
	if err := json.Unmarshal(result.Stdout, &report); err != nil {
		return fmt.Errorf("%s fixture 分析结果无效：%w\n%s", kind, err, result.Stdout)
	}
	var problems []error
	for _, issue := range report.Issues {
		file := issue.Pos.Filename
		if filepath.IsAbs(file) {
			file, err = filepath.Rel(dir, file)
			if err != nil {
				return err
			}
		}
		key := fmt.Sprintf("%s:%d:%s", filepath.ToSlash(file), issue.Pos.Line, issue.FromLinter)
		if expected[key] == 0 {
			problems = append(problems, fmt.Errorf("%s fixture 多出诊断 %s", kind, key))
		} else {
			expected[key]--
		}
	}
	for key, count := range expected {
		if count != 0 {
			problems = append(problems, fmt.Errorf("%s fixture 缺少诊断 %s", kind, key))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return err
	}
	fmt.Printf("通过：%s fixture（%d 条真实诊断）\n", kind, len(report.Issues))
	return nil
}
