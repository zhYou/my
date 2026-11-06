package scaffold

import "fmt"

func validateCommandName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("名称必须以小写字母开头，且只能包含小写字母、数字或连字符")
	}
	// Go 会把无前导零且至少为 v2 的目录段当作模块版本，改用父目录作为安装名。
	version := len(name) >= 2 && name[0] == 'v' && name[1] != '0' && name != "v1"
	if version {
		for index := 1; index < len(name); index++ {
			if name[index] < '0' || name[index] > '9' {
				version = false
				break
			}
		}
	}
	if version {
		return fmt.Errorf("名称不能使用 v2、v10 等 Go 安装路径的版本段，请选择其他名称")
	}
	return nil
}
