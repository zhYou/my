package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// validCompletionInput 在独立命令树解析已完成参数，避免 Cobra 将解析诊断直接写进程 stderr。
// 正在输入的值不参与解析；不修改真正执行补全的 flag 值、Changed 状态或回调。
func validCompletionInput(root *cobra.Command, args []string, index int) bool {
	if len(args) <= index+1 {
		return true
	}
	last := args[len(args)-1]
	complete := append([]string(nil), args[:index]...)
	complete = append(complete, args[index+1:len(args)-1]...)
	target, remaining, err := root.Find(complete)
	if err != nil {
		return false
	}
	unknownValueFlag := false
	if strings.HasPrefix(last, "-") {
		if before, _, equal := strings.Cut(last, "="); equal {
			name := strings.TrimPrefix(before, "--")
			short := !strings.HasPrefix(before, "--")
			if short {
				name = before[len(before)-1:]
			}
			unknownValueFlag = commandFlag(target, name, short) == nil
		}
	} else if len(remaining) > 0 {
		previous := remaining[len(remaining)-1]
		if strings.HasPrefix(previous, "-") && previous != "-" && previous != "--" && !strings.Contains(previous, "=") {
			name := strings.TrimPrefix(previous, "--")
			short := !strings.HasPrefix(previous, "--")
			if short {
				name = previous[len(previous)-1:]
			}
			flag := commandFlag(target, name, short)
			if flag == nil {
				unknownValueFlag = true
			} else if flag.NoOptDefVal == "" {
				remaining = remaining[:len(remaining)-1]
			}
		}
	}
	if err := target.ParseFlags(remaining); err != nil {
		return false
	}
	return !unknownValueFlag || target.ArgsLenAtDash() >= 0
}
