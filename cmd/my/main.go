package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhYou/my/internal/app/cli"
)

const appName = "my"

// 发布构建通过 -ldflags 注入这些值。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], nil, signalContext))
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func run(args []string, deps *cli.Dependencies, notify func() (context.Context, context.CancelFunc)) int {
	ctx, stop := notify()
	defer stop()
	return cli.Run(
		ctx,
		args,
		cli.Options{
			Name:         appName,
			Version:      version,
			Commit:       commit,
			Date:         date,
			In:           os.Stdin,
			Out:          os.Stdout,
			ErrOut:       os.Stderr,
			Dependencies: deps,
		},
	)
}
