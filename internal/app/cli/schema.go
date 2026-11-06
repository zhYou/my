package cli

import (
	"encoding/json"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandSchema struct {
	ResultSchema json.RawMessage `json:"result_schema,omitempty"`
	Path         string          `json:"path"`
	Use          string          `json:"use"`
	Description  string          `json:"description"`
	Examples     string          `json:"examples,omitempty"`
	Flags        []flagSchema    `json:"flags"`
	Commands     []commandSchema `json:"commands"`
}

type flagSchema struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Required    bool   `json:"required"`
	Inherited   bool   `json:"inherited"`
}

func newSchemaCommand(root *cobra.Command, registry *resultRegistry) *cobra.Command {
	var target *cobra.Command
	return &cobra.Command{
		Use:         "schema [命令路径...]",
		Short:       "以 JSON 格式描述命令和选项",
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			var err error
			target, err = resolveCommand(root, args)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeJSON(cmd.OutOrStdout(), struct {
				SchemaVersion int             `json:"schema_version"`
				OutputFormats []string        `json:"output_formats"`
				ExitCodes     map[string]int  `json:"exit_codes"`
				Commands      []commandSchema `json:"commands"`
			}{1, []string{"text", outputJSON}, map[string]int{"success": 0, "runtime": 1, "usage": 2, "cancelled": 130}, []commandSchema{describeCommand(target, registry)}})
		},
	}
}

func describeCommand(cmd *cobra.Command, registry *resultRegistry) commandSchema {
	result := commandSchema{
		ResultSchema: (*registry)[cmd],
		Path:         cmd.CommandPath(),
		Use:          cmd.Use,
		Description:  cmd.Short,
		Examples:     cmd.Example,
		Flags:        []flagSchema{},
		Commands:     []commandSchema{},
	}
	add := func(inherited bool) func(*pflag.Flag) {
		return func(flag *pflag.Flag) {
			if flag.Hidden {
				return
			}
			required := len(flag.Annotations[cobra.BashCompOneRequiredFlag]) > 0
			result.Flags = append(
				result.Flags,
				flagSchema{flag.Name, flag.Value.Type(), flag.Usage, flag.DefValue, required, inherited},
			)
		}
	}
	cmd.LocalFlags().VisitAll(add(false))
	cmd.InheritedFlags().VisitAll(add(true))
	sort.Slice(result.Flags, func(i, j int) bool {
		return result.Flags[i].Name < result.Flags[j].Name
	})
	for _, child := range cmd.Commands() {
		if !child.Hidden {
			result.Commands = append(result.Commands, describeCommand(child, registry))
		}
	}
	return result
}
