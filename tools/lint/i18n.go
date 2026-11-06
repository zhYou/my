package main

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"text/template"
	"text/template/parse"

	"github.com/zhYou/my/internal/base/messages/catalog"
)

// templateValue 跟踪根对象与可证明的字符串字面量；未知值不能用于判断根参数。
type templateValue struct {
	known     bool
	root      bool
	literal   string
	isLiteral bool
}

type templateAnalyzer struct {
	template  *template.Template
	params    map[string]bool
	active    map[string]bool
	depth     int
	pipeDepth int
	err       error
}

// checkI18n 校验集中目录的语法、双语 ID 和各复数形式参数并集。
func checkI18n(source fs.FS) error {
	names, err := catalog.Files(source)
	if err != nil {
		return err
	}
	files := make(map[string]string)
	for _, name := range names {
		if path.Ext(name) != ".toml" {
			continue
		}
		base := path.Base(name)
		if base != "active.en.toml" && base != "active.zh-CN.toml" {
			return fmt.Errorf("不支持的消息文件 %q", name)
		}
		if files[base] != "" {
			return fmt.Errorf("消息文件 %q 重复", base)
		}
		files[base] = name
	}
	read := func(lang string) (map[string]map[string]string, error) {
		base := "active." + lang + ".toml"
		name := files[base]
		if name == "" {
			return nil, fmt.Errorf("缺少消息文件 %s", base)
		}
		data, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, fmt.Errorf("读取 %s：%w", name, err)
		}
		result, err := catalog.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", name, err)
		}
		return result, nil
	}
	en, err := read("en")
	if err != nil {
		return err
	}
	zh, err := read("zh-CN")
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(en))
	for id := range en {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		other, ok := zh[id]
		if !ok {
			return fmt.Errorf("zh-CN 缺少消息 %q", id)
		}
		enParams, err := parameters(en[id])
		if err != nil {
			return fmt.Errorf("消息 %q 的 en 模板：%w", id, err)
		}
		zhParams, err := parameters(other)
		if err != nil {
			return fmt.Errorf("消息 %q 的 zh-CN 模板：%w", id, err)
		}
		if !slices.Equal(enParams, zhParams) {
			return fmt.Errorf("消息 %q 的参数不一致：en=%v，zh-CN=%v", id, enParams, zhParams)
		}
	}
	for id := range zh {
		if _, ok := en[id]; !ok {
			return fmt.Errorf("en 缺少消息 %q", id)
		}
	}
	return nil
}

func parameters(entry map[string]string) ([]string, error) {
	set := make(map[string]bool)
	for form, text := range entry {
		if form == "description" {
			continue
		}
		t, err := template.New("").Parse(text)
		if err != nil {
			return nil, err
		}
		a := templateAnalyzer{template: t, params: set, active: make(map[string]bool)}
		a.collect(t.Root, true, map[string]templateValue{"$": {known: true, root: true}})
		if a.err != nil {
			return nil, a.err
		}
	}
	result := make([]string, 0, len(set))
	for key := range set {
		result = append(result, key)
	}
	slices.Sort(result)
	return result, nil
}

func (a *templateAnalyzer) collect(node parse.Node, rootDot bool, aliases map[string]templateValue) {
	if a.err != nil {
		return
	}
	switch n := node.(type) {
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				a.collect(child, rootDot, aliases)
			}
		}
	case *parse.ActionNode:
		a.collect(n.Pipe, rootDot, aliases)
		a.bindAliases(n.Pipe, rootDot, aliases, false)
	case *parse.PipeNode:
		if n != nil {
			if a.pipeDepth > 0 && (len(n.Decl) > 0 || n.IsAssign) {
				a.err = fmt.Errorf("不支持嵌套管道中的模板变量声明或赋值")
				return
			}
			a.pipeDepth++
			var previous *templateValue
			for _, command := range n.Cmds {
				a.collectCommand(command, rootDot, aliases, previous)
				value := commandValue(command, rootDot, aliases, previous)
				previous = &value
			}
			a.pipeDepth--
		}
	case *parse.FieldNode:
		if rootDot && len(n.Ident) > 0 {
			a.params[n.Ident[0]] = true
		}
	case *parse.VariableNode:
		if len(n.Ident) > 1 {
			value := aliases[n.Ident[0]]
			if !value.known {
				a.err = fmt.Errorf("无法静态确定模板变量 %q", n.Ident[0])
			} else if value.root {
				a.params[n.Ident[1]] = true
			}
		}
	case *parse.ChainNode:
		a.collect(n.Node, rootDot, aliases)
		value := nodeValue(n.Node, rootDot, aliases)
		if !value.known {
			a.err = fmt.Errorf("无法静态确定模板链的上下文")
		} else if value.root && len(n.Field) > 0 {
			a.params[n.Field[0]] = true
		}
	case *parse.IfNode:
		aliases = maps.Clone(aliases)
		a.collect(n.Pipe, rootDot, aliases)
		a.bindAliases(n.Pipe, rootDot, aliases, true)
		a.collectBranches(n.List, n.ElseList, rootDot, rootDot, aliases)
	case *parse.RangeNode:
		aliases = maps.Clone(aliases)
		a.collect(n.Pipe, rootDot, aliases)
		if n.Pipe != nil && n.Pipe.IsAssign {
			a.err = fmt.Errorf("不支持控制结构中的模板变量赋值")
			return
		}
		bodyAliases, elseAliases := maps.Clone(aliases), maps.Clone(aliases)
		if n.Pipe != nil {
			initial := pipeValue(n.Pipe, rootDot, aliases)
			for _, variable := range n.Pipe.Decl {
				bodyAliases[variable.Ident[0]] = templateValue{known: true}
				elseAliases[variable.Ident[0]] = initial
			}
		}
		a.depth++
		a.collect(n.List, false, bodyAliases)
		a.collect(n.ElseList, rootDot, elseAliases)
		a.depth--
	case *parse.WithNode:
		aliases = maps.Clone(aliases)
		a.collect(n.Pipe, rootDot, aliases)
		bodyRoot, err := templateContext(n.Pipe, rootDot, aliases)
		if err != nil {
			a.err = err
			return
		}
		a.bindAliases(n.Pipe, rootDot, aliases, true)
		a.collectBranches(n.List, n.ElseList, bodyRoot, rootDot, aliases)
	case *parse.TemplateNode:
		a.collect(n.Pipe, rootDot, aliases)
		bodyRoot, err := templateContext(n.Pipe, rootDot, aliases)
		if err != nil {
			a.err = err
			return
		}
		called := a.template.Lookup(n.Name)
		if called == nil || called.Tree == nil {
			a.err = fmt.Errorf("未定义模板 %q", n.Name)
			return
		}
		if a.active[n.Name] {
			a.err = fmt.Errorf("不支持递归模板 %q", n.Name)
			return
		}
		a.active[n.Name] = true
		a.collect(called.Root, bodyRoot, map[string]templateValue{"$": {known: true, root: bodyRoot}})
		delete(a.active, n.Name)
	}
}

func (a *templateAnalyzer) collectBranches(
	body, alternative *parse.ListNode,
	bodyRoot, elseRoot bool,
	aliases map[string]templateValue,
) {
	a.depth++
	a.collect(body, bodyRoot, maps.Clone(aliases))
	a.collect(alternative, elseRoot, maps.Clone(aliases))
	a.depth--
}

func (a *templateAnalyzer) collectCommand(
	command *parse.CommandNode,
	rootDot bool,
	aliases map[string]templateValue,
	previous *templateValue,
) {
	for _, arg := range command.Args {
		a.collect(arg, rootDot, aliases)
	}
	if a.err != nil || len(command.Args) == 0 {
		return
	}
	ident, ok := command.Args[0].(*parse.IdentifierNode)
	if !ok || ident.Ident != "index" {
		return
	}
	if len(command.Args) < 2 {
		a.err = fmt.Errorf("无法静态确定 index 对象")
		return
	}
	object := nodeValue(command.Args[1], rootDot, aliases)
	if !object.known {
		a.err = fmt.Errorf("无法静态确定 index 对象")
		return
	}
	if !object.root {
		return
	}
	var key templateValue
	if len(command.Args) > 2 {
		key = nodeValue(command.Args[2], rootDot, aliases)
	} else if previous != nil {
		key = *previous
	}
	if !key.isLiteral {
		a.err = fmt.Errorf("根对象的 index 键必须是可静态确定的字符串")
		return
	}
	a.params[key.literal] = true
}

// bindAliases 跟踪根别名和字面量；禁止跨分支修改外层变量。
func (a *templateAnalyzer) bindAliases(
	pipe *parse.PipeNode,
	rootDot bool,
	aliases map[string]templateValue,
	control bool,
) {
	if pipe == nil || len(pipe.Decl) == 0 {
		return
	}
	if pipe.IsAssign && (control || a.depth > 0) {
		a.err = fmt.Errorf("不支持控制结构中的模板变量赋值")
		return
	}
	if len(pipe.Decl) != 1 {
		a.err = fmt.Errorf("不支持多个模板变量声明")
		return
	}
	aliases[pipe.Decl[0].Ident[0]] = pipeValue(pipe, rootDot, aliases)
}

// nodeValue 只对可证明的根引用及字符串值作肯定判断。
func nodeValue(node parse.Node, rootDot bool, aliases map[string]templateValue) templateValue {
	switch n := node.(type) {
	case *parse.DotNode:
		return templateValue{known: true, root: rootDot}
	case *parse.VariableNode:
		value := aliases[n.Ident[0]]
		if len(n.Ident) > 1 && value.known {
			return templateValue{known: true}
		}
		return value
	case *parse.StringNode:
		return templateValue{known: true, literal: n.Text, isLiteral: true}
	case *parse.FieldNode, *parse.ChainNode, *parse.NumberNode, *parse.BoolNode, *parse.NilNode:
		return templateValue{known: true}
	case *parse.PipeNode:
		return pipeValue(n, rootDot, aliases)
	default:
		return templateValue{}
	}
}

func pipeValue(pipe *parse.PipeNode, rootDot bool, aliases map[string]templateValue) templateValue {
	if pipe == nil {
		return templateValue{known: true}
	}
	var previous *templateValue
	for _, command := range pipe.Cmds {
		value := commandValue(command, rootDot, aliases, previous)
		previous = &value
	}
	if previous == nil {
		return templateValue{}
	}
	return *previous
}

func commandValue(
	command *parse.CommandNode,
	rootDot bool,
	aliases map[string]templateValue,
	previous *templateValue,
) templateValue {
	if len(command.Args) == 1 && previous == nil {
		return nodeValue(command.Args[0], rootDot, aliases)
	}
	if len(command.Args) == 0 {
		return templateValue{}
	}
	ident, ok := command.Args[0].(*parse.IdentifierNode)
	if !ok {
		return templateValue{}
	}
	if ident.Ident == "index" && len(command.Args) >= 2 {
		if nodeValue(command.Args[1], rootDot, aliases).known {
			return templateValue{known: true}
		}
	}
	if ident.Ident == "or" || ident.Ident == "and" {
		values := command.Args[1:]
		if len(values) == 0 && previous == nil {
			return templateValue{}
		}
		for _, arg := range values {
			value := nodeValue(arg, rootDot, aliases)
			if !value.known || !value.root {
				return templateValue{}
			}
		}
		if previous != nil && (!previous.known || !previous.root) {
			return templateValue{}
		}
		return templateValue{known: true, root: true}
	}
	return templateValue{}
}

// templateContext 识别可证明的根对象或子值；未知上下文直接拒绝。
func templateContext(pipe *parse.PipeNode, rootDot bool, aliases map[string]templateValue) (bool, error) {
	value := pipeValue(pipe, rootDot, aliases)
	if !value.known {
		return false, fmt.Errorf("无法静态确定模板上下文")
	}
	return value.root, nil
}
