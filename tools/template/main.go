package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：获取当前目录失败：", err)
		os.Exit(1)
	}
	os.Exit(run(os.Args[1:], root, os.Stdout, os.Stderr))
}

func run(args []string, root string, out, errOut io.Writer) int {
	if len(args) != 0 {
		switch args[0] {
		case "init":
			return initialize(args[1:], root, out, errOut)
		case "check":
			if len(args) == 1 {
				if err := verifyTemplate(); err != nil {
					//nolint:errcheck // 已在报告失败；stderr 不可用时保留既定失败退出码。
					_, _ = fmt.Fprintln(errOut, "错误：", err)
					return 1
				}
				return 0
			}
		}
	}
	//nolint:errcheck // 已在报告失败；stderr 不可用时保留既定失败退出码。
	_, _ = fmt.Fprintln(errOut, "错误：用法：template <init --module 模块路径 --name 名称|check>")
	return 2
}
