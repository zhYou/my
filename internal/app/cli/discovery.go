package cli

import (
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/zhYou/my/internal/base/fault"

	"github.com/spf13/cobra"
)

// 补全协议中的 shell 名称也用于对应文件和生成器分派。
const (
	shellBash       = "bash"
	shellZsh        = "zsh"
	shellFish       = "fish"
	shellPowerShell = "powershell"
)

func resolveCommand(root *cobra.Command, args []string) (*cobra.Command, error) {
	target := root
	for _, name := range args {
		var next *cobra.Command
		for _, child := range target.Commands() {
			if !child.Hidden && (child.Name() == name || slices.Contains(child.Aliases, name)) {
				next = child
				break
			}
		}
		if next == nil {
			return nil, usageProblem("unknown_path", fault.Params{paramValue: strconv.Quote(strings.Join(args, " "))})
		}
		target = next
	}
	return target, nil
}

func newHelpCommand(root *cobra.Command) *cobra.Command {
	var target *cobra.Command
	return &cobra.Command{
		Use:               "help [命令路径...]",
		Short:             "显示命令帮助",
		ValidArgsFunction: completeHelp(root),
		Annotations:       map[string]string{annotationSkipConfig: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			var err error
			target, err = resolveCommand(root, args)
			return err
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return target.Help()
		},
	}
}

func newCompletionCommand(root *cobra.Command, call *invocation, name string) *cobra.Command {
	shells := []string{shellBash, shellZsh, shellFish, shellPowerShell}
	completion := &cobra.Command{
		Use:         "completion",
		Short:       "生成 Shell 补全脚本",
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageProblem("shell_count", fault.Params{"Count": len(args)})
			}
			if len(args) == 1 && !slices.Contains(shells, args[0]) {
				return usageProblem("shell", fault.Params{paramValue: strconv.Quote(args[0])})
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			// 保留 completion -- <shell> 这一历史位置参数调用。
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				return generateCompletion(root, args[0], w)
			})
		},
	}
	for _, shell := range shells {
		child := &cobra.Command{
			Use: shell,
			Args: func(_ *cobra.Command, args []string) error {
				if len(args) != 0 {
					return usageProblem("shell_count", fault.Params{"Count": len(args) + 1})
				}
				return nil
			},
			RunE: func(cmd *cobra.Command, _ []string) error {
				return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
					return generateCompletion(root, shell, w)
				})
			},
		}
		file := name + "." + shell
		switch shell {
		case shellZsh:
			file = "_" + name
		case shellPowerShell:
			file = name + ".ps1"
		}
		params := map[string]any{paramName: name, "Shell": shell, "File": file}
		call.bindCommand(child, commandTexts{
			short:   textMessage{id: "cli.completion_shell", params: params},
			example: textMessage{id: "cli.completion_shell_example", params: params},
		})
		completion.AddCommand(child)
	}
	return completion
}

func completeHelp(root *cobra.Command) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, prefix string) ([]cobra.Completion, cobra.ShellCompDirective) {
		target, err := resolveCommand(root, args)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var choices []cobra.Completion
		for _, child := range target.Commands() {
			if !child.Hidden && (child.IsAvailableCommand() || child.Name() == commandHelp) &&
				strings.HasPrefix(child.Name(), prefix) {
				choices = append(choices, child.Name()+"\t"+child.Short)
			}
		}
		return choices, cobra.ShellCompDirectiveNoFileComp
	}
}

func registerCompletionValues(root *cobra.Command) error {
	for _, flag := range []struct {
		name   string
		values []cobra.Completion
	}{
		{flagLanguage, []cobra.Completion{languageEnglish, languageChinese, "zh"}},
		{flagLogLevel, []cobra.Completion{"debug", defaultLogLevel, "warn", "error"}},
	} {
		if err := root.RegisterFlagCompletionFunc(
			flag.name,
			cobra.FixedCompletions(flag.values, cobra.ShellCompDirectiveNoFileComp),
		); err != nil {
			return err
		}
	}
	return nil
}

func generateCompletion(root *cobra.Command, shell string, w io.Writer) error {
	switch shell {
	case shellBash:
		return root.GenBashCompletionV2(w, true)
	case shellZsh:
		return root.GenZshCompletion(w)
	case shellFish:
		return root.GenFishCompletion(w, true)
	default:
		return root.GenPowerShellCompletionWithDesc(w)
	}
}
