package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/command"
	"github.com/zhYou/my/internal/datarepo"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/app/doctor"
	"github.com/zhYou/my/internal/base/messages"
)

func newDoctorCommand(call *invocation) *cobra.Command {
	options := doctor.Options{Timeout: 10 * time.Second}
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
			call.setPhase("doctor.run")
			call.configureDoctor(&options)
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
									paramName: check.Name,
									"Status":  check.Status,
									"Detail":  check.Detail,
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
	cmd.Flags().BoolVar(&options.Remote, "remote", false, "显式检查 GitHub 远端")
	cmd.Flags().DurationVar(&options.Timeout, "timeout", 10*time.Second, "每项检查的超时")
	call.bindFlag(cmd.Flags().Lookup("remote"), message("cli.doctor_remote"))
	call.bindFlag(cmd.Flags().Lookup("timeout"), message("cli.doctor_timeout"))
	if err := registerResult[doctor.Result](&call.results, cmd, nil); err != nil {
		panic(err)
	}
	return cmd
}

func (call *invocation) configureDoctor(options *doctor.Options) {
	options.Localizer = call.localizer
	options.State = call.deps.State
	options.Paths = func() (paths.Directories, error) {
		if call.options.Directories != nil {
			return *call.options.Directories, nil
		}
		return paths.Resolve(
			paths.Options{
				AppName:   call.options.Name,
				LookupEnv: call.options.LookupEnv,
				Layout:    call.options.PathLayout,
			},
		)
	}
	options.LoadConfig = func(ctx context.Context) (config.Config, error) {
		return call.deps.LoadConfig(
			ctx,
			config.Options{
				AppName:       call.options.Name,
				Layout:        call.options.PathLayout,
				Path:          call.configPath,
				Explicit:      call.configPath != "",
				UserConfigDir: call.options.ConfigDir,
				LookupEnv:     call.options.LookupEnv,
				Validator:     call.deps.Validator,
			},
		)
	}
	options.CheckGH = call.deps.ProbeGH
	if options.CheckGH == nil {
		options.CheckGH = func(ctx context.Context) error {
			result := command.Capture(
				ctx,
				command.Invocation{Program: "gh", Args: []string{"--version"}, Operation: "doctor.gh"},
				command.Execution{Localizer: call.localizer}.WithEnv("GH_DEBUG=", "GH_PROMPT_DISABLED=1"),
				command.CaptureOptions{},
			)
			if result.Err != nil {
				return result.Err
			}
			if result.StdoutTruncated || result.StderrTruncated {
				return errors.New("probe output exceeded capture limit")
			}
			return nil
		}
	}
	options.Credential = call.deps.Credential
	if options.Credential == nil {
		options.Credential = (datarepo.CredentialSource{Execution: command.Execution{Localizer: call.localizer}}).Read
	}
	options.NewClient = func(ctx context.Context, cfg datarepo.Config, token string) (*datarepo.Client, error) {
		return datarepo.New(
			ctx,
			cfg,
			datarepo.Options{
				Token: func(context.Context) (string, error) {
					return token, nil
				},
				Transport: call.deps.DataTransport,
			},
		)
	}
}
