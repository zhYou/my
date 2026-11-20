package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	agentRulesDirectory = ".codex/rules"
	agentRuleExtension  = ".rules"
	prefixRuleCall      = "prefix_rule"
	justificationField  = "justification"
)

type ruleTokenKind uint8

const (
	ruleIdentifier ruleTokenKind = iota
	ruleString
	rulePunctuation
)

type ruleToken struct {
	kind ruleTokenKind
	text string
	line int
}

// checkAgentRules 要求每个项目规则文件至少包含一条面向用户的中文理由。
func checkAgentRules(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf(resolveRepositoryError, err)
	}
	directory := filepath.Join(root, filepath.FromSlash(agentRulesDirectory))
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("读取 %s：%w", agentRulesDirectory, err)
	}
	found := false
	var problems []error
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != agentRuleExtension {
			continue
		}
		found = true
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			problems = append(problems, fmt.Errorf("检查 %s：%w", entry.Name(), err))
			continue
		}
		if !info.Mode().IsRegular() {
			problems = append(problems, fmt.Errorf(notRegularError, entry.Name()))
			continue
		}
		if err := checkAgentRuleFile(path); err != nil {
			problems = append(problems, err)
		}
	}
	if !found {
		problems = append(problems, fmt.Errorf("%s 缺少 %s 文件", agentRulesDirectory, agentRuleExtension))
	}
	return errors.Join(problems...)
}

func checkAgentRuleFile(path string) error {
	//nolint:gosec // G304：只读取调用者指定仓库中的固定规则目录。
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf(readPathError, path, err)
	}
	tokens, err := lexAgentRule(path, string(data))
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return fmt.Errorf("%s 缺少 justification", path)
	}
	count := 0
	var problems []error
	for i := 0; i < len(tokens); {
		if tokens[i].kind != ruleIdentifier || tokens[i].text != prefixRuleCall {
			problems = append(problems, fmt.Errorf("%s:%d 顶层只能包含 prefix_rule 调用", path, tokens[i].line))
			return errors.Join(problems...)
		}
		if i+1 >= len(tokens) || tokens[i+1].text != "(" {
			problems = append(problems, fmt.Errorf("%s:%d prefix_rule 缺少左括号", path, tokens[i].line))
			return errors.Join(problems...)
		}
		end, err := matchingRuleDelimiter(path, tokens, i+1)
		if err != nil {
			problems = append(problems, err)
			return errors.Join(problems...)
		}
		callJustifications, callProblems := inspectRuleCall(path, tokens[i+2:end])
		problems = append(problems, callProblems...)
		for _, value := range callJustifications {
			count++
			if !containsHan(value.text) {
				problems = append(problems, fmt.Errorf("%s:%d justification 必须使用中文", path, value.line))
			}
		}
		i = end + 1
	}
	if count == 0 {
		problems = append(problems, fmt.Errorf("%s 缺少 justification", path))
	}
	return errors.Join(problems...)
}

func lexAgentRule(path, source string) ([]ruleToken, error) {
	var tokens []ruleToken
	line := 1
	for offset := 0; offset < len(source); {
		r, size := utf8.DecodeRuneInString(source[offset:])
		if r == utf8.RuneError && size == 1 {
			return nil, fmt.Errorf("%s:%d 规则文本无效", path, line)
		}
		if unicode.IsSpace(r) {
			if r == '\n' {
				line++
			}
			offset += size
			continue
		}
		if r == '#' {
			for offset < len(source) && source[offset] != '\n' {
				offset++
			}
			continue
		}
		start := offset
		tokenLine := line
		if unicode.IsLetter(r) || r == '_' {
			offset += size
			for offset < len(source) {
				next, nextSize := utf8.DecodeRuneInString(source[offset:])
				if !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' {
					break
				}
				offset += nextSize
			}
			tokens = append(tokens, ruleToken{kind: ruleIdentifier, text: source[start:offset], line: tokenLine})
			continue
		}
		if r == '"' {
			offset += size
			escaped := false
			for offset < len(source) {
				next, nextSize := utf8.DecodeRuneInString(source[offset:])
				if next == '\n' {
					line++
				}
				offset += nextSize
				if escaped {
					escaped = false
					continue
				}
				if next == '\\' {
					escaped = true
					continue
				}
				if next == '"' {
					break
				}
			}
			if offset > len(source) || source[offset-1] != '"' {
				return nil, fmt.Errorf("%s:%d 规则字符串无效：字符串未闭合", path, tokenLine)
			}
			value, err := strconv.Unquote(source[start:offset])
			if err != nil {
				return nil, fmt.Errorf("%s:%d 规则字符串无效：%w", path, tokenLine, err)
			}
			tokens = append(tokens, ruleToken{kind: ruleString, text: value, line: tokenLine})
			continue
		}
		if strings.ContainsRune("()[]{},=", r) {
			tokens = append(tokens, ruleToken{kind: rulePunctuation, text: string(r), line: tokenLine})
			offset += size
			continue
		}
		return nil, fmt.Errorf("%s:%d 规则文本无效：无法识别 %q", path, line, r)
	}
	return tokens, nil
}

func matchingRuleDelimiter(path string, tokens []ruleToken, start int) (int, error) {
	pairs := map[string]string{")": "(", "]": "[", "}": "{"}
	var stack []ruleToken
	for i := start; i < len(tokens); i++ {
		switch tokens[i].text {
		case "(", "[", "{":
			stack = append(stack, tokens[i])
		case ")", "]", "}":
			if len(stack) == 0 || stack[len(stack)-1].text != pairs[tokens[i].text] {
				return 0, fmt.Errorf("%s:%d 括号不匹配", path, tokens[i].line)
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("%s:%d 括号未闭合", path, tokens[start].line)
}

func inspectRuleCall(path string, tokens []ruleToken) ([]ruleToken, []error) {
	var justifications []ruleToken
	var problems []error
	for i := 0; i < len(tokens); {
		if tokens[i].text == "," {
			i++
			continue
		}
		if tokens[i].kind != ruleIdentifier {
			problems = append(problems, fmt.Errorf("%s:%d 规则参数无效", path, tokens[i].line))
			return justifications, problems
		}
		field := tokens[i]
		i++
		if i >= len(tokens) || tokens[i].text != "=" {
			problems = append(problems, fmt.Errorf("%s:%d %s 缺少等号", path, field.line, field.text))
			return justifications, problems
		}
		i++
		valueStart := i
		depth := 0
		for i < len(tokens) {
			switch tokens[i].text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			case ",":
				if depth == 0 {
					goto valueDone
				}
			}
			i++
		}
	valueDone:
		if valueStart == i {
			problems = append(problems, fmt.Errorf("%s:%d %s 缺少值", path, field.line, field.text))
			continue
		}
		if field.text == justificationField {
			if i != valueStart+1 || tokens[valueStart].kind != ruleString {
				problems = append(problems, fmt.Errorf("%s:%d justification 无效", path, field.line))
			} else {
				justifications = append(justifications, tokens[valueStart])
			}
		}
		if i < len(tokens) && tokens[i].text == "," {
			i++
		}
	}
	return justifications, problems
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
