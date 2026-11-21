package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zhYou/my/internal/locales"
)

// lintCheck 声明一个开发检查；name 同时用于命令行分发和用法文本。
type lintCheck struct {
	name string
	run  func() error
}

// lintRegistry 是检查名、用法和分发的唯一来源。
type lintRegistry []lintCheck

// lintChecks 登记全部开发检查；新增检查只需在此增加一条声明。
var lintChecks = lintRegistry{
	{name: "i18n", run: func() error {
		return checkProjectMessages(context.Background(), ".", locales.Catalog())
	}},
	{name: "config", run: func() error {
		return checkConfig(".")
	}},
	{name: "opencode", run: func() error {
		return checkOpenCodeConfig(".")
	}},
	{name: "codex-rules", run: func() error {
		return checkCodexRules(".")
	}},
	{name: "doc", run: func() error {
		return checkDocumentation(".")
	}},
	{name: "style", run: func() error {
		return checkStyle(".")
	}},
	{name: "versions", run: func() error {
		return checkVersions(".")
	}},
	{name: "test", run: func() error {
		return checkRules(context.Background())
	}},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return lintChecks.run(args)
}

// usage 汇总注册表中的检查名，确保用法文本与分发不会漂移。
func (r lintRegistry) usage() string {
	names := make([]string, len(r))
	for i, check := range r {
		names[i] = check.name
	}
	return "用法：lint <" + strings.Join(names, "|") + ">"
}

// run 校验参数并执行唯一匹配的检查；参数不足、多余或未知时返回包含用法文本的错误。
func (r lintRegistry) run(args []string) error {
	if len(args) != 1 {
		return errors.New(r.usage())
	}
	for _, check := range r {
		if check.name == args[0] {
			return check.run()
		}
	}
	return fmt.Errorf("%s，未知检查 %q", r.usage(), args[0])
}
