package scaffold

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/spf13/afero"
	"golang.org/x/mod/modfile"
)

// Project 是从项目文件静态读取的应用身份。
type Project struct {
	Module      string
	Name        string
	CommandPath string
}

// Inspect 查找唯一命令入口，不执行项目代码。
func Inspect(root string) (Project, error) {
	i := (&Initializer{}).defaults()
	return i.inspect(root)
}

func (i *Initializer) inspect(root string) (Project, error) {
	var project Project
	modulePath, err := i.safePath(root, moduleFile)
	if err != nil {
		return project, err
	}
	data, err := afero.ReadFile(i.FS, modulePath)
	if err != nil {
		return project, err
	}
	module, err := modfile.Parse(moduleFile, data, nil)
	if err != nil {
		return project, fmt.Errorf("解析模块身份失败：%w", err)
	}
	if module.Module == nil {
		return project, fmt.Errorf("项目缺少模块路径")
	}
	project.Module = module.Module.Mod.Path
	cmd, err := i.safePath(root, "cmd")
	if err != nil {
		return project, err
	}
	entries, err := afero.ReadDir(i.FS, cmd)
	if os.IsNotExist(err) {
		return project, fmt.Errorf("未发现 cmd 下的命令入口；旧布局请按迁移说明处理")
	}
	if err != nil {
		return project, err
	}
	count := 0
	for _, entry := range entries {
		relative := path.Join("cmd", entry.Name())
		if _, err := i.safePath(root, relative); err != nil {
			return project, err
		}
		if !entry.IsDir() {
			continue
		}
		file, err := i.safePath(root, path.Join(relative, mainFile))
		if err != nil {
			return project, err
		}
		source, err := afero.ReadFile(i.FS, file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return project, err
		}
		name, err := entryName(file, source)
		if err != nil {
			return project, err
		}
		if err := validateCommandName(name); err != nil {
			return project, fmt.Errorf("命令入口 %s 的名称无效：%w", relative, err)
		}
		if name != entry.Name() {
			return project, fmt.Errorf("命令目录与有效 appName 不一致：%s", relative)
		}
		count++
		project.Name = name
		project.CommandPath = filepath.ToSlash(relative)
	}
	if count != 1 {
		return project, fmt.Errorf("项目必须有唯一命令入口，实际发现 %d 个", count)
	}
	return project, nil
}

func entryName(filename string, source []byte) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, source, 0)
	if err != nil {
		return "", fmt.Errorf("解析命令入口失败：%w", err)
	}
	name := ""
	main := false
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, ident := range value.Names {
					if ident.Name != "appName" || index >= len(value.Values) {
						continue
					}
					if literal, ok := value.Values[index].(*ast.BasicLit); ok && literal.Kind == token.STRING {
						var err error
						name, err = strconv.Unquote(literal.Value)
						if err != nil {
							return "", fmt.Errorf("解析 appName 失败：%w", err)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.Name == "main" && d.Recv == nil && d.Type.TypeParams == nil && d.Type.Params.NumFields() == 0 &&
				d.Type.Results.NumFields() == 0 &&
				d.Body != nil {
				main = true
			}
		}
	}
	if file.Name.Name != "main" || !main || name == "" {
		return "", fmt.Errorf("命令入口须定义 main 包、main 函数和 appName 常量：%s", filename)
	}
	return name, nil
}
