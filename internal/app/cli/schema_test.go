package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/greeting"
)

func TestSchemaReflectsRegisteredCommands(t *testing.T) {
	var out bytes.Buffer
	root := metadataRoot(Options{Name: "testcli", Out: &out})
	extra := &cobra.Command{Use: "extra", Short: "新增的命令"}
	extra.Flags().String("token", "", "示例选项")
	if err := extra.MarkFlagRequired("token"); err != nil {
		t.Fatal(err)
	}
	root.AddCommand(extra)
	root.SetArgs([]string{"schema", "extra"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		SchemaVersion int `json:"schema_version"`
		Commands      []struct {
			Path  string `json:"path"`
			Flags []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"flags"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || len(got.Commands) != 1 || got.Commands[0].Path != "testcli extra" {
		t.Fatalf("schema=%s", out.String())
	}
	found := false
	for _, flag := range got.Commands[0].Flags {
		if flag.Name == "token" && flag.Required {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少必填选项标记：%s", out.String())
	}
}

func TestSchemaGenerationDoesNotExecuteCommands(t *testing.T) {
	count := 0
	deps := Dependencies{
		LoadConfig: func(context.Context, config.Options) (config.Config, error) {
			count++
			return config.Config{}, nil
		},
		Greet: func(context.Context, greeting.Options) (greeting.Result, error) {
			count++
			return greeting.Result{}, nil
		},
	}
	var out bytes.Buffer
	require.Zero(t, Run(context.Background(), []string{"schema", "greet"}, Options{Out: &out, Dependencies: &deps}))
	var doc struct {
		Commands []struct {
			Result json.RawMessage `json:"result_schema"`
		} `json:"commands"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &doc))
	require.Len(t, doc.Commands, 1)
	require.NotEmpty(t, doc.Commands[0].Result)
	require.Zero(t, count)
}

func TestDiagnosticSchemaMatchesActualErrorAndWarnings(t *testing.T) {
	code, out, stderr := invoke(t, "schema", "greet")
	require.Zero(t, code, stderr)
	var document struct {
		Diagnostic json.RawMessage `json:"diagnostic_schema"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &document))
	require.NotEmpty(t, document.Diagnostic)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(document.Diagnostic, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "state"), []byte("blocked"), 0o600))
	var stdout, diagnostics bytes.Buffer
	code = Run(
		t.Context(),
		[]string{"--json", "greet", "--name", "fixture"},
		Options{
			Out:       &stdout,
			ErrOut:    &diagnostics,
			LookupEnv: func(key string) (string, bool) { return home, key == "MY_HOME" },
			Dependencies: &Dependencies{Greet: func(context.Context, greeting.Options) (greeting.Result, error) {
				return greeting.Result{}, errors.New("fixture failure")
			}},
		},
	)
	require.Equal(t, 1, code)
	require.Empty(t, stdout.String())
	lines := bytes.Split(bytes.TrimSpace(diagnostics.Bytes()), []byte("\n"))
	require.GreaterOrEqual(t, len(lines), 2)
	for i, line := range lines {
		var value map[string]any
		require.NoError(t, json.Unmarshal(line, &value))
		require.NoError(t, resolved.Validate(value), string(line))
		if i == len(lines)-1 {
			require.Equal(t, "error", value["type"])
		} else {
			require.Equal(t, "warning", value["type"])
		}
	}
	for _, invalid := range []string{`{"type":"error","schema_version":1,"diagnostics":[]}`, `{"type":"warning","schema_version":1,"exit_code":1,"diagnostics":[]}`, `{"type":"other","schema_version":1,"diagnostics":[]}`} {
		var value any
		require.NoError(t, json.Unmarshal([]byte(invalid), &value))
		require.Error(t, resolved.Validate(value))
	}
}
