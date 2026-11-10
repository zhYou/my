package configuration

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// TOMLPatch 描述一个字段值的替换或删除；零值表示删除。
type TOMLPatch struct {
	Path    []string
	Value   UnitValue
	Comment string
}

type tomlNode struct {
	path       []string
	expression unstable.Range
	value      unstable.Range
}

type tomlTable struct {
	path       []string
	header     unstable.Range
	lastKeyEnd int
}

// TOMLValue 读取嵌套字段并返回规范值；字段不存在时返回零值。
func TOMLValue(data []byte, path []string) (UnitValue, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return UnitValue{}, invalidTOML(err)
	}
	var current any = document
	for _, component := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return UnitValue{}, nil
		}
		value, exists := object[component]
		if !exists {
			return UnitValue{}, nil
		}
		current = value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return UnitValue{}, invalidTOML(err)
	}
	return UnitValueFromBytes(encoded), nil
}

// PatchTOML 逐个修改字段，保留未替换字节、注释和排版。
func PatchTOML(data []byte, patches []TOMLPatch) ([]byte, error) {
	result := append([]byte(nil), data...)
	for _, patch := range patches {
		if len(patch.Path) == 0 {
			return nil, invalidInput()
		}
		var err error
		result, err = patchTOMLOne(result, patch)
		if err != nil {
			return nil, err
		}
	}
	var check map[string]any
	if err := toml.Unmarshal(result, &check); err != nil {
		return nil, invalidTOML(err)
	}
	return result, nil
}

// SortedTOMLKeys 返回稳定排序的 map 键，供目标读取使用。
func SortedTOMLKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func patchTOMLOne(data []byte, patch TOMLPatch) ([]byte, error) {
	nodes, tables, err := parseTOMLOutline(data)
	if err != nil {
		return nil, err
	}
	want := strings.Join(patch.Path, "\x00")
	for _, node := range nodes {
		if strings.Join(node.path, "\x00") != want {
			continue
		}
		if !patch.Value.Exists {
			start := lineStart(data, int(node.expression.Offset))
			end := lineEnd(data, int(node.expression.Offset+node.expression.Length))
			return replaceRange(data, start, end, nil), nil
		}
		value, err := encodeTOMLValueBytes(patch.Value)
		if err != nil {
			return nil, invalidTOML(err)
		}
		value = preserveSimpleStringQuotes(value, data[node.value.Offset:node.value.Offset+node.value.Length])
		return replaceRange(
			data,
			int(node.value.Offset),
			int(node.value.Offset+node.value.Length),
			value,
		), nil
	}
	if !patch.Value.Exists {
		return data, nil
	}
	value, err := encodeTOMLValueBytes(patch.Value)
	if err != nil {
		return nil, invalidTOML(err)
	}
	tablePath := patch.Path[:len(patch.Path)-1]
	key := tomlKey(patch.Path[len(patch.Path)-1])
	comment := ""
	if patch.Comment != "" {
		comment = "# " + strings.ReplaceAll(patch.Comment, "\n", " ") + "\n"
	}
	line := []byte(comment + key + " = " + string(value) + "\n")
	if len(tablePath) == 0 {
		offset := firstTableOffset(data)
		return replaceRange(data, offset, offset, line), nil
	}
	if table, ok := tables[strings.Join(tablePath, "\x00")]; ok {
		offset := table.lastKeyEnd
		if table.lastKeyEnd == 0 {
			offset = lineEnd(data, int(table.header.Offset+table.header.Length))
		}
		return replaceRange(data, offset, offset, line), nil
	}
	var block bytes.Buffer
	block.WriteByte('\n')
	block.WriteByte('[')
	block.WriteString(encodeTOMLPath(tablePath))
	block.WriteString("]\n")
	block.Write(line)
	offset := len(data)
	if offset > 0 && data[offset-1] != '\n' {
		offset = len(data)
	}
	return replaceRange(data, offset, offset, block.Bytes()), nil
}

func parseTOMLOutline(data []byte) ([]tomlNode, map[string]tomlTable, error) {
	parser := unstable.Parser{KeepComments: false}
	parser.Reset(data)
	var current []string
	var nodes []tomlNode
	tables := map[string]tomlTable{}
	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.Table, unstable.ArrayTable:
			current = nodeKey(expression)
			table := tomlTable{
				path:   append([]string(nil), current...),
				header: expression.Raw,
			}
			tables[strings.Join(current, "\x00")] = table
		case unstable.KeyValue:
			value := expression.Value()
			path := append(
				append([]string(nil), current...),
				nodeKey(expression)...,
			)
			nodes = append(nodes, tomlNode{
				path:       path,
				expression: expression.Raw,
				value:      value.Raw,
			})
			if len(current) == 0 {
				continue
			}
			table := tables[strings.Join(current, "\x00")]
			table.lastKeyEnd = lineEnd(data, int(expression.Raw.Offset+expression.Raw.Length))
			tables[strings.Join(current, "\x00")] = table
		}
	}
	if err := parser.Error(); err != nil {
		return nil, nil, invalidTOML(err)
	}
	return nodes, tables, nil
}

func nodeKey(node *unstable.Node) []string {
	var result []string
	iterator := node.Key()
	for iterator.Next() {
		result = append(result, string(iterator.Node().Data))
	}
	return result
}

func encodeTOMLPath(path []string) string {
	encoded := make([]string, len(path))
	for i, component := range path {
		encoded[i] = tomlKey(component)
	}
	return strings.Join(encoded, ".")
}

func tomlKey(value string) string {
	safe := value != ""
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		safe = false
		break
	}
	if safe {
		return value
	}
	return strconv.Quote(value)
}

func encodeTOMLValue(value any) ([]byte, error) {
	data, err := toml.Marshal(map[string]any{"value": value})
	if err != nil {
		return nil, err
	}
	_, encoded, found := bytes.Cut(data, []byte("= "))
	if !found {
		return nil, errors.New("missing TOML value separator")
	}
	return bytes.TrimSpace(encoded), nil
}

func encodeTOMLValueBytes(value UnitValue) ([]byte, error) {
	if !value.Exists {
		return nil, invalidInput()
	}
	if len(value.Data) == 0 {
		return nil, invalidInput()
	}
	var decoded any
	if err := json.Unmarshal(value.Data, &decoded); err != nil {
		return nil, err
	}
	return encodeTOMLValue(decoded)
}

func preserveSimpleStringQuotes(value, existing []byte) []byte {
	if len(value) < 2 || value[0] != '"' || len(existing) == 0 || existing[0] != '\'' {
		return value
	}
	var decoded string
	probe := append([]byte("value = "), value...)
	var document map[string]any
	if err := toml.Unmarshal(probe, &document); err != nil {
		return value
	}
	var ok bool
	if decoded, ok = document["value"].(string); !ok || strings.Contains(decoded, "'") {
		return value
	}
	return []byte("'" + decoded + "'")
}

func stringValue(value string) UnitValue {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return UnitValueFromBytes(data)
}

func boolValue(value bool) UnitValue {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return UnitValueFromBytes(data)
}

func lineStart(data []byte, offset int) int {
	if offset > len(data) {
		offset = len(data)
	}
	for offset > 0 && data[offset-1] != '\n' {
		offset--
	}
	return offset
}

func lineEnd(data []byte, offset int) int {
	if offset > len(data) {
		offset = len(data)
	}
	for offset < len(data) {
		if data[offset] == '\n' {
			return offset + 1
		}
		offset++
	}
	return len(data)
}

func firstTableOffset(data []byte) int {
	parser := unstable.Parser{}
	parser.Reset(data)
	for parser.NextExpression() {
		expression := parser.Expression()
		if expression.Kind == unstable.Table || expression.Kind == unstable.ArrayTable {
			return int(expression.Raw.Offset)
		}
	}
	return len(data)
}

func replaceRange(data []byte, start, end int, replacement []byte) []byte {
	result := make([]byte, 0, len(data)-(end-start)+len(replacement))
	result = append(result, data[:start]...)
	result = append(result, replacement...)
	result = append(result, data[end:]...)
	return result
}

func invalidTOML(cause error) error {
	return problem(codeStateInvalid, phaseValidate, cause)
}
