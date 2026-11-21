package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/app/shellenv"
	"github.com/zhYou/my/internal/app/target"
	"github.com/zhYou/my/internal/base/agecrypt"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"
)

const flagAgeIdentity = "age-identity"

func newTargetCommand(call *invocation) *cobra.Command {
	group := &cobra.Command{
		Use:         "target",
		Args:        noArgs,
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	group.AddCommand(
		newTargetCaptureCommand(call),
		newTargetDiffCommand(call),
		newTargetListCommand(call),
		newTargetUpdateCommand(call),
		newTargetSyncCommand(call),
		newTargetResolveCommand(call),
		newTargetRemoveCommand(call),
	)
	call.bindCommand(
		group,
		commandTexts{
			short: message("cli.target_command"),
			example: textMessage{
				id:     "cli.target_command_example",
				params: map[string]any{paramName: call.options.Name},
			},
		},
	)
	return group
}

func newTargetCaptureCommand(call *invocation) *cobra.Command {
	input := target.CaptureOptions{}
	cmd := &cobra.Command{
		Use:         "capture [target...]",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			targets, err := parseTargetIDs(args)
			if err != nil {
				return err
			}
			input.Targets = targets
			return input.Validate(target.DefaultDefinitions())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.targetService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Capture(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "target.capture_result", map[string]any{
				paramCommit:  result.Snapshot.Commit,
				paramChanged: result.Changed,
			})
		},
	}
	cmd.Flags().BoolVar(&input.All, flagAll, false, "收录所有现有普通配置目标")
	cmd.Flags().StringSliceVar(&input.Machines, flagMachine, nil, "限制目标适用的机器 ID")
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(
		cmd,
		commandTexts{
			short: message("cli.target_capture"),
			example: textMessage{
				id:     "cli.target_capture_example",
				params: map[string]any{paramName: call.options.Name},
			},
		},
	)
	call.bindFlag(cmd.Flags().Lookup(flagAll), message("cli.target_all"))
	call.bindFlag(cmd.Flags().Lookup(flagMachine), message(messageTargetMachine))
	registerOrPanic[target.Result](call, cmd)
	return cmd
}

func newTargetDiffCommand(call *invocation) *cobra.Command {
	input := target.DiffOptions{}
	cmd := &cobra.Command{
		Use:         "diff [target...]",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			targets, err := parseTargetIDs(args)
			input.Targets = targets
			if err != nil {
				return err
			}
			return input.Validate(target.DefaultDefinitions())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.targetService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Diff(cmd.Context(), input)
			if err != nil {
				return err
			}
			if call.asJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				if len(result.Changes) == 0 {
					text, err := call.localizer.Format("target.no_changes", messages.FormatOptions{})
					if err != nil {
						return err
					}
					_, err = fmt.Fprintln(w, text)
					return err
				}
				for _, change := range result.Changes {
					text, err := call.localizer.Format(
						"target.diff_line",
						messages.FormatOptions{Params: map[string]any{
							paramTarget: change.Target,
							"Unit":      change.Unit,
							"Action":    change.Action,
						}},
					)
					if err != nil {
						return err
					}
					if _, err := fmt.Fprintln(w, text); err != nil {
						return err
					}
					if change.Diff != "" {
						if _, err := fmt.Fprint(w, change.Diff); err != nil {
							return err
						}
					}
				}
				return nil
			})
		},
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(
		cmd,
		commandTexts{
			short:   message("cli.target_diff"),
			example: textMessage{id: "cli.target_diff_example", params: map[string]any{paramName: call.options.Name}},
		},
	)
	registerOrPanic[target.DiffResult](call, cmd)
	return cmd
}

func newTargetListCommand(call *invocation) *cobra.Command {
	input := target.ListOptions{}
	cmd := &cobra.Command{
		Use:         "list",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.targetService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.List(cmd.Context(), input)
			if err != nil {
				return err
			}
			if call.asJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				header, err := call.localizer.Format("target.list_header", messages.FormatOptions{})
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintln(w, header); err != nil {
					return err
				}
				for _, item := range result.Targets {
					scope := "-"
					if len(item.Scope) != 0 {
						scope = strings.Join(item.Scope, ",")
					}
					baseline := item.BaselineCommit
					if baseline == "" {
						baseline = "-"
					}
					if _, err := fmt.Fprintf(
						w,
						"%s\t%s\t%s\t%s\t%d\n",
						item.Target,
						item.Status,
						scope,
						baseline,
						item.Units,
					); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&input.All, flagAll, false, "包含远端和本机都不存在的内建目标")
	call.bindCommand(
		cmd,
		commandTexts{
			short: message("cli.target_list"),
			example: textMessage{
				id:     "cli.target_list_example",
				params: map[string]any{paramName: call.options.Name},
			},
		},
	)
	call.bindFlag(cmd.Flags().Lookup(flagAll), message("cli.target_list_all"))
	registerOrPanic[target.ListResult](call, cmd)
	return cmd
}

func newTargetUpdateCommand(call *invocation) *cobra.Command {
	input := target.UpdateOptions{}
	cmd := &cobra.Command{
		Use:         "update <target> --machine <id>... | --all-machines",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return err
			}
			input.Target = target.ID(args[0])
			return input.Validate(target.DefaultDefinitions())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.targetService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Update(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "target.update_result", map[string]any{
				paramTarget:  result.Target,
				"Scope":      strings.Join(result.Scope, ","),
				paramCommit:  result.Snapshot.Commit,
				paramChanged: result.Changed,
			})
		},
	}
	cmd.Flags().StringSliceVar(&input.Scope.IDs, flagMachine, nil, "将目标限制到机器 ID")
	cmd.Flags().BoolVar(&input.Scope.All, flagAllMachines, false, "将目标应用到全部 active 机器")
	cmd.MarkFlagsOneRequired(flagMachine, flagAllMachines)
	cmd.MarkFlagsMutuallyExclusive(flagMachine, flagAllMachines)
	call.bindCommand(
		cmd,
		commandTexts{
			short: message("cli.target_update"),
			example: textMessage{
				id:     "cli.target_update_example",
				params: map[string]any{paramName: call.options.Name},
			},
		},
	)
	call.bindFlag(cmd.Flags().Lookup(flagMachine), message(messageTargetMachine))
	call.bindFlag(cmd.Flags().Lookup(flagAllMachines), message("cli.target_all_machines"))
	registerOrPanic[target.UpdateResult](call, cmd)
	return cmd
}

func newTargetSyncCommand(call *invocation) *cobra.Command {
	input := target.SyncOptions{}
	cmd := &cobra.Command{
		Use:         "sync [target...]",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			targets, err := parseTargetIDs(args)
			input.Targets = targets
			if err != nil {
				return err
			}
			return input.Validate(target.DefaultDefinitions())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.targetService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Sync(cmd.Context(), input)
			if err != nil {
				return err
			}
			if call.asJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				text, err := call.localizer.Format("target.sync_result", messages.FormatOptions{Params: map[string]any{
					paramCommit:  result.Snapshot.Commit,
					paramChanged: result.Changed,
				}})
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintln(w, text); err != nil {
					return err
				}
				if len(result.RemovedBaselines) == 0 {
					return nil
				}
				removed := make([]string, len(result.RemovedBaselines))
				for index, id := range result.RemovedBaselines {
					removed[index] = string(id)
				}
				text, err = call.localizer.Format("target.sync_removed", messages.FormatOptions{Params: map[string]any{
					"Targets": strings.Join(removed, ", "),
				}})
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(w, text)
				return err
			})
		},
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(
		cmd,
		commandTexts{
			short:   message("cli.target_sync"),
			example: textMessage{id: "cli.target_sync_example", params: map[string]any{paramName: call.options.Name}},
		},
	)
	registerOrPanic[target.Result](call, cmd)
	return cmd
}

func newTargetResolveCommand(call *invocation) *cobra.Command {
	input := target.ResolveOptions{}
	cmd := &cobra.Command{
		Use:         "resolve <target> (--unit <id> | --all) --from local|remote",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return err
			}
			input.Target = target.ID(args[0])
			return input.Validate(target.DefaultDefinitions())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.targetService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Resolve(cmd.Context(), input)
			if err != nil {
				return err
			}
			units := make([]string, 0, len(result.Resolved))
			for _, unit := range result.Resolved {
				units = append(units, string(unit))
			}
			return businessOutput(cmd, call, result, "target.resolve_result", map[string]any{
				paramTarget: input.Target,
				"Resolved":  len(result.Resolved),
				"Remaining": len(result.Remaining),
				"Units":     strings.Join(units, ", "),
				paramCommit: result.Snapshot.Commit,
			})
		},
	}
	cmd.Flags().StringVar((*string)(&input.Unit), flagUnit, "", "冲突单位 ID")
	cmd.Flags().BoolVar(&input.All, flagAll, false, "解决目标的全部冲突")
	cmd.Flags().StringVar((*string)(&input.From), flagFrom, "", "采用 local 或 remote")
	if err := cmd.MarkFlagRequired(flagFrom); err != nil {
		panic(err)
	}
	cmd.MarkFlagsMutuallyExclusive(flagUnit, flagAll)
	cmd.MarkFlagsOneRequired(flagUnit, flagAll)
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(
		cmd,
		commandTexts{
			short: message("cli.target_resolve"),
			example: textMessage{
				id:     "cli.target_resolve_example",
				params: map[string]any{paramName: call.options.Name},
			},
		},
	)
	call.bindFlag(cmd.Flags().Lookup(flagUnit), message("cli.target_unit"))
	call.bindFlag(cmd.Flags().Lookup(flagAll), message("cli.target_resolve_all"))
	call.bindFlag(cmd.Flags().Lookup(flagFrom), message("cli.target_from"))
	registerOrPanic[target.ResolveResult](call, cmd)
	return cmd
}

func newTargetRemoveCommand(call *invocation) *cobra.Command {
	input := target.RemoveOptions{}
	cmd := &cobra.Command{
		Use:         "remove <target>...",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			targets, err := parseTargetIDs(args)
			input.Targets = targets
			if err != nil {
				return err
			}
			return input.Validate(target.DefaultDefinitions())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.targetService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Remove(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "target.remove_result", map[string]any{
				paramCommit: result.Snapshot.Commit,
			})
		},
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(
		cmd,
		commandTexts{
			short:   message("cli.target_remove"),
			example: textMessage{id: "cli.target_remove_example", params: map[string]any{paramName: call.options.Name}},
		},
	)
	registerOrPanic[target.Result](call, cmd)
	return cmd
}

func (call *invocation) targetService(
	ctx context.Context,
) (*target.Service, machine.Binding, error) {
	data, err := call.businessData(ctx)
	if err != nil {
		return nil, machine.Binding{}, err
	}
	binding, client, known := data.binding, data.repository, data.machines
	home, err := call.userHome()
	if err != nil {
		return nil, machine.Binding{}, err
	}
	environment := shellenv.New(shellenv.Options{
		Logger: call.logger, Tracer: call.sessionTracer(), Localizer: call.localizer,
		Repository: client,
		Cache: shellenv.NewFileCache(
			filepath.Join(call.directories.State, shellenvStateDirectory),
			binding.MachineID,
		),
		LoadAgeKey:    call.loadAgeKey,
		MachineID:     binding.MachineID,
		KnownMachines: known,
	})
	service := target.New(target.Options{
		Logger: call.logger, Tracer: call.sessionTracer(), Localizer: call.localizer,
		Repository: client,
		State: guardedTargetState{
			State: target.NewFileState(call.directories.State, binding.MachineID),
			guard: data.guard,
		},
		Home:             home,
		OS:               runtime.GOOS,
		MachineID:        binding.MachineID,
		KnownMachines:    known,
		LoadAgeKey:       call.loadAgeKey,
		EnvironmentCache: environment,
	})
	return service, binding, nil
}

func (call *invocation) requireBinding() (machine.Binding, error) {
	state := call.machineState()
	binding, exists, err := state.Read()
	if err != nil {
		return machine.Binding{}, err
	}
	if !exists {
		return machine.Binding{}, problemForCLI("machine.not_bound", nil)
	}
	if !strings.EqualFold(binding.Repository, call.config.Data.Repository) ||
		binding.Branch != call.config.Data.Branch {
		return machine.Binding{}, problemForCLI(codeBindingConflict, nil)
	}
	return binding, nil
}

func (call *invocation) userHome() (string, error) {
	lookup := call.options.LookupEnv
	if lookup != nil {
		for _, key := range []string{"HOME", "USERPROFILE"} {
			if value, ok := lookup(key); ok && filepath.IsAbs(value) {
				return value, nil
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", problemForCLI("paths.home", err)
	}
	return home, nil
}

func (call *invocation) addAgeIdentityFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&call.ageIdentity, flagAgeIdentity, "", "覆盖 age identity 文件路径")
	call.bindFlag(cmd.Flags().Lookup(flagAgeIdentity), message("cli.age_identity"))
}

func validateAgeIdentity(cmd *cobra.Command, call *invocation) error {
	if cmd.Flags().Changed(flagAgeIdentity) && call.ageIdentity != "" && !filepath.IsAbs(call.ageIdentity) {
		return usageProblem("identity_path", nil)
	}
	return nil
}

func (call *invocation) loadAgeKey() (*agecrypt.Key, error) {
	call.ageOnce.Do(func() {
		if path := call.config.Secrets.IdentityFile; path != "" {
			call.ageKey, call.ageErr = agecrypt.LoadIdentity(path)
		}
	})
	return call.ageKey, call.ageErr
}

func parseTargetIDs(args []string) ([]target.ID, error) {
	ids := make([]target.ID, 0, len(args))
	for _, value := range args {
		id := target.ID(value)
		if err := id.Validate(); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func problemForCLI(code string, cause error) error {
	definition := fault.Definition{Code: fault.Code(code), Kind: fault.Runtime}
	if cause == nil {
		return definition.New()
	}
	return definition.Wrap(cause, fault.WithSensitiveCause())
}

func registerOrPanic[T any](call *invocation, cmd *cobra.Command) {
	if err := registerResult[T](&call.results, cmd, nil); err != nil {
		panic(err)
	}
}
