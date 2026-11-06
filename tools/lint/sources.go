package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// scanSources 扫描所有平台的手写源码；不跟随符号链接，也不读取构建产物和工作记录。
func scanSources(root string, visit func(*token.FileSet, sourceFile)) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("解析仓库路径: %w", err)
	}
	fset := token.NewFileSet()
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("读取 %s: %w", path, walkErr)
		}
		name := entry.Name()
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if path != root &&
				(strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == testdataDirectory || name == "vendor" || relative == "bin" || relative == "dist" || relative == "docs/superpowers") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("检查 %s: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s 不是普通文件", relative)
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("解析 %s: %w", relative, err)
		}
		if !ast.IsGenerated(file) {
			visit(fset, sourceFile{relative, file})
		}
		return nil
	})
}
