package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/greeting"
	"go.opentelemetry.io/otel/codes"
)

func newGreetCommand(call *invocation) *cobra.Command {
	var name string
	greet := &cobra.Command{
		Use:   "greet --name 姓名",
		Short: "输出问候语（可移除的示例）",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := noArgs(cmd, args); err != nil {
				return err
			}
			return (greeting.Options{Name: name, Localizer: call.localizer}).Validate(
				cmd.Context(),
				call.deps.Validator,
			)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, span := call.session.Tracer.Start(cmd.Context(), "greeting.run")
			defer span.End()

			result, err := call.deps.Greet(ctx, greeting.Options{Name: name, Localizer: call.localizer})
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, call.localizer.Text("session.failed", nil))
				return err
			}

			call.logger.WithContext(ctx).Debugw(call.localizer.Text("cli.greet_log", nil), "event", "greeting_finished")
			if call.asJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				_, err := fmt.Fprintln(w, result.Message)
				return err
			})
		},
	}
	greet.Flags().StringVar(&name, flagName, "", "问候对象的姓名")
	if err := greet.MarkFlagRequired(flagName); err != nil {
		panic(err)
	}

	if err := registerResult[greeting.Result](&call.results, greet, nil); err != nil {
		panic(err)
	}
	return greet
}
