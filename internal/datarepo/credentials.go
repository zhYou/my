package datarepo

import (
	"context"
	"strings"

	"github.com/zhYou/my/internal/command"
)

// CaptureFunc 注入受控的外部进程边界，默认使用 command.Capture。
type CaptureFunc func(context.Context, command.Invocation, command.Execution, command.CaptureOptions) command.Result

// CredentialSource 复用 gh 当前登录，只获取内存 token，不执行登录或刷新。
type CredentialSource struct {
	Execution command.Execution
	Capture   CaptureFunc
}

// Read 通过能力探测兼容旧 gh，新版显式禁止自动刷新。
func (s CredentialSource) Read(ctx context.Context) (string, error) {
	capture := s.Capture
	if capture == nil {
		capture = command.Capture
	}
	execution := s.Execution.WithEnv("GH_DEBUG=", "GH_PROMPT_DISABLED=1")
	run := func(args ...string) command.Result {
		return capture(
			ctx,
			command.Invocation{Program: "gh", Args: args, Operation: phaseAuthenticate},
			execution,
			command.CaptureOptions{Limit: command.DefaultCaptureLimit},
		)
	}
	help := run("auth", "token", "--help")
	if help.Err != nil || help.StdoutTruncated || help.StderrTruncated {
		return "", failure(codeAuthentication, phaseAuthenticate, help.Err, []string{}, recoverAuth)
	}
	args := []string{"auth", "token", "--hostname", "github.com"}
	if strings.Contains(string(help.Stdout), "--no-refresh") {
		args = append(args, "--no-refresh")
	}
	result := run(args...)
	if result.Err != nil || result.StdoutTruncated || result.StderrTruncated {
		return "", failure(codeAuthentication, phaseAuthenticate, result.Err, []string{}, recoverAuth)
	}
	token := strings.TrimSpace(string(result.Stdout))
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return "", failure(codeAuthentication, phaseAuthenticate, nil, []string{}, recoverAuth)
	}
	return token, nil
}
