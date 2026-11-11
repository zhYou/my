package main

import (
	"context"
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
		return fmt.Errorf("用法：lint <i18n|config|doc|style|versions|test>")
	}
	switch args[0] {
	case "i18n":
		return checkProjectMessages(context.Background(), ".", locales.Catalog())
	case "config":
		return checkConfig(".")
	case "style":
		return checkStyle(".")
	case "doc":
		return checkDocumentation(".")
	case "versions":
		return checkVersions(".")
	case "test":
		return checkRules(context.Background())
	default:
		return fmt.Errorf("用法：lint <i18n|config|doc|style|versions|test>，未知检查 %q", args[0])
	}
}
