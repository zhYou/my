package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// checkDiagnosticReferences 检查当前构建上下文内生产代码的常量诊断消息引用。
// 动态值交由领域测试验证；省略 Code 的归一化包装不引入消息引用。
func checkDiagnosticReferences(ctx context.Context, root string, ids map[string]bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	//nolint:gosec // G304：只读取调用者指定 checkout 的固定模块声明。
	data, err := os.ReadFile(filepath.Join(root, moduleFile))
	if err != nil {
		return fmt.Errorf("读取模块身份：%w", err)
	}
	module := modfile.ModulePath(data)
	if module == "" {
		return fmt.Errorf("模块身份无效")
	}
	loaded, err := packages.Load(&packages.Config{
		Context: ctx, Dir: root, Mode: packages.LoadSyntax,
		Env: append(os.Environ(), "GOWORK=off"), BuildFlags: []string{"-mod=readonly"},
	}, "./internal/...", "./cmd/...")
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	if err != nil {
		return fmt.Errorf("加载诊断引用源码：%w", err)
	}
	if len(loaded) == 0 {
		return fmt.Errorf("未找到运行时源码")
	}
	var problems []error
	for _, pkg := range loaded {
		for _, problem := range pkg.Errors {
			problems = append(problems, errors.New(problem.Error()))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return fmt.Errorf("加载诊断引用源码：%w", err)
	}
	var references []diagnosticReference
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			if ast.IsGenerated(file) {
				continue
			}
			references = append(
				references,
				collectDiagnosticReferences(pkg.Fset, pkg.TypesInfo, file, module+"/internal/base/fault")...)
		}
	}
	slices.SortFunc(references, func(a, b diagnosticReference) int {
		if a.position.Filename != b.position.Filename {
			return strings.Compare(a.position.Filename, b.position.Filename)
		}
		if a.position.Line != b.position.Line {
			return a.position.Line - b.position.Line
		}
		return a.position.Column - b.position.Column
	})
	for _, reference := range references {
		if ids[reference.id] || reference.id == "" && reference.owner == "Definition" {
			continue
		}
		path, err := filepath.Rel(root, reference.position.Filename)
		if err != nil {
			return err
		}
		problems = append(
			problems,
			fmt.Errorf(
				"%s:%d:%d: DIAG001：%s.Code 引用消息 %q 不存在，请同时补齐双语消息目录",
				filepath.ToSlash(path),
				reference.position.Line,
				reference.position.Column,
				reference.owner,
				reference.id,
			),
		)
	}
	return errors.Join(problems...)
}

const diagnosticCodeField = "Code"

type diagnosticReference struct {
	position token.Position
	owner    string
	id       string
}

func collectDiagnosticReferences(
	fset *token.FileSet,
	info *types.Info,
	file *ast.File,
	faultPath string,
) []diagnosticReference {
	var references []diagnosticReference
	record := func(owner string, expr ast.Expr) {
		value := info.Types[expr].Value
		if value != nil && value.Kind() == constant.String {
			references = append(
				references,
				diagnosticReference{fset.Position(expr.Pos()), owner, constant.StringVal(value)},
			)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.CompositeLit:
			owner := diagnosticOwner(info.TypeOf(current), faultPath)
			if owner == "" {
				break
			}
			fields, ok := info.TypeOf(current).Underlying().(*types.Struct)
			if !ok {
				break
			}
			for i, element := range current.Elts {
				if pair, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := pair.Key.(*ast.Ident); ok && key.Name == diagnosticCodeField {
						record(owner, pair.Value)
					}
				} else if i < fields.NumFields() && fields.Field(i).Name() == diagnosticCodeField {
					record(owner, element)
				}
			}
		case *ast.AssignStmt:
			if len(current.Lhs) != len(current.Rhs) || current.Tok != token.ASSIGN {
				break
			}
			for i, lhs := range current.Lhs {
				selector, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != diagnosticCodeField {
					continue
				}
				if owner := diagnosticFieldOwner(info.Selections[selector], faultPath); owner != "" {
					record(owner, current.Rhs[i])
				}
			}
		}
		return true
	})
	return references
}

func diagnosticOwner(t types.Type, faultPath string) string {
	if t == nil {
		return ""
	}
	t = types.Unalias(t)
	if pointer, ok := t.(*types.Pointer); ok {
		t = types.Unalias(pointer.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != faultPath {
		return ""
	}
	name := named.Obj().Name()
	if name == "Definition" || name == "Recovery" {
		return name
	}
	return ""
}

// diagnosticFieldOwner 沿提升字段的选择路径定位实际声明 Code 的结构体。
func diagnosticFieldOwner(selection *types.Selection, faultPath string) string {
	if selection == nil || selection.Kind() != types.FieldVal {
		return ""
	}
	t := selection.Recv()
	indices := selection.Index()
	for _, index := range indices[:len(indices)-1] {
		t = types.Unalias(t)
		if pointer, ok := t.(*types.Pointer); ok {
			t = types.Unalias(pointer.Elem())
		}
		structure, ok := t.Underlying().(*types.Struct)
		if !ok {
			return ""
		}
		t = structure.Field(index).Type()
	}
	return diagnosticOwner(t, faultPath)
}
