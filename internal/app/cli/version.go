package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/base/messages"
)

func newVersionCommand(opts Options, call *invocation) *cobra.Command {
	version := &cobra.Command{
		Use:         commandVersion,
		Short:       "显示版本和构建信息",
		Args:        noArgs,
		Annotations: map[string]string{annotationSkipConfig: annotationEnabled},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if call.asJSON {
				return writeJSON(cmd.OutOrStdout(), versionResult{opts.Name, opts.Version, opts.Commit, opts.Date})
			}
			return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
				text, err := call.localizer.Format(
					"cli.version_text",
					messages.FormatOptions{
						Params: map[string]any{
							paramName: opts.Name,
							"Version": opts.Version,
							"Commit":  opts.Commit,
							"Date":    opts.Date,
						},
					},
				)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(w, text)
				return err
			})
		},
	}

	if err := registerResult[versionResult](&call.results, version, nil); err != nil {
		panic(err)
	}
	return version
}

type versionResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}
