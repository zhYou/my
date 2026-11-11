package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) != 0 {
		return usageProblem("no_args", fault.Params{paramValue: strconv.Quote(args[0])})
	}
	return nil
}

// 根据结构化错误生成本地化提示，避免替换用户输入中的文本。
func localizedFlagError(_ *cobra.Command, err error) error {
	if structured, ok := errors.AsType[*fault.Error](err); ok {
		return structured
	}
	var missing *pflag.NotExistError
	var required *pflag.ValueRequiredError
	var invalid *pflag.InvalidValueError
	var syntax *pflag.InvalidSyntaxError
	switch {
	case errors.As(err, &missing):
		return usageProblem("unknown_flag", fault.Params{paramFlag: strconv.Quote(missing.GetSpecifiedName())})
	case errors.As(err, &required):
		return usageProblem("required_flag", fault.Params{paramFlag: required.GetFlag().Name})
	case errors.As(err, &invalid):
		return usageProblem(
			"invalid_flag",
			fault.Params{
				paramFlag:  invalid.GetFlag().Name,
				paramValue: strconv.Quote(invalid.GetValue()),
				"Type":     invalid.GetFlag().Value.Type(),
			},
		)
	case errors.As(err, &syntax):
		return usageProblem("syntax", fault.Params{paramValue: strconv.Quote(syntax.GetSpecifiedFlag())})
	default:
		return (fault.Definition{Code: "cli.parse", Kind: fault.Usage}).Wrap(err)
	}
}

func configureHelp(cmd *cobra.Command, call *invocation) {
	// Cobra 创建默认选项前会合并自身和祖先的持久选项。
	cmd.InheritedFlags()
	if cmd.Flags().Lookup(flagHelp) == nil {
		cmd.InitDefaultHelpFlag()
		call.bindFlag(cmd.Flags().Lookup(flagHelp), message("cli.help_flag"))
	}
	if cmd.Version != "" && cmd.Flags().Lookup(flagVersion) == nil {
		cmd.InitDefaultVersionFlag()
		call.bindFlag(cmd.Flags().Lookup(flagVersion), message("cli.version_flag"))
	}
	cmd.SetFlagErrorFunc(localizedFlagError)
	cmd.SetHelpFunc(func(c *cobra.Command, _ []string) {
		text, err := localizedHelp(c, true, call.localizer)
		if err == nil {
			err = writeBytes(c.OutOrStdout(), []byte(text))
		}
		if err != nil && call.helpErr == nil {
			call.helpErr = fault.Wrap(fault.Runtime, "", err)
		}
	})
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		text, err := localizedHelp(c, false, call.localizer)
		if err == nil {
			err = writeBytes(c.ErrOrStderr(), []byte(text))
		}
		if err != nil {
			return fault.Wrap(fault.Runtime, "", err)
		}
		return nil
	})
	for _, child := range cmd.Commands() {
		configureHelp(child, call)
	}
}

type helpFormatter struct {
	localizer *messages.Localizer
	err       error
}

func (f *helpFormatter) text(id string, params map[string]any) string {
	if f.err != nil {
		return ""
	}
	var text string
	text, f.err = f.localizer.Format(id, messages.FormatOptions{Params: params})
	return text
}

func localizedHelp(cmd *cobra.Command, description bool, l *messages.Localizer) (string, error) {
	// strings.Builder 的写入不会返回错误；最后统一检查消息渲染结果。
	var out strings.Builder
	f := &helpFormatter{localizer: l}
	if description {
		if cmd.Long != "" {
			fmt.Fprintf(&out, "%s\n\n", cmd.Long)
		} else if cmd.Short != "" {
			fmt.Fprintf(&out, "%s\n\n", cmd.Short)
		}
	}
	fmt.Fprintln(&out, f.text("help.usage", nil))
	use := cmd.CommandPath()
	if _, args, ok := strings.Cut(cmd.Use, " "); ok {
		use += " " + args
	}
	if cmd.HasAvailableFlags() && !cmd.DisableFlagsInUseLine {
		use += f.text("help.flags_suffix", nil)
	}
	fmt.Fprintf(&out, "  %s\n", use)
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(&out, "  %s%s\n", cmd.CommandPath(), f.text("help.command_suffix", nil))
	}
	if len(cmd.Aliases) > 0 {
		fmt.Fprintf(&out, "\n%s\n  %s\n", f.text("help.aliases", nil), strings.Join(cmd.Aliases, ", "))
	}
	if cmd.Example != "" {
		fmt.Fprintf(&out, "\n%s\n%s\n", f.text("help.examples", nil), cmd.Example)
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(&out, "\n"+f.text("help.commands", nil))
		columns := tabwriter.NewWriter(&out, 0, 4, 2, ' ', 0)
		for _, child := range cmd.Commands() {
			if child.IsAvailableCommand() || child.Name() == commandHelp {
				//nolint:errcheck // tabwriter 只写入内存 strings.Builder，出口不会返回错误。
				_, _ = fmt.Fprintf(columns, "  %s\t%s\n", child.Name(), child.Short)
			}
		}
		//nolint:errcheck // tabwriter 的底层为内存 strings.Builder，不会返回写入错误。
		_ = columns.Flush()
	}
	writeLocalizedFlags(&out, f.text("help.flags", nil), cmd.LocalNonPersistentFlags(), f)
	writeLocalizedFlags(&out, f.text("help.global_flags", nil), cmd.InheritedFlags(), f)
	writeLocalizedFlags(&out, f.text("help.global_flags", nil), cmd.PersistentFlags(), f)
	if cmd.HasAvailableSubCommands() {
		suffix := f.text("help.command_suffix", nil)
		fmt.Fprintln(
			&out,
			"\n"+f.text(
				"help.detail",
				map[string]any{"Command": strconv.Quote(cmd.CommandPath() + suffix + " --help")},
			),
		)
	}
	if f.err != nil {
		return "", f.err
	}
	return out.String(), nil
}

func writeLocalizedFlags(out *strings.Builder, title string, flags *pflag.FlagSet, f *helpFormatter) {
	if !flags.HasAvailableFlags() {
		return
	}
	fmt.Fprintf(out, "\n%s:\n", title)
	columns := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		label := "    --" + flag.Name
		if flag.Shorthand != "" && flag.ShorthandDeprecated == "" {
			label = "-" + flag.Shorthand + ", --" + flag.Name
		}
		if flag.NoOptDefVal == "" {
			label += " <" + flag.Value.Type() + ">"
		}
		description := flag.Usage
		if len(flag.Annotations[cobra.BashCompOneRequiredFlag]) > 0 {
			description += f.text("help.required", nil)
		}
		if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "0" {
			description += f.text("help.default", map[string]any{paramValue: strconv.Quote(flag.DefValue)})
		}
		//nolint:errcheck // tabwriter 只写入内存 strings.Builder，出口不会返回错误。
		_, _ = fmt.Fprintf(columns, "  %s\t%s\n", label, description)
	})
	//nolint:errcheck // tabwriter 的底层为内存 strings.Builder，不会返回写入错误。
	_ = columns.Flush()
}
