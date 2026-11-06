package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const documentationFile = "doc.go"

type sourceFile struct {
	path string
	file *ast.File
}

// checkDocumentation 检查 root 中正式 Go 包的说明是否集中在纯文档 doc.go 中。
// 不按当前平台筛选源码；测试夹具、生成代码和外部测试包不受约束。
func checkDocumentation(root string) error {
	packages := map[string][]sourceFile{}
	err := scanSources(root, func(_ *token.FileSet, source sourceFile) {
		if strings.HasSuffix(source.path, "_test.go") && strings.HasSuffix(source.file.Name.Name, "_test") {
			return
		}
		directory := filepath.Dir(source.path)
		packages[directory] = append(packages[directory], source)
	})
	if err != nil {
		return err
	}
	var directories []string
	for directory := range packages {
		directories = append(directories, directory)
	}
	slices.Sort(directories)
	var problems []error
	for _, directory := range directories {
		problems = append(problems, checkPackage(directory, packages[directory])...)
	}
	return errors.Join(problems...)
}

func checkPackage(directory string, files []sourceFile) []error {
	var documentation *ast.File
	packageName := ""
	var problems []error
	for _, source := range files {
		if filepath.Base(source.path) == documentationFile {
			documentation = source.file
			continue
		}
		if packageName == "" {
			packageName = source.file.Name.Name
		} else if source.file.Name.Name != packageName {
			problems = append(problems, fmt.Errorf("%s: 包名不一致，期望 %s", source.path, packageName))
		}
		if !strings.HasSuffix(source.path, "_test.go") && source.file.Doc != nil {
			problems = append(problems, fmt.Errorf("%s: 包说明必须集中在 doc.go", source.path))
		}
	}
	path := filepath.ToSlash(filepath.Join(directory, documentationFile))
	if documentation == nil {
		return append(problems, fmt.Errorf("%s: 缺少 doc.go", path))
	}
	if packageName != "" && documentation.Name.Name != packageName {
		problems = append(problems, fmt.Errorf("%s: 包名不一致，期望 %s", path, packageName))
	}
	description := ""
	if documentation.Doc != nil {
		description = strings.TrimSpace(documentation.Doc.Text())
	}
	description = strings.TrimPrefix(description, "Package "+documentation.Name.Name)
	if !strings.ContainsFunc(description, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsNumber(r)
	}) {
		problems = append(problems, fmt.Errorf("%s: 包说明不能为空", path))
	}
	if len(documentation.Decls) != 0 {
		problems = append(problems, fmt.Errorf("%s: 只能包含包说明和 package 声明", path))
	}
	return problems
}
