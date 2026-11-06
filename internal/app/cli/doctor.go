package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/doctor"
	"github.com/zhYou/my/internal/base/messages"
)

func newDoctorCommand(call *invocation) *cobra.Command {
	options := doctor.Options{}
	cmd := &cobra.Command{
		Use:         "doctor",
		Short:       "报告应用检查结果",
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		Args: func(cmd *cobra.Command, args []string) error {
			if err := noArgs(cmd, args); err != nil {
				return err
			}
			return options.Validate(cmd.Context())
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := call.deps.Doctor(cmd.Context(), options)
			if err != nil {
				return err
			}
			if result.Checks == nil {
				result.Checks = []doctor.Check{}
			}
			if call.asJSON {
				err = writeJSON(cmd.OutOrStdout(), result)
			} else {
				err = writeText(cmd.OutOrStdout(), func(w io.Writer) error {
					if len(result.Checks) == 0 {
						text, err := call.localizer.Format("doctor.empty", messages.FormatOptions{})
						if err != nil {
							return err
						}
						_, err = fmt.Fprintln(w, text)
						return err
					}
					for _, check := range result.Checks {
						text, err := call.localizer.Format(
							"doctor.check",
							messages.FormatOptions{
								Params: map[string]any{
									"Name":   check.Name,
									"Status": check.Status,
									"Detail": check.Detail,
								},
							},
						)
						if err != nil {
							return err
						}
						if _, err = fmt.Fprintln(w, text); err != nil {
							return err
						}
					}
					return nil
				})
			}
			if err != nil {
				return err
			}
			// 检查已完成：失败报告保留在 stdout，退出状态不伪装成执行错误。
			if !result.RequiredAvailable {
				call.reportExitCode = 1
			}
			return nil
		},
	}
	if err := registerResult[doctor.Result](&call.results, cmd, nil); err != nil {
		panic(err)
	}
	return cmd
}
