package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/session"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/iostreams"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/locales"
)

// Options 提供进程依赖；每次调用均创建新的命令树。
type Options struct {
	PathLayout paths.Layout
	Name       string
	Version    string
	Commit     string
	Date       string
	In         io.Reader
	Out        io.Writer
	ErrOut     io.Writer
	ConfigDir  string
	// Directories 显式覆写应用目录，供嵌入调用及测试隔离；ConfigDir 仅选择配置目录。
	Directories  *paths.Directories
	Language     string
	LookupEnv    func(string) (string, bool)
	Dependencies *Dependencies
}

// Run 执行一次命令并返回约定的退出码，不直接退出进程。
func Run(ctx context.Context, args []string, opts Options) int {
	return run(ctx, args, opts, locales.LoadBundle)
}

func run(ctx context.Context, args []string, opts Options, loadBundle func() (*messages.Bundle, error)) (code int) {
	code = 1
	started := time.Now()
	var executionErr error
	opts = defaults(opts)
	// Cobra、日志、诊断回调与最终错误输出共用调用级 stderr 写锁。
	opts.ErrOut = iostreams.Synchronize(opts.ErrOut, nil)
	sink := &recordingWriter{writer: opts.Out}
	opts.Out = sink
	//nolint:contextcheck // Cobra 构造阶段不执行请求，下面 ExecuteContextC 才注入调用者 ctx。
	root, call, err := buildRoot(opts, loadBundle, args...)
	defer func() {
		if call.session != nil {
			call.session.Finish(
				ctx,
				logging.CommandResult{ExitCode: code, Duration: time.Since(started), Err: executionErr},
			)
		}
		if executionErr != nil {
			call.reportDiagnostic(opts.ErrOut, diagnosticError, executionErr, code)
		}
	}()
	if root == nil {
		executionErr = err
		return 1
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		if index, protocol := completionRequest(root, args); protocol {
			// 使用独立树预解析，不能替换全局 os.Stderr，也不能提前改变实际补全状态。
			shadowOptions := opts
			shadowOptions.Language = call.localizer.Language()
			shadowOptions.Out, shadowOptions.ErrOut = io.Discard, io.Discard
			//nolint:contextcheck // 仅构造独立解析树；实际请求仍由 ExecuteContextC 注入调用者 ctx。
			shadow, shadowCall, setupErr := buildRoot(shadowOptions, func() (*messages.Bundle, error) {
				return call.bundle, nil
			})
			if setupErr != nil {
				err = fault.Wrap(fault.Runtime, "", setupErr)
			} else {
				value := shadowCall.languageFlag
				value.completing, value.preselected = true, true
				if !validCompletionInput(shadow, args, index) {
					if writeErr := writeBytes(
						opts.Out,
						fmt.Appendf(nil, ":%d\n", cobra.ShellCompDirectiveError|cobra.ShellCompDirectiveNoFileComp),
					); writeErr != nil {
						err = fault.Wrap(fault.Runtime, "", writeErr)
					} else {
						return 0
					}
				}
			}
		}
	}
	if err == nil {
		root.SetArgs(args)
		_, err = root.ExecuteContextC(ctx)
	}
	if err == nil && call.helpErr != nil {
		err = call.helpErr
	}
	if err == nil && sink.Err() != nil {
		err = (fault.Definition{Code: "cli.write", Kind: fault.Runtime}).Wrap(sink.Err())
	}
	err = classifyExecutionError(err, call.phase)
	executionErr = err
	if err == nil {
		return call.reportExitCode
	}
	return operationExitCode(ctx, err)
}

func defaults(opts Options) Options {
	if opts.PathLayout == "" {
		opts.PathLayout = paths.HomeLayout
	}
	if opts.Name == "" {
		opts.Name = "my"
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Commit == "" {
		opts.Commit = "none"
	}
	if opts.Date == "" {
		opts.Date = "unknown"
	}
	streams := (iostreams.IOStreams{In: opts.In, Out: opts.Out, ErrOut: opts.ErrOut}).WithDefaults()
	opts.In, opts.Out, opts.ErrOut = streams.In, streams.Out, streams.ErrOut
	return opts
}

func buildRoot(
	opts Options,
	loadBundle func() (*messages.Bundle, error),
	args ...string,
) (*cobra.Command, *invocation, error) {
	call := newInvocation(opts.Dependencies)
	call.options = opts
	bundle, bootstrapErr := loadBundle()
	if bundle == nil && bootstrapErr == nil {
		bootstrapErr = fmt.Errorf("message bundle is nil")
	}
	if bootstrapErr != nil {
		var err error
		bundle, err = messages.NewBundle()
		if err != nil {
			return nil, call, err
		}
	}
	call.bundle = bundle
	call.localizer = bundle.Localizer(languageEnglish)
	root := &cobra.Command{
		Use:           opts.Name,
		Short:         "供用户和自动化工具使用的 CLI 模板",
		Version:       opts.Version,
		Args:          noArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetIn(opts.In)
	root.SetOut(opts.Out)
	root.SetErr(opts.ErrOut)
	root.PersistentFlags().BoolVar(&call.asJSON, flagJSON, false, "以 JSON 格式输出结果")
	root.PersistentFlags().StringVar(&call.configPath, flagConfig, "", "从指定的 TOML 文件读取配置")
	root.PersistentFlags().StringVar(&call.logLevel, flagLogLevel, defaultLogLevel, "日志级别：debug、info、warn、error")
	languageFlagValue := &languageValue{call: call, root: root, preselected: len(args) > 0}
	call.languageFlag = languageFlagValue
	root.PersistentFlags().Var(languageFlagValue, flagLanguage, "显示语言")
	root.PersistentFlags().Lookup(flagLanguage).DefValue = languageEnglish
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if err := validateFlags(cmd, call); err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if skipsConfig(cmd, root) {
			call.setPhase("execute")
			return nil
		}
		var override *string
		if cmd.Flags().Changed(flagLogLevel) {
			override = &call.logLevel
		}
		call.setPhase("configuration")
		cfg, err := call.deps.LoadConfig(
			cmd.Context(),
			config.Options{
				AppName:       opts.Name,
				Layout:        opts.PathLayout,
				Path:          call.configPath,
				Explicit:      cmd.Flags().Changed(flagConfig),
				UserConfigDir: opts.ConfigDir,
				Validator:     call.deps.Validator,
				LookupEnv:     opts.LookupEnv,
				LogLevel:      override,
			},
		)
		if err != nil {
			return (fault.Definition{Code: "cli.config_failed", Kind: fault.Configuration}).Wrap(err)
		}
		if cmd.Annotations[annotationNeedsData] == annotationEnabled {
			if err := cfg.Data.Validate(); err != nil {
				return err
			}
		}
		call.config = cfg
		if opts.Directories != nil {
			call.directories = *opts.Directories
		} else if cmd.Annotations[annotationNeedsData] == annotationEnabled || cmd.Annotations[annotationNeedsPaths] == annotationEnabled {
			dirs, pathErr := paths.Resolve(
				paths.Options{AppName: opts.Name, LookupEnv: opts.LookupEnv, Layout: opts.PathLayout},
			)
			if pathErr != nil {
				return pathErr
			}
			call.directories = dirs
		}
		ctx, owned := session.Begin(
			cmd.Context(),
			cfg,
			logging.RunInfo{
				Component: opts.Name,
				Command:   cmd.CommandPath(),
				Version:   opts.Version,
				Commit:    opts.Commit,
			},
			session.Options{
				Paths:          paths.Options{AppName: opts.Name, LookupEnv: opts.LookupEnv, Layout: opts.PathLayout},
				Directories:    opts.Directories,
				Stderr:         opts.ErrOut,
				ExternalLogger: call.deps.Logger,
				NewLogger:      call.deps.NewLogger,
				Localizer:      call.localizer,
			},
			func(err error) { call.reportDiagnostic(opts.ErrOut, diagnosticWarning, err, 0) },
		)
		call.setSession(owned)
		call.logger = owned.Logger
		cmd.SetContext(ctx)
		return nil
	}
	root.CompletionOptions.DisableDefaultCmd = true
	help := newHelpCommand(root)
	root.SetHelpCommand(help)
	greet := newGreetCommand(call)
	version := newVersionCommand(opts, call)
	schema := newSchemaCommand(root, &call.results)
	completion := newCompletionCommand(root, call, opts.Name)
	doctorCommand := newDoctorCommand(call)
	root.AddCommand(greet, version, schema, completion, doctorCommand, newDataCommand(call), newMachineCommand(call))
	call.bindCommand(doctorCommand, commandTexts{
		short: message("cli.doctor"),
		example: textMessage{
			id:     "cli.doctor_example",
			params: map[string]any{paramName: opts.Name},
		},
	})
	call.bindCommand(root, commandTexts{
		example: textMessage{
			id:     "cli.root_example",
			params: map[string]any{paramName: opts.Name},
		},
		short: message("cli.root"),
	})
	call.bindCommand(help, commandTexts{
		example: textMessage{
			id:     "cli.help_example",
			params: map[string]any{paramName: opts.Name},
		},
		short: message("cli.help"),
		use:   message("cli.help_use"),
	})
	call.bindCommand(greet, commandTexts{
		short: message("cli.greet"),
		use:   message("cli.greet_use"),
		example: textMessage{
			id:     "cli.greet_example",
			params: map[string]any{paramName: opts.Name},
		},
	})
	call.bindCommand(version, commandTexts{
		example: textMessage{
			id:     "cli.version_example",
			params: map[string]any{paramName: opts.Name},
		},
		short: message("cli.version"),
	})
	call.bindCommand(schema, commandTexts{
		example: textMessage{
			id:     "cli.schema_example",
			params: map[string]any{paramName: opts.Name},
		},
		short: message("cli.schema"),
		use:   message("cli.schema_use"),
	})
	call.bindCommand(completion, commandTexts{
		example: textMessage{
			id:     "cli.completion_example",
			params: map[string]any{paramName: opts.Name},
		},
		short: message("cli.completion"),
	})
	for _, binding := range []struct {
		flag string
		id   string
	}{
		{flagJSON, "cli.json"}, {flagConfig, "cli.config"}, {flagLogLevel, "cli.log_level"}, {flagLanguage, "cli.lang"},
	} {
		call.bindFlag(root.PersistentFlags().Lookup(binding.flag), message(binding.id))
	}
	call.bindFlag(greet.Flags().Lookup(flagName), message("cli.name"))
	configureHelp(root, call)
	configureHelp(help, call)
	_, completing := completionRequest(root, args)
	languageFlagValue.completing = completing
	if completing {
		// Cobra 的框架提示不属于候选协议；最终应用错误仍写调用者 stderr。
		root.SetErr(io.Discard)
		if err := registerCompletionValues(root); err != nil {
			return root, call, fault.Wrap(fault.Runtime, "", err)
		}
	}
	protectCommand(root)
	protectCommand(help)
	selected := opts.Language
	if selected == "" {
		selected = languageEnvironment(opts)
	}
	call.jsonDiagnostics = selectJSON(root, args)
	if bootstrapErr != nil {
		return root, call, (fault.Definition{Code: "cli.bootstrap", Kind: fault.Runtime}).Wrap(
			bootstrapErr,
			fault.WithSensitiveCause(),
			fault.WithDiagnostic(fault.Diagnostic{Phase: "bootstrap", CompletedEffects: []string{}}),
		)
	}
	lang, selectionErr := selectLanguage(root, args, selected)
	call.localizer = bundle.Localizer(lang)
	if err := call.localizeTree(root, lang); err != nil {
		return root, call, fault.Wrap(fault.Runtime, "", err)
	}
	return root, call, selectionErr
}

func skipsConfig(cmd, root *cobra.Command) bool {
	if cmd == root || cmd.Annotations[annotationSkipConfig] == annotationEnabled {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == commandCompletion || c.Name() == cobra.ShellCompRequestCmd ||
			c.Name() == cobra.ShellCompNoDescRequestCmd {
			return true
		}
	}
	return false
}

// validateFlags 在配置/资源入口前根据 Cobra 元数据检查必填和组合。
func validateFlags(cmd *cobra.Command, call *invocation) error {
	if f := cmd.Flags().Lookup(flagLogLevel); f != nil && f.Changed {
		switch f.Value.String() {
		case "debug", defaultLogLevel, "warn", "error":
		default:
			return usageProblem(
				"invalid_flag",
				fault.Params{
					paramFlag:  flagLogLevel,
					paramValue: strconv.Quote(f.Value.String()),
					"Type":     "debug|info|warn|error",
				},
			)
		}
	}

	var missing []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0 && !f.Changed {
			missing = append(missing, "--"+f.Name)
		}
	})
	if len(missing) > 0 {
		return usageProblem(schemaRequiredKey, fault.Params{"Flags": strings.Join(missing, ", ")})
	}
	rules := []struct {
		annotation string
		id         string
	}{
		{"cobra_annotation_required_if_others_set", "together"},
		{"cobra_annotation_one_required", "one"},
		{"cobra_annotation_mutually_exclusive", "exclusive"},
	}
	for _, rule := range rules {
		var failure error
		seen := map[string]bool{}
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			for _, group := range f.Annotations[rule.annotation] {
				if seen[group] {
					continue
				}
				seen[group] = true
				names := strings.Fields(group)
				count := 0
				present := true
				for _, name := range names {
					other := cmd.Flags().Lookup(name)
					if other == nil {
						present = false
						break
					}
					if other.Changed {
						count++
					}
				}
				if !present {
					continue
				}
				invalid := rule.id == "together" && count > 0 && count < len(names) || rule.id == "one" && count == 0 ||
					rule.id == "exclusive" && count > 1
				if invalid && failure == nil {
					failure = usageProblem(
						"group",
						fault.Params{"Flags": group, "Rule": call.localizer.Text("cli.group_"+rule.id, nil)},
					)
				}
			}
		})
		if failure != nil {
			return failure
		}
	}
	return nil
}
