package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/app/shellenv"
)

func newEnvCommand(call *invocation) *cobra.Command {
	group := &cobra.Command{
		Use:         "env",
		Args:        noArgs,
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		RunE:        func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	group.AddCommand(newEnvSetCommand(call), newEnvRemoveCommand(call), newEnvListCommand(call))
	call.bindCommand(group, commandTexts{short: message("cli.env")})
	return group
}

func newEnvSetCommand(call *invocation) *cobra.Command {
	input := shellenv.SetOptions{}
	cmd := &cobra.Command{
		Use:         "set <NAME>",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			input.Name = args[0]
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			value, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 64<<10))
			if err != nil {
				return err
			}
			input.Value = value
			service, _, err := call.environmentService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Set(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "shellenv.set_result", map[string]any{
				paramName:    result.Name,
				"Secret":     result.Secret,
				paramChanged: result.Changed,
			})
		},
	}
	cmd.Flags().BoolVar(&input.Secret, flagSecret, false, "以 age 密文保存")
	cmd.Flags().BoolVar(&input.Overwrite, "overwrite", false, "覆盖同名变量")
	cmd.Flags().StringSliceVar(&input.Machines, flagMachine, nil, "限制变量适用的机器 ID")
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.env_set")})
	for _, binding := range []struct {
		flag string
		id   string
	}{
		{flagSecret, "cli.env_secret"},
		{"overwrite", "cli.env_overwrite"},
		{flagMachine, "cli.config_machine"},
	} {
		call.bindFlag(cmd.Flags().Lookup(binding.flag), message(binding.id))
	}
	registerOrPanic[shellenv.Result](call, cmd)
	return cmd
}

func newEnvRemoveCommand(call *invocation) *cobra.Command {
	input := shellenv.RemoveOptions{}
	cmd := &cobra.Command{
		Use:         "remove <NAME>",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			input.Name = args[0]
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.environmentService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Remove(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "shellenv.remove_result", map[string]any{
				paramName: result.Name,
			})
		},
	}
	cmd.Flags().BoolVar(&input.Secret, flagSecret, false, "删除秘密变量")
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.env_remove")})
	call.bindFlag(cmd.Flags().Lookup(flagSecret), message("cli.env_secret"))
	registerOrPanic[shellenv.Result](call, cmd)
	return cmd
}

func newEnvListCommand(call *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.environmentService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.List(cmd.Context())
			if err != nil {
				return err
			}
			if call.asJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				for _, variable := range result.Variables {
					kind := "plain"
					if variable.Secret {
						kind = flagSecret
					}
					machines := append([]string(nil), variable.Machines...)
					sort.Strings(machines)
					if _, err := fmt.Fprintf(
						w,
						"%s\t%s\t%s\n",
						variable.Name,
						kind,
						strings.Join(machines, ","),
					); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.env_list")})
	registerOrPanic[shellenv.ListResult](call, cmd)
	return cmd
}

func (call *invocation) environmentService(
	ctx context.Context,
) (*shellenv.Service, machine.Binding, error) {
	client, err := call.dataRepository(ctx)
	if err != nil {
		return nil, machine.Binding{}, err
	}
	binding, err := call.requireBinding()
	if err != nil {
		return nil, machine.Binding{}, err
	}
	snapshot, err := client.Read(ctx)
	if err != nil {
		return nil, machine.Binding{}, err
	}
	records, err := machine.Records(snapshot)
	if err != nil {
		return nil, machine.Binding{}, err
	}
	known := make([]string, 0, len(records))
	for id, record := range records {
		if record.Status == machine.Active {
			known = append(known, id)
		}
	}
	sort.Strings(known)
	ageKey, err := call.loadAgeKey()
	if err != nil {
		return nil, machine.Binding{}, err
	}
	return shellenv.New(shellenv.Options{
		Repository:     client,
		Cache:          shellenv.NewFileCache(filepath.Join(call.directories.State, shellenvStateDirectory)),
		AgeKey:         ageKey,
		MachineID:      binding.MachineID,
		KnownMachines:  known,
		RepositoryName: binding.Repository,
		Branch:         binding.Branch,
	}), binding, nil
}
