package main

import (
	"errors"
	"os"
)

// errNotRegularFile 表示路径存在但不是普通文件；符号链接不会跟随。
var errNotRegularFile = errors.New("不是普通文件")

// checkRegularFile 确认 path 是普通文件。符号链接及其他非普通文件返回 errNotRegularFile，
// 读取元数据失败则返回原始错误；调用者负责附加面向用户的名称。
func checkRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errNotRegularFile
	}
	return nil
}
