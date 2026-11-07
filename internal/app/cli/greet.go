package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/telemetry"

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
			ctx, span := call.session.Tracer.Start(cmd.Context(), phaseGreeting)
			defer span.End()

			started := time.Now()
			result, err := call.deps.Greet(ctx, greeting.Options{Name: name, Localizer: call.localizer})
			err = classifyExecutionError(err, phaseGreeting)
			eventMessage := call.localizer.Text("cli.greet_log", nil)
			if err != nil {
				eventMessage = call.localizer.Text("cli.greet_failed_log", nil)
			}
			call.logger.WithContext(ctx).
				Record(greetingFinished{message: eventMessage, duration: time.Since(started), failed: err != nil})
			if err != nil {
				telemetry.RecordError(span, err)
				span.SetStatus(codes.Error, call.localizer.Text("session.failed", nil))
				return err
			}

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

type greetingFinished struct {
	message  string
	duration time.Duration
	failed   bool
}

func (e greetingFinished) Emit(logger *logging.Logger) {
	outcome := "success"
	if e.failed {
		outcome = "failed"
	}
	fields := []any{"event", "greeting_finished", "phase", phaseGreeting, "outcome", outcome, "duration", e.duration}
	if e.failed {
		logger.Errorw(e.message, fields...)
		return
	}
	logger.Infow(e.message, fields...)
}
