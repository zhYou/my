package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

// Change 描述两个机器契约之间的一项路径差异。
type Change struct {
	Path   string          `json:"path"`
	Kind   string          `json:"kind"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

// Record 记录机器契约变更的兼容性和迁移说明。
type Record struct {
	ID              string   `json:"id"`
	Compatibility   string   `json:"compatibility"`
	Summary         string   `json:"summary"`
	Paths           []string `json:"paths"`
	PreviousVersion string   `json:"previous_version"`
	TargetVersion   string   `json:"target_version"`
	Migration       string   `json:"migration"`
}

// Normalize 移除人类说明并规范化机器契约结构。
func Normalize(data []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return json.MarshalIndent(normalize(value), "", "  ")
}

// Diff 比较规范化契约，按路径返回差异。
func Diff(before, after []byte) ([]Change, error) {
	var a, b any
	if err := json.Unmarshal(before, &a); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(after, &b); err != nil {
		return nil, err
	}
	changes := []Change{}
	var visit func(string, any, any, bool, bool)
	visit = func(path string, old, current any, oldOK, newOK bool) {
		if oldOK && newOK && reflect.DeepEqual(old, current) {
			return
		}
		am, aMap := old.(map[string]any)
		bm, bMap := current.(map[string]any)
		if oldOK && newOK && aMap && bMap {
			keys := []string{}
			for k := range am {
				keys = append(keys, k)
			}
			for k := range bm {
				if _, ok := am[k]; !ok {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				av, ao := am[k]
				bv, bo := bm[k]
				visit(path+"/"+strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1"), av, bv, ao, bo)
			}
			return
		}
		kind := "changed"
		if !oldOK {
			kind = "added"
		} else if !newOK || strings.Contains(path, "/exit_codes/") || strings.HasSuffix(path, "/exit_codes") || strings.HasSuffix(path, "/type") || strings.HasSuffix(path, "/types") || strings.Contains(path, "/results/") || strings.HasSuffix(path, "/required") {
			kind = breakingChange
		}
		if !oldOK {
			if strings.HasSuffix(path, "/required") {
				kind = breakingChange
			}
			if object, ok := current.(map[string]any); ok && object["required"] == true {
				kind = breakingChange
			}
		}
		//nolint:errcheck // old/current 仅来自成功 json.Unmarshal 的树，节点再次编码不会失败。
		prev, _ := json.Marshal(old)
		//nolint:errcheck // current 仅来自成功 json.Unmarshal 的树，不包含自定义编码器或非法数值。
		next, _ := json.Marshal(current)
		changes = append(changes, Change{path, kind, prev, next})
	}
	visit("", a, b, true, true)
	return changes, nil
}

// ValidateRecords 检查每项契约差异是否有有效的兼容变更记录。
func ValidateRecords(changes []Change, records []Record, initial bool) error {
	if initial {
		found := false
		for _, r := range records {
			if r.PreviousVersion == "" && r.ID != "" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("首次建立机器契约须提供 previous_version 为 null 的初始化记录")
		}
	}
	for _, change := range changes {
		covered := false
		for _, r := range records {
			if r.ID == "" || r.Summary == "" || r.Migration == "" || !semver.IsValid(r.TargetVersion) {
				return fmt.Errorf("兼容变更记录字段不完整：%s", r.ID)
			}
			if r.Compatibility != "compatible" && r.Compatibility != breakingCompatibility &&
				r.Compatibility != "review-required" {
				return fmt.Errorf("兼容性声明无效：%s", r.ID)
			}
			if r.PreviousVersion != "" &&
				(!semver.IsValid(r.PreviousVersion) || semver.Compare(r.TargetVersion, r.PreviousVersion) <= 0) {
				return fmt.Errorf("目标版本必须高于有效的旧版本：%s", r.ID)
			}
			matches := slices.ContainsFunc(r.Paths, func(path string) bool {
				return path == change.Path || strings.HasPrefix(change.Path, strings.TrimSuffix(path, "/")+"/")
			})
			if !matches {
				continue
			}
			if change.Kind == breakingChange {
				if r.Compatibility != breakingCompatibility {
					return fmt.Errorf("潜在破坏变更 %s 不能声明为兼容", change.Path)
				}
				if r.PreviousVersion == "" {
					return fmt.Errorf("破坏变更须指定旧版本：%s", r.ID)
				}
				oldMajor := semver.Major(r.PreviousVersion)
				if oldMajor == "v0" {
					if semver.MajorMinor(r.TargetVersion) == semver.MajorMinor(r.PreviousVersion) {
						return fmt.Errorf("0.x 破坏变更须提升次版本")
					}
				} else if semver.Major(r.TargetVersion) == oldMajor {
					return fmt.Errorf("破坏变更须提升主版本")
				}
			}
			if change.Kind == "changed" && r.Compatibility == "compatible" {
				return fmt.Errorf("复杂变化 %s 须声明 review-required 或 breaking", change.Path)
			}
			covered = true
		}
		if !covered {
			return fmt.Errorf("机器契约变化缺少新变更记录：%s（%s）", change.Path, change.Kind)
		}
	}
	return nil
}

// normalize 只在命令元数据与 Schema 注解的语境中移除人类说明。
func normalize(value any) any {
	root, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if schema, ok := root["schema"].(map[string]any); ok {
		root["schema"] = normalizeCommand(schema)
	} else if _, ok := root["commands"]; ok {
		return normalizeCommand(root)
	}
	return root
}

func normalizeCommand(command map[string]any) any {
	out := map[string]any{}
	for key, value := range command {
		switch key {
		case descriptionField, "examples", "use":
			continue
		case "commands":
			out[key] = normalizeNamed(value, "path", normalizeCommand)
		case "flags":
			out[key] = normalizeNamed(value, "name", func(flag map[string]any) any {
				entry := map[string]any{}
				for key, value := range flag {
					if key != descriptionField {
						entry[key] = value
					}
				}
				return entry
			})
		case "result_schema":
			out[key] = normalizeSchema(value)
		default:
			out[key] = value
		}
	}
	return out
}

func normalizeNamed(value any, key string, visit func(map[string]any) any) any {
	switch items := value.(type) {
	case []any:
		if len(items) == 0 {
			return items
		}
		out := map[string]any{}
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				return value
			}
			name, ok := object[key].(string)
			if !ok {
				return value
			}
			if _, exists := out[name]; exists {
				return value
			}
			out[name] = visit(object)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for name, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				return value
			}
			out[name] = visit(object)
		}
		return out
	}
	return value
}

func normalizeSchema(value any) any {
	schema, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := map[string]any{}
	for key, value := range schema {
		switch key {
		case descriptionField, "examples":
			continue
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies":
			if children, ok := value.(map[string]any); ok {
				mapped := map[string]any{}
				for name, child := range children {
					mapped[name] = normalizeSchema(child)
				}
				out[key] = mapped
			} else {
				out[key] = value
			}
		case "additionalProperties",
			"unevaluatedProperties",
			"propertyNames",
			"items",
			"contains",
			"if",
			"then",
			"else",
			"not",
			"contentSchema",
			"additionalItems",
			"unevaluatedItems":
			out[key] = normalizeSchema(value)
		case "prefixItems", "allOf", "anyOf", "oneOf":
			if children, ok := value.([]any); ok {
				mapped := make([]any, len(children))
				for index, child := range children {
					mapped[index] = normalizeSchema(child)
				}
				out[key] = mapped
			} else {
				out[key] = value
			}
		default:
			out[key] = value
		}
	}
	return out
}
