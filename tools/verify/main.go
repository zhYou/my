package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：获取当前目录失败：", err)
		return 1
	}
	if err := run(ctx, os.Args[1:], root, defaultDependencies()); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		return exitCode(err)
	}
	return 0
}

func exitCode(err error) int {
	switch {
	case errors.Is(err, errUsage):
		return 2
	case errors.Is(err, context.Canceled):
		return 130
	default:
		return 1
	}
}
