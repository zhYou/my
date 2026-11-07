package cli

import "encoding/json"

// machineSchema 显式描述 time.Time 的 RFC3339 JSON 编码，避免推导忽略自定义编码。
func machineSchema(kind string) json.RawMessage {
	text := map[string]any{schemaTypeKey: schemaStringType}
	timestamp := map[string]any{schemaTypeKey: schemaStringType, "format": "date-time"}
	record := map[string]any{
		schemaTypeKey:                 schemaObjectType,
		schemaAdditionalPropertiesKey: true,
		schemaRequiredKey: []string{
			"schema_version",
			"machine_id",
			flagKind,
			"os",
			"arch",
			flagNote,
			"registered_at",
			"status",
		},
		schemaPropertiesKey: map[string]any{
			"schema_version": map[string]any{"const": 1},
			"machine_id": map[string]any{
				schemaTypeKey: schemaStringType,
				"pattern":     "^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$",
			},
			flagKind:        map[string]any{schemaEnumKey: []string{"laptop", "desktop", "server", "vm"}},
			"os":            map[string]any{schemaEnumKey: []string{"linux", "darwin", "windows"}},
			"arch":          map[string]any{schemaEnumKey: []string{"amd64", "arm64"}},
			flagNote:        text,
			"registered_at": timestamp,
			"retired_at":    timestamp,
			"status":        map[string]any{schemaEnumKey: []string{"active", "retired"}},
		},
	}
	snapshot := map[string]any{
		schemaTypeKey:                 schemaObjectType,
		schemaAdditionalPropertiesKey: true,
		schemaRequiredKey:             []string{"repository", "repository_id", "branch", "commit"},
		schemaPropertiesKey: map[string]any{
			"repository":    text,
			"repository_id": map[string]any{schemaTypeKey: "integer", "minimum": 1},
			"branch":        text,
			"commit":        text,
		},
	}
	properties := map[string]any{"snapshot": snapshot}
	required := []string{"snapshot"}
	if kind == schemaList {
		properties["machines"] = map[string]any{schemaTypeKey: "array", "items": record}
		required = append(required, "machines")
	} else {
		properties["machine"] = record
		required = append(required, "machine")
	}
	if kind == schemaChange {
		properties["changed"] = map[string]any{schemaTypeKey: "boolean"}
		properties["bound"] = map[string]any{schemaTypeKey: "boolean"}
		required = append(required, "changed", "bound")
	}
	raw, err := json.Marshal(
		map[string]any{
			schemaTypeKey:                 schemaObjectType,
			schemaAdditionalPropertiesKey: true,
			schemaPropertiesKey:           properties,
			schemaRequiredKey:             required,
		},
	)
	if err != nil {
		panic(err)
	}
	return raw
}
