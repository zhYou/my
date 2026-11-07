package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/command"
	"github.com/zhYou/my/internal/datarepo"
)

const (
	annotationNeedsData  = "my.needs-data"
	annotationNeedsPaths = "my.needs-paths"
)

func newDataCommand(call *invocation) *cobra.Command {
	group := &cobra.Command{
		Use:         "data",
		Args:        noArgs,
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		RunE:        func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	initialize := &cobra.Command{
		Use:         "init",
		Args:        noArgs,
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := call.machineService(cmd.Context(), true)
			if err != nil {
				return err
			}
			result, err := service.Initialize(cmd.Context())
			if err != nil {
				return err
			}
			return businessOutput(
				cmd,
				call,
				result,
				"data.init_result",
				map[string]any{
					"Repository": result.Snapshot.Repository,
					"Branch":     result.Snapshot.Branch,
					paramCommit:  result.Snapshot.Commit,
					"Changed":    result.Changed,
				},
			)
		},
	}
	if err := registerResult[machine.InitResult](&call.results, initialize, nil); err != nil {
		panic(err)
	}
	group.AddCommand(initialize)
	call.bindCommand(group, commandTexts{short: message("cli.data")})
	call.bindCommand(initialize, commandTexts{short: message("cli.data_init")})
	return group
}

func newMachineCommand(call *invocation) *cobra.Command {
	group := &cobra.Command{
		Use:         "machine",
		Args:        noArgs,
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		RunE:        func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	register := newRegisterCommand(call)
	bind := newBindCommand(call)
	list := &cobra.Command{
		Use:         schemaList,
		Args:        noArgs,
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := call.machineService(cmd.Context(), true)
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
				header, err := call.localizer.Format("machine.list_header", messages.FormatOptions{})
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintln(w, header); err != nil {
					return err
				}
				for _, record := range result.Machines {
					if _, err := fmt.Fprintf(
						w,
						"%s\t%s\t%s\t%s\t%s\t%s\n",
						record.MachineID,
						record.Status,
						record.OS,
						record.Arch,
						record.Kind,
						record.Note,
					); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	show := &cobra.Command{
		Use:         "show <id>",
		Args:        machineIDArgs,
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := call.machineService(cmd.Context(), true)
			if err != nil {
				return err
			}
			result, err := service.Show(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return businessOutput(
				cmd,
				call,
				result,
				messageRecordResult,
				recordParams(result.Machine, result.Snapshot.Commit),
			)
		},
	}
	current := &cobra.Command{
		Use:         "current",
		Args:        noArgs,
		Annotations: map[string]string{annotationNeedsPaths: annotationEnabled},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := call.machineService(cmd.Context(), false)
			if err != nil {
				return err
			}
			result, err := service.Current(cmd.Context())
			if err != nil {
				return err
			}
			return businessOutput(
				cmd,
				call,
				result,
				"machine.current_result",
				map[string]any{"ID": result.MachineID, "Repository": result.Repository, "Branch": result.Branch},
			)
		},
	}
	retire := &cobra.Command{
		Use:         "retire <id>",
		Args:        machineIDArgs,
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := call.machineService(cmd.Context(), true)
			if err != nil {
				return err
			}
			result, err := service.Retire(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return businessOutput(
				cmd,
				call,
				result,
				messageRecordResult,
				recordParams(result.Machine, result.Snapshot.Commit),
			)
		},
	}
	if err := registerResult[machine.ListResult](&call.results, list, machineSchema(schemaList)); err != nil {
		panic(err)
	}
	if err := registerResult[machine.ShowResult](&call.results, show, machineSchema("show")); err != nil {
		panic(err)
	}
	if err := registerResult[machine.Binding](&call.results, current, nil); err != nil {
		panic(err)
	}
	if err := registerResult[machine.ChangeResult](&call.results, retire, machineSchema(schemaChange)); err != nil {
		panic(err)
	}
	group.AddCommand(register, list, show, current, bind, retire)
	call.bindCommand(group, commandTexts{short: message("cli.machine")})
	for _, binding := range []struct {
		cmd *cobra.Command
		id  string
	}{{list, "cli.machine_list"}, {show, "cli.machine_show"}, {current, "cli.machine_current"}, {retire, "cli.machine_retire"}} {
		call.bindCommand(binding.cmd, commandTexts{short: message(binding.id)})
	}
	return group
}

func newRegisterCommand(call *invocation) *cobra.Command {
	input := machine.RegisterOptions{}
	cmd := &cobra.Command{
		Use:         "register <id>",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := machineIDArgs(cmd, args); err != nil {
				return err
			}
			input.ID = args[0]
			return input.Validate()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := call.machineService(cmd.Context(), true)
			if err != nil {
				return err
			}
			result, err := service.Register(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(
				cmd,
				call,
				result,
				messageRecordResult,
				recordParams(result.Machine, result.Snapshot.Commit),
			)
		},
	}
	cmd.Flags().StringVar(&input.Kind, flagKind, "", "机器类型")
	cmd.Flags().StringVar(&input.Note, flagNote, "", "单行备注")
	for _, flag := range []string{flagKind, flagNote} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}
	call.bindCommand(cmd, commandTexts{short: message("cli.machine_register")})
	call.bindFlag(cmd.Flags().Lookup(flagKind), message("cli.machine_kind"))
	call.bindFlag(cmd.Flags().Lookup(flagNote), message("cli.machine_note"))
	if err := registerResult[machine.ChangeResult](&call.results, cmd, machineSchema(schemaChange)); err != nil {
		panic(err)
	}
	return cmd
}

func newBindCommand(call *invocation) *cobra.Command {
	input := machine.BindOptions{}
	cmd := &cobra.Command{
		Use:         "bind <id>",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := machineIDArgs(cmd, args); err != nil {
				return err
			}
			input.ID = args[0]
			return input.Validate()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := call.machineService(cmd.Context(), true)
			if err != nil {
				return err
			}
			result, err := service.Bind(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(
				cmd,
				call,
				result,
				messageRecordResult,
				recordParams(result.Machine, result.Snapshot.Commit),
			)
		},
	}
	cmd.Flags().BoolVar(&input.Reactivate, "reactivate", false, "重新激活退役记录")
	cmd.Flags().BoolVar(&input.UpdatePlatform, "update-platform", false, "使用当前系统和架构更新记录")
	call.bindCommand(cmd, commandTexts{short: message("cli.machine_bind")})
	call.bindFlag(cmd.Flags().Lookup("reactivate"), message("cli.machine_reactivate"))
	call.bindFlag(cmd.Flags().Lookup("update-platform"), message("cli.machine_update_platform"))
	if err := registerResult[machine.ChangeResult](&call.results, cmd, machineSchema(schemaChange)); err != nil {
		panic(err)
	}
	return cmd
}

func machineIDArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	return machine.ValidateID(args[0])
}

func (call *invocation) machineService(ctx context.Context, remote bool) (*machine.Service, error) {
	var state machine.State = machine.NewFileState(call.directories.State)
	if call.deps.State != nil {
		state = call.deps.State(call.directories.State)
	}
	options := machine.Options{
		Config:    call.config.Data,
		State:     state,
		Logger:    call.logger,
		Localizer: call.localizer,
		Now:       call.deps.Now,
	}
	if call.session != nil {
		options.Tracer = call.session.Tracer
	}
	if remote {
		token := call.deps.Credential
		if token == nil {
			token = (datarepo.CredentialSource{Execution: command.Execution{Localizer: call.localizer, Logger: call.logger, Tracer: options.Tracer}}).Read
		}
		client, err := datarepo.New(
			ctx,
			call.config.Data,
			datarepo.Options{
				Token:     token,
				Transport: call.deps.DataTransport,
				Logger:    call.logger,
				Tracer:    options.Tracer,
				Localizer: call.localizer,
				CacheDir:  call.directories.Cache,
			},
		)
		if err != nil {
			return nil, err
		}
		options.Repository = client
	}
	return machine.New(options), nil
}

func businessOutput[T any](cmd *cobra.Command, call *invocation, result T, id string, params map[string]any) error {
	if call.asJSON {
		return writeJSON(cmd.OutOrStdout(), result)
	}
	text, err := call.localizer.Format(id, messages.FormatOptions{Params: params})
	if err != nil {
		return err
	}
	return writeText(cmd.OutOrStdout(), func(w io.Writer) error { _, err := fmt.Fprintln(w, text); return err })
}

func recordParams(record machine.Record, commit string) map[string]any {
	return map[string]any{
		"ID":           record.MachineID,
		"Kind":         record.Kind,
		"OS":           record.OS,
		"Arch":         record.Arch,
		"Note":         record.Note,
		"RegisteredAt": record.RegisteredAt.Format(time.RFC3339),
		"Status":       record.Status,
		paramCommit:    commit,
	}
}
