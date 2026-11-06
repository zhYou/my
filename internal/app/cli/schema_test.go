package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

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
