package main

import (
	"fmt"
	"os"
)

// checkRegularFile 确认 path 是普通文件；符号链接及其他非普通文件返回带路径的错误，
// 读取元数据失败时保留原始错误链。
func checkRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf(checkPathError, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(notRegularError, path)
	}
	return nil
}
