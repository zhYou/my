package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/configuration"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/app/shellenv"
	"github.com/zhYou/my/internal/base/agecrypt"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"
)

const flagAgeIdentity = "age-identity"

func newConfigCommand(call *invocation) *cobra.Command {
	group := &cobra.Command{
		Use:         "config",
		Args:        noArgs,
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		RunE:        func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	group.AddCommand(
		newConfigCaptureCommand(call),
		newConfigDiffCommand(call),
		newConfigSyncCommand(call),
		newConfigResolveCommand(call),
		newConfigRemoveCommand(call),
	)
	call.bindCommand(group, commandTexts{short: message("cli.config_command")})
	return group
}

func newConfigCaptureCommand(call *invocation) *cobra.Command {
	input := configuration.CaptureOptions{}
	cmd := &cobra.Command{
		Use:         "capture [target...]",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			if (len(args) == 0) == !input.All {
				return usageProblem("config_target_scope", nil)
			}
			targets, err := parseTargetIDs(args)
			if err != nil {
				return err
			}
			input.Targets = targets
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.configurationService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Capture(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "configuration.capture_result", map[string]any{
				paramCommit:  result.Snapshot.Commit,
				paramChanged: result.Changed,
			})
		},
	}
	cmd.Flags().BoolVar(&input.All, "all", false, "收录所有现有普通配置目标")
	cmd.Flags().StringSliceVar(&input.Machines, flagMachine, nil, "限制目标适用的机器 ID")
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.config_capture")})
	call.bindFlag(cmd.Flags().Lookup("all"), message("cli.config_all"))
	call.bindFlag(cmd.Flags().Lookup(flagMachine), message("cli.config_machine"))
	registerOrPanic[configuration.Result](call, cmd)
	return cmd
}

func newConfigDiffCommand(call *invocation) *cobra.Command {
	input := configuration.DiffOptions{}
	cmd := &cobra.Command{
		Use:         "diff [target...]",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			targets, err := parseTargetIDs(args)
			input.Targets = targets
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.configurationService(cmd.Context())
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
					text, err := call.localizer.Format("configuration.no_changes", messages.FormatOptions{})
					if err != nil {
						return err
					}
					_, err = fmt.Fprintln(w, text)
					return err
				}
				for _, change := range result.Changes {
					text, err := call.localizer.Format(
						"configuration.diff_line",
						messages.FormatOptions{Params: map[string]any{
							"Target": change.Target,
							"Unit":   change.Unit,
							"Action": change.Action,
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
	call.bindCommand(cmd, commandTexts{short: message("cli.config_diff")})
	registerOrPanic[configuration.DiffResult](call, cmd)
	return cmd
}

func newConfigSyncCommand(call *invocation) *cobra.Command {
	input := configuration.SyncOptions{}
	cmd := &cobra.Command{
		Use:         "sync [target...]",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			targets, err := parseTargetIDs(args)
			input.Targets = targets
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.configurationService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Sync(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "configuration.sync_result", map[string]any{
				paramCommit:  result.Snapshot.Commit,
				paramChanged: result.Changed,
			})
		},
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.config_sync")})
	registerOrPanic[configuration.Result](call, cmd)
	return cmd
}

func newConfigResolveCommand(call *invocation) *cobra.Command {
	input := configuration.ResolveOptions{}
	cmd := &cobra.Command{
		Use:         "resolve <target> --unit <id> --from local|remote",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return err
			}
			input.Target = configuration.TargetID(args[0])
			return input.Target.Validate()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := input.Unit.Validate(); err != nil {
				return err
			}
			if input.From != configuration.ResolveLocal && input.From != configuration.ResolveRemote {
				return usageProblem("config_resolve_source", nil)
			}
			service, _, err := call.configurationService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Resolve(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "configuration.resolve_result", map[string]any{
				paramCommit: result.Snapshot.Commit,
			})
		},
	}
	cmd.Flags().StringVar((*string)(&input.Unit), flagUnit, "", "冲突单位 ID")
	cmd.Flags().StringVar((*string)(&input.From), flagFrom, "", "采用 local 或 remote")
	for _, name := range []string{flagUnit, flagFrom} {
		if err := cmd.MarkFlagRequired(name); err != nil {
			panic(err)
		}
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.config_resolve")})
	call.bindFlag(cmd.Flags().Lookup(flagUnit), message("cli.config_unit"))
	call.bindFlag(cmd.Flags().Lookup(flagFrom), message("cli.config_from"))
	registerOrPanic[configuration.Result](call, cmd)
	return cmd
}

func newConfigRemoveCommand(call *invocation) *cobra.Command {
	input := configuration.RemoveOptions{}
	cmd := &cobra.Command{
		Use:         "remove <target>...",
		Annotations: map[string]string{annotationNeedsData: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageProblem("config_target_count", nil)
			}
			targets, err := parseTargetIDs(args)
			input.Targets = targets
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, _, err := call.configurationService(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.Remove(cmd.Context(), input)
			if err != nil {
				return err
			}
			return businessOutput(cmd, call, result, "configuration.remove_result", map[string]any{
				paramCommit: result.Snapshot.Commit,
			})
		},
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.config_remove")})
	registerOrPanic[configuration.Result](call, cmd)
	return cmd
}

func (call *invocation) configurationService(
	ctx context.Context,
) (*configuration.Service, machine.Binding, error) {
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
	record, exists := records[binding.MachineID]
	if !exists || record.Status != machine.Active {
		return nil, machine.Binding{}, problemForCLI("machine.not_active", nil)
	}
	ageKey, err := call.loadAgeKey()
	if err != nil {
		return nil, machine.Binding{}, err
	}
	home, err := call.userHome()
	if err != nil {
		return nil, machine.Binding{}, err
	}
	environment := shellenv.New(shellenv.Options{
		Repository:    client,
		Cache:         shellenv.NewFileCache(filepath.Join(call.directories.State, shellenvStateDirectory)),
		AgeKey:        ageKey,
		MachineID:     binding.MachineID,
		KnownMachines: known,
	})
	service := configuration.New(configuration.Options{
		Repository:       client,
		State:            configuration.NewFileState(call.directories.State),
		Home:             home,
		OS:               runtime.GOOS,
		MachineID:        binding.MachineID,
		KnownMachines:    known,
		AgeKey:           ageKey,
		EnvironmentCache: environment,
	})
	return service, binding, nil
}

func (call *invocation) requireBinding() (machine.Binding, error) {
	var state machine.State = machine.NewFileState(call.directories.State)
	if call.deps.State != nil {
		state = call.deps.State(call.directories.State)
	}
	binding, exists, err := state.Read()
	if err != nil {
		return machine.Binding{}, err
	}
	if !exists {
		return machine.Binding{}, problemForCLI("machine.not_bound", nil)
	}
	if !strings.EqualFold(binding.Repository, call.config.Data.Repository) ||
		binding.Branch != call.config.Data.Branch {
		return machine.Binding{}, problemForCLI("machine.binding_conflict", nil)
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

func (call *invocation) loadAgeKey() (*agecrypt.Key, error) {
	path := call.ageIdentity
	if path == "" {
		path = call.config.Secrets.IdentityFile
	}
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, problemForCLI("secrets.identity_path", nil)
	}
	return agecrypt.LoadIdentity(path)
}

func parseTargetIDs(args []string) ([]configuration.TargetID, error) {
	targets := make([]configuration.TargetID, 0, len(args))
	seen := map[configuration.TargetID]struct{}{}
	for _, value := range args {
		target := configuration.TargetID(value)
		if err := target.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := seen[target]; duplicate {
			return nil, usageProblem("config_target_duplicate", nil)
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets, nil
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
