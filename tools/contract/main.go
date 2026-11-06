package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：contract <check [--against Git引用]|update>")
	}
	switch args[0] {
	case "check":
		against := ""
		if len(args) == 3 && args[1] == "--against" {
			against = args[2]
		} else if len(args) != 1 {
			return fmt.Errorf("用法：contract check [--against Git引用]")
		}
		return checkContract(context.Background(), against)
	case "update":
		if len(args) != 1 {
			return fmt.Errorf("用法：contract update")
		}
		return updateContract(context.Background())
	default:
		return fmt.Errorf("用法：contract <check [--against Git引用]|update>")
	}
}
