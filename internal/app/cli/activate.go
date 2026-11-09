package cli

import (
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/shellenv"
)

func newActivateCommand(call *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activate <bash|zsh|fish|powershell>",
		Short: "输出当前 shell 的环境变量",
		Annotations: map[string]string{
			annotationNeedsPaths:  annotationEnabled,
			annotationSkipSession: annotationEnabled,
		},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			if call.asJSON {
				return usageProblem("activate_json", nil)
			}
			switch args[0] {
			case "bash", "zsh", "fish", "powershell":
				return nil
			default:
				return usageProblem("activate_shell", nil)
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			binding, err := call.requireBinding()
			if err != nil {
				return err
			}
			ageKey, err := call.loadAgeKey()
			if err != nil {
				return err
			}
			service := shellenv.New(shellenv.Options{
				Cache:          shellenv.NewFileCache(filepath.Join(call.directories.State, shellenvStateDirectory)),
				AgeKey:         ageKey,
				MachineID:      binding.MachineID,
				RepositoryName: binding.Repository,
				Branch:         binding.Branch,
			})
			output, err := service.Activate(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				_, err := io.WriteString(w, output)
				return err
			})
		},
	}
	call.addAgeIdentityFlag(cmd)
	call.bindCommand(cmd, commandTexts{short: message("cli.activate")})
	return cmd
}
