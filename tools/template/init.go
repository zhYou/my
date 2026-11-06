package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/zhYou/my/tools/internal/scaffold"
)

func initialize(args []string, root string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	module := flags.String("module", "", "Go 模块路径，例如 github.com/acme/mycli")
	name := flags.String("name", "", "可执行文件名称，例如 mycli；不能使用 v2、v10 等 Go 版本段")
	usage := func() error {
		var output strings.Builder
		fmt.Fprintln(&output, "用法：mise run template:init --module 模块路径 --name 名称")
		flags.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(&output, "  --%s <string>\t%s\n", f.Name, f.Usage)
		})
		_, err := io.WriteString(out, output.String())
		return err
	}
	flags.Usage = func() {}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if err := usage(); err != nil {
				return 1
			}
			return 0
		}
		//nolint:errcheck // 已在报告错误；错误出口失败时保留原退出码，不递归报告。
		_, _ = fmt.Fprintln(errOut, "错误：解析参数失败：", err)
		return 2
	}
	if flags.NArg() != 0 {
		//nolint:errcheck // 已在报告错误；错误出口失败时保留原退出码，不递归报告。
		_, _ = fmt.Fprintln(errOut, "错误：不接受位置参数")
		return 2
	}
	changed, err := scaffold.Initialize(root, *module, *name)
	if err != nil {
		//nolint:errcheck // 已在报告错误；错误出口失败时保留原退出码，不递归报告。
		_, _ = fmt.Fprintln(errOut, "错误：", err)
		return 1
	}
	_, writeErr := fmt.Fprintf(out, "已初始化 %s（%s），更新了 %d 个文件。\n", *module, *name, len(changed))
	if writeErr != nil {
		return 1
	}
	return 0
}
