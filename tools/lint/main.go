package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/zhYou/my/internal/locales"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return errors.New(lintUsage)
	}
	switch args[0] {
	case "i18n":
		return checkProjectMessages(context.Background(), ".", locales.Catalog())
	case "config":
		return checkConfig(".")
	case "opencode":
		return checkOpenCodeConfig(".")
	case "agent-rules":
		return checkAgentRules(".")
	case "style":
		return checkStyle(".")
	case "doc":
		return checkDocumentation(".")
	case "versions":
		return checkVersions(".")
	case "test":
		return checkRules(context.Background())
	default:
		return fmt.Errorf("%s，未知检查 %q", lintUsage, args[0])
	}
}
