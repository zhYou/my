package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// checkStyle 检查具名和匿名结构体字段，避免合并声明掩盖字段边界。
func checkStyle(root string) error {
	var problems []error
	err := scanSources(root, func(fset *token.FileSet, source sourceFile) {
		ast.Inspect(source.file, func(node ast.Node) bool {
			structure, ok := node.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structure.Fields.List {
				if len(field.Names) < 2 {
					continue
				}
				names := make([]string, len(field.Names))
				for i, name := range field.Names {
					names[i] = name.Name
				}
				problems = append(
					problems,
					fmt.Errorf(
						"%s:%d: 结构体字段须逐项声明：%s",
						source.path,
						fset.Position(field.Pos()).Line,
						strings.Join(names, ", "),
					),
				)
			}
			return true
		})
	})
	return errors.Join(append(problems, err)...)
}
