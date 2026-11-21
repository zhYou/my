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
	codexRulesDirectory = ".codex/rules"
	codexRuleExtension  = ".rules"
	prefixRuleCall      = "prefix_rule"
	patternField        = "pattern"
	decisionField       = "decision"
	justificationField  = "justification"
	matchField          = "match"
	notMatchField       = "not_match"
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

// checkCodexRules 校验项目 Codex 规则文件的 prefix_rule 语法，
// 并要求每个文件至少包含一条面向用户的中文理由。
func checkCodexRules(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf(resolveRepositoryError, err)
	}
	directory := filepath.Join(root, filepath.FromSlash(codexRulesDirectory))
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf(readPathError, codexRulesDirectory, err)
	}
	found := false
	var problems []error
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != codexRuleExtension {
			continue
		}
		found = true
		path := filepath.Join(directory, entry.Name())
		if err := checkRegularFile(path); err != nil {
			problems = append(problems, err)
			continue
		}
		if err := checkCodexRuleFile(path); err != nil {
			problems = append(problems, err)
		}
	}
	if !found {
		problems = append(problems, fmt.Errorf("%s 缺少 %s 文件", codexRulesDirectory, codexRuleExtension))
	}
	return errors.Join(problems...)
}

func checkCodexRuleFile(path string) error {
	//nolint:gosec // G304：只读取调用者指定仓库中的固定规则目录。
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf(readPathError, path, err)
	}
	tokens, err := lexCodexRule(path, string(data))
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return fmt.Errorf("%s 缺少 justification", path)
	}
	count := 0
	var problems []error
	for i := 0; i < len(tokens); {
		end, arguments, err := checkRuleCall(path, tokens, i)
		if err != nil {
			problems = append(problems, err)
			return errors.Join(problems...)
		}
		problems = append(problems, arguments.problems...)
		for _, value := range arguments.justifications {
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

// checkRuleCall 校验一个 prefix_rule 调用，返回调用结束位置和参数解析结果；
// 顶层结构错误直接返回。
func checkRuleCall(path string, tokens []ruleToken, start int) (int, ruleArguments, error) {
	if tokens[start].kind != ruleIdentifier || tokens[start].text != prefixRuleCall {
		return 0, ruleArguments{}, fmt.Errorf("%s:%d 顶层只能包含 prefix_rule 调用", path, tokens[start].line)
	}
	if start+1 >= len(tokens) || !isRulePunctuation(tokens[start+1], "(") {
		return 0, ruleArguments{}, fmt.Errorf("%s:%d prefix_rule 缺少左括号", path, tokens[start].line)
	}
	end, err := matchingRuleDelimiter(path, tokens, start+1)
	if err != nil {
		return 0, ruleArguments{}, err
	}
	return end, parseRuleArguments(path, tokens[start].line, tokens[start+2:end]), nil
}

func lexCodexRule(path, source string) ([]ruleToken, error) {
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
		if r == '"' || r == '\'' {
			quote := r
			offset += size
			escaped := false
			closed := false
			for offset < len(source) {
				next, nextSize := utf8.DecodeRuneInString(source[offset:])
				if next == '\n' {
					break
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
				if next == quote {
					closed = true
					break
				}
			}
			if !closed {
				return nil, fmt.Errorf("%s:%d 规则字符串无效：字符串未闭合", path, tokenLine)
			}
			value, err := unquoteRuleString(source[start:offset])
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

func unquoteRuleString(raw string) (string, error) {
	if len(raw) < 2 {
		return "", errors.New("字符串未闭合")
	}
	quote := raw[0]
	if raw[len(raw)-1] != quote {
		return "", errors.New("字符串未闭合")
	}
	var value strings.Builder
	for i := 1; i < len(raw)-1; {
		r, size := utf8.DecodeRuneInString(raw[i:])
		if r != '\\' {
			value.WriteString(raw[i : i+size])
			i += size
			continue
		}
		i += size
		if i >= len(raw)-1 {
			return "", errors.New("转义序列不完整")
		}
		escape, escapeSize := utf8.DecodeRuneInString(raw[i:])
		i += escapeSize
		switch escape {
		case 'a':
			value.WriteByte('\a')
		case 'b':
			value.WriteByte('\b')
		case 'f':
			value.WriteByte('\f')
		case 'n':
			value.WriteByte('\n')
		case 'r':
			value.WriteByte('\r')
		case 't':
			value.WriteByte('\t')
		case 'v':
			value.WriteByte('\v')
		case '\\', '\'', '"':
			value.WriteRune(escape)
		case 'x', 'u', 'U':
			digits := 2
			switch escape {
			case 'u':
				digits = 4
			case 'U':
				digits = 8
			}
			if i+digits > len(raw)-1 {
				return "", fmt.Errorf("转义序列 \\%c 不完整", escape)
			}
			code, err := strconv.ParseUint(raw[i:i+digits], 16, 32)
			if err != nil {
				return "", fmt.Errorf("转义序列 \\%c 无效：%w", escape, err)
			}
			if code > utf8.MaxRune || (code >= 0xD800 && code <= 0xDFFF) {
				return "", fmt.Errorf("转义序列 \\%c 超出有效 Unicode 范围", escape)
			}
			value.WriteRune(rune(code))
			i += digits
		default:
			return "", fmt.Errorf("不支持的转义 \\%c", escape)
		}
	}
	return value.String(), nil
}

var ruleDelimiterPairs = map[string]string{")": "(", "]": "[", "}": "{"}

// isRulePunctuation 判断 token 是否为指定的结构标点；字符串内容永远不参与结构判断。
func isRulePunctuation(token ruleToken, text string) bool {
	return token.kind == rulePunctuation && token.text == text
}

// ruleDelimiterDelta 返回结构标点对括号深度的变化；字符串和其他标点始终为 0。
func ruleDelimiterDelta(token ruleToken) int {
	if token.kind != rulePunctuation {
		return 0
	}
	switch token.text {
	case "(", "[", "{":
		return 1
	case ")", "]", "}":
		return -1
	default:
		return 0
	}
}

func matchingRuleDelimiter(path string, tokens []ruleToken, start int) (int, error) {
	var stack []ruleToken
	for i := start; i < len(tokens); i++ {
		switch ruleDelimiterDelta(tokens[i]) {
		case 1:
			stack = append(stack, tokens[i])
		case -1:
			if len(stack) == 0 || stack[len(stack)-1].text != ruleDelimiterPairs[tokens[i].text] {
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

// ruleArguments 是 prefix_rule 参数解析结果：justifications 是待校验的理由字符串，
// problems 是字段、取值或结构错误。
type ruleArguments struct {
	justifications []ruleToken
	problems       []error
}

// parseRuleArguments 解析 prefix_rule 括号内的参数，挑出所有 justification 字符串；
// 参数结构和 pattern、decision 的取值错误写入 problems。示例命令的语义不在本检查范围内。
func parseRuleArguments(path string, callLine int, tokens []ruleToken) ruleArguments {
	var justifications []ruleToken
	var problems []error
	seen := make(map[string]struct{})
	for i := 0; i < len(tokens); {
		if isRulePunctuation(tokens[i], ",") {
			i++
			continue
		}
		if tokens[i].kind != ruleIdentifier {
			problems = append(problems, fmt.Errorf("%s:%d 规则参数无效", path, tokens[i].line))
			return ruleArguments{justifications: justifications, problems: problems}
		}
		field := tokens[i]
		i++
		if i >= len(tokens) || !isRulePunctuation(tokens[i], "=") {
			problems = append(problems, fmt.Errorf("%s:%d %s 缺少等号", path, field.line, field.text))
			return ruleArguments{justifications: justifications, problems: problems}
		}
		i++
		valueStart := i
		depth := 0
		for i < len(tokens) {
			if isRulePunctuation(tokens[i], ",") && depth == 0 {
				break
			}
			depth += ruleDelimiterDelta(tokens[i])
			i++
		}
		value := tokens[valueStart:i]
		if _, ok := seen[field.text]; ok {
			problems = append(problems, fmt.Errorf("%s:%d 重复的规则字段 %s", path, field.line, field.text))
		} else {
			seen[field.text] = struct{}{}
		}
		if len(value) == 0 {
			problems = append(problems, fmt.Errorf("%s:%d %s 缺少值", path, field.line, field.text))
			continue
		}
		switch field.text {
		case patternField:
			problems = append(problems, inspectRulePattern(path, field, value)...)
		case decisionField:
			problems = append(problems, inspectRuleDecision(path, field, value)...)
		case justificationField:
			if len(value) != 1 || value[0].kind != ruleString {
				problems = append(problems, fmt.Errorf("%s:%d justification 无效", path, field.line))
			} else {
				justifications = append(justifications, value[0])
			}
		case matchField, notMatchField:
			// 示例命令的语义不在本检查范围内。
		default:
			problems = append(problems, fmt.Errorf("%s:%d 未知的规则字段 %s", path, field.line, field.text))
		}
		if i < len(tokens) && isRulePunctuation(tokens[i], ",") {
			i++
		}
	}
	if _, ok := seen[patternField]; !ok {
		problems = append(problems, fmt.Errorf("%s:%d prefix_rule 缺少 pattern", path, callLine))
	}
	return ruleArguments{justifications: justifications, problems: problems}
}

func inspectRulePattern(path string, field ruleToken, value []ruleToken) []error {
	if len(value) < 2 || !isRulePunctuation(value[0], "[") || !isRulePunctuation(value[len(value)-1], "]") {
		return []error{fmt.Errorf("%s:%d pattern 必须是列表", path, field.line)}
	}
	elements := splitRuleElements(value[1 : len(value)-1])
	if len(elements) == 0 {
		return []error{fmt.Errorf("%s:%d pattern 不能为空列表", path, field.line)}
	}
	var problems []error
	for _, element := range elements {
		if len(element) == 1 && element[0].kind == ruleString {
			continue
		}
		if len(element) >= 2 && isRulePunctuation(element[0], "[") &&
			isRulePunctuation(element[len(element)-1], "]") {
			group := splitRuleElements(element[1 : len(element)-1])
			if len(group) > 0 && ruleElementsAreStrings(group) {
				continue
			}
		}
		problems = append(problems, fmt.Errorf("%s:%d pattern 元素必须是字符串或非空字符串列表", path, field.line))
	}
	return problems
}

func ruleElementsAreStrings(elements [][]ruleToken) bool {
	for _, element := range elements {
		if len(element) != 1 || element[0].kind != ruleString {
			return false
		}
	}
	return true
}

func splitRuleElements(tokens []ruleToken) [][]ruleToken {
	var elements [][]ruleToken
	var current []ruleToken
	depth := 0
	for _, token := range tokens {
		depth += ruleDelimiterDelta(token)
		if isRulePunctuation(token, ",") && depth == 0 {
			elements = append(elements, current)
			current = nil
			continue
		}
		current = append(current, token)
	}
	if len(current) > 0 {
		elements = append(elements, current)
	}
	return elements
}

func inspectRuleDecision(path string, field ruleToken, value []ruleToken) []error {
	if len(value) == 1 && value[0].kind == ruleString {
		switch value[0].text {
		case "allow", "prompt", "forbidden":
			return nil
		}
	}
	return []error{fmt.Errorf("%s:%d decision 必须是 allow、prompt 或 forbidden", path, field.line)}
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
