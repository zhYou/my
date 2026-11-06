package schema

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Draft 是公开结果结构采用的 JSON Schema 方言。
const Draft = "https://json-schema.org/draft/2020-12/schema"

// FromType 从 Go 类型推导结果结构；显式覆盖用于特殊编码，禁止加载外部引用。
func FromType(t reflect.Type, override json.RawMessage) (json.RawMessage, error) {
	var schema *jsonschema.Schema
	if len(override) > 0 {
		schema = &jsonschema.Schema{}
		if err := json.Unmarshal(override, schema); err != nil {
			return nil, fmt.Errorf("解析结果结构覆盖失败：%w", err)
		}
	} else {
		if t == nil {
			return nil, fmt.Errorf("结果类型不能为空")
		}
		if err := checkEncoding(t, map[reflect.Type]bool{}); err != nil {
			return nil, err
		}
		var err error
		schema, err = jsonschema.ForType(t, nil)
		if err != nil {
			return nil, fmt.Errorf("推导结果结构失败，请提供显式覆盖：%w", err)
		}
		if err := openStructs(schema); err != nil {
			return nil, fmt.Errorf("开放结果结构失败：%w", err)
		}
	}
	schema.Schema = Draft
	if _, err := schema.Resolve(nil); err != nil {
		return nil, fmt.Errorf("结果结构无效：%w", err)
	}
	return json.Marshal(schema)
}

func checkEncoding(t reflect.Type, seen map[reflect.Type]bool) error {
	if seen[t] {
		return nil
	}
	seen[t] = true
	marshaler := reflect.TypeFor[json.Marshaler]()
	textMarshaler := reflect.TypeFor[encoding.TextMarshaler]()
	if t.Implements(marshaler) || reflect.PointerTo(t).Implements(marshaler) || t.Implements(textMarshaler) ||
		reflect.PointerTo(t).Implements(textMarshaler) {
		return fmt.Errorf("类型 %s 使用自定义 JSON 编码，需要显式结果结构", t)
	}
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return fmt.Errorf("字节切片使用 base64 JSON 编码，需要显式结果结构")
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return checkEncoding(t.Elem(), seen)
	case reflect.Struct:
		for field := range t.Fields() {
			if field.IsExported() && strings.Split(field.Tag.Get("json"), ",")[0] != "-" {
				if slices.Contains(strings.Split(field.Tag.Get("json"), ","), "string") {
					return fmt.Errorf("字段 %s 使用 JSON string 编码选项，需要显式结果结构", field.Name)
				}
				if err := checkEncoding(field.Type, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func openStructs(s *jsonschema.Schema) error {
	if s == nil {
		return nil
	}
	if s.AdditionalProperties != nil {
		raw, err := json.Marshal(s.AdditionalProperties)
		if err != nil {
			return err
		}
		if string(raw) == "false" {
			s.AdditionalProperties = &jsonschema.Schema{}
		} else {
			if s.Type == "object" {
				s.Type = ""
				s.Types = []string{"object", "null"}
			}
			if err := openStructs(s.AdditionalProperties); err != nil {
				return err
			}
		}
	}
	for _, property := range s.Properties {
		if err := openStructs(property); err != nil {
			return err
		}
	}
	for _, def := range s.Defs {
		if err := openStructs(def); err != nil {
			return err
		}
	}
	if err := openStructs(s.Items); err != nil {
		return err
	}
	for _, sub := range s.AnyOf {
		if err := openStructs(sub); err != nil {
			return err
		}
	}
	return nil
}
