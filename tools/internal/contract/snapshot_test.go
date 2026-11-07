package contract

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizationPreservesBusinessFieldNames(t *testing.T) {
	for _, name := range []string{"description", "examples", "use"} {
		t.Run(name, func(t *testing.T) {
			document := func(kind string) []byte {
				value := map[string]any{
					"schema": map[string]any{"commands": []any{map[string]any{
						"path":        "contract-cli greet",
						"description": "中文命令说明",
						"flags":       []any{},
						"commands":    []any{},
						"result_schema": map[string]any{
							"type":       "object",
							"properties": map[string]any{name: map[string]any{"type": kind, "description": "字段说明"}},
						},
					}}},
					"results": map[string]any{"greet": map[string]any{name: kind}},
				}
				raw, err := json.Marshal(value)
				require.NoError(t, err)
				return raw
			}
			before, err := Normalize(document("string"))
			require.NoError(t, err)
			after, err := Normalize(document("number"))
			require.NoError(t, err)
			var doc map[string]any
			require.NoError(t, json.Unmarshal(before, &doc))
			results, ok := doc["results"].(map[string]any)
			require.True(t, ok)
			greet, ok := results["greet"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "string", greet[name])
			schema, ok := doc["schema"].(map[string]any)
			require.True(t, ok)
			commands, ok := schema["commands"].(map[string]any)
			require.True(t, ok)
			command, ok := commands["contract-cli greet"].(map[string]any)
			require.True(t, ok)
			require.NotContains(t, command, "description")
			resultSchema, ok := command["result_schema"].(map[string]any)
			require.True(t, ok)
			fields, ok := resultSchema["properties"].(map[string]any)
			require.True(t, ok)
			require.Contains(t, fields, name)
			field, ok := fields[name].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "string", field["type"])
			changes, err := Diff(before, after)
			require.NoError(t, err)
			require.Len(t, changes, 2)
			for _, change := range changes {
				require.Equal(t, "breaking", change.Kind)
			}
		})
	}
}

func TestContractNormalization(t *testing.T) {
	a := []byte(
		`{"commands":[{"path":"contract-cli greet","description":"旧文案","flags":[{"name":"name","type":"string","required":true,"description":"姓名"}]}]}`,
	)
	b := []byte(
		`{"commands":[{"path":"contract-cli greet","description":"新文案","flags":[{"name":"name","type":"string","required":true,"description":"输入"}]}]}`,
	)
	x, err := Normalize(a)
	require.NoError(t, err)
	y, err := Normalize(b)
	require.NoError(t, err)
	require.JSONEq(t, string(x), string(y))
	changes, err := Diff(
		x,
		[]byte(
			`{"commands":{"contract-cli greet":{"path":"contract-cli greet","flags":{"name":{"name":"name","type":"number","required":true}}}}}`,
		),
	)
	require.NoError(t, err)
	require.NotEmpty(t, changes)
	require.Equal(t, "breaking", changes[0].Kind)
}

func TestExitCodeChangeRequiresBreakingVersion(t *testing.T) {
	changes, err := Diff(
		[]byte(`{"schema":{"exit_codes":{"runtime":1}}}`),
		[]byte(`{"schema":{"exit_codes":{"runtime":3}}}`),
	)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	record := Record{
		ID:              "exit-code",
		Compatibility:   "review-required",
		Summary:         "修改运行退出码",
		Paths:           []string{"/schema/exit_codes/runtime"},
		PreviousVersion: "v1.0.0",
		TargetVersion:   "v1.0.1",
		Migration:       "更新调用者",
	}
	require.Error(t, ValidateRecords(changes, []Record{record}, false))
	record.Compatibility = "breaking"
	require.Error(t, ValidateRecords(changes, []Record{record}, false))
	record.TargetVersion = "v2.0.0"
	require.NoError(t, ValidateRecords(changes, []Record{record}, false))
}

func TestDiagnosticSchemaAnnotationsDoNotChangeContract(t *testing.T) {
	before, err := Normalize(
		[]byte(
			`{"schema":{"diagnostic_schema":{"type":"object","description":"English","properties":{"description":{"type":"string"},"message":{"type":"string","description":"before"}}}}}`,
		),
	)
	require.NoError(t, err)
	after, err := Normalize(
		[]byte(
			`{"schema":{"diagnostic_schema":{"type":"object","description":"中文","properties":{"description":{"type":"string"},"message":{"type":"string","description":"after"}}}}}`,
		),
	)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	var document struct {
		Schema struct {
			Diagnostic struct{ Properties map[string]any } `json:"diagnostic_schema"`
		}
	}
	require.NoError(t, json.Unmarshal(after, &document))
	require.Contains(t, document.Schema.Diagnostic.Properties, "description")
}
