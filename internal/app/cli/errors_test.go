package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zhYou/my/internal/app/greeting"
	"github.com/zhYou/my/internal/command"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
)

func TestChineseErrorsPreserveUserValues(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"unknown"}, "unknown"},
		{[]string{"greet"}, "--name"},
		{[]string{"greet", "--name", "x", "extra"}, "extra"},
		{[]string{"greet", "--unknown"}, "unknown"},
		{[]string{"greet", "-x"}, "x"},
		{[]string{"greet", "--name"}, "name"},
		{[]string{"--json=invalid"}, "invalid"},
		{[]string{"completion", "bogus"}, "bogus"},
		{[]string{"help", "bogus"}, "bogus"},
		{[]string{"schema", "bogus"}, "bogus"},
	} {
		code, out, errOut := invoke(t, append([]string{"--lang", "zh"}, tc.args...)...)
		if code != 2 || out != "" || !strings.HasPrefix(errOut, "错误：") || !strings.Contains(errOut, tc.want) {
			t.Errorf("参数=%v 退出码=%d 标准输出=%q 标准错误=%q", tc.args, code, out, errOut)
		}
		for _, english := range []string{"unknown", "required", "unsupported", "invalid", "accepts", "Error:"} {
			// 用户输入保留原文，错误模板必须使用中文。
			if strings.Contains(strings.ReplaceAll(errOut, tc.want, ""), english) {
				t.Errorf("参数=%v 错误仍包含英文模板：%s", tc.args, errOut)
			}
		}
	}
}

func TestExitCodeAndUnwrap(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"成功", nil, 0},
		{"调用", fault.New(fault.Usage, "参数无效"), 2},
		{"配置", fault.New(fault.Configuration, "配置无效"), 1},
		{"运行", fault.New(fault.Runtime, "执行失败"), 1},
		{"普通业务错误", errors.New("执行失败"), 1},
		{"取消", fault.Wrap(fault.Runtime, "请求失败", context.Canceled), 130},
		{"超时", fmt.Errorf("请求失败：%w", context.DeadlineExceeded), 130},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, exitCode(tc.err))
		})
	}
}

func TestBusinessErrorWithCustomPreRun(t *testing.T) {
	cause := errors.New("业务执行失败")
	root := metadataRoot(Options{Out: io.Discard, ErrOut: io.Discard})
	root.AddCommand(&cobra.Command{
		Use: "fail",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return nil
		},
		RunE: wrapRun(func(*cobra.Command, []string) error {
			return cause
		}),
	})
	root.SetArgs([]string{"fail"})
	err := classifyExecutionError(root.Execute())
	require.ErrorIs(t, err, cause)
	require.Equal(t, 1, exitCode(err))
}

func TestCobraRequiredFlagIsUsageError(t *testing.T) {
	root := metadataRoot(Options{Out: io.Discard, ErrOut: io.Discard})
	requiredCommand := &cobra.Command{
		Use: "required",
		RunE: wrapRun(func(*cobra.Command, []string) error {
			return nil
		}),
	}
	requiredCommand.Flags().String("token", "", "令牌")
	require.NoError(t, requiredCommand.MarkFlagRequired("token"))
	root.AddCommand(requiredCommand)
	root.SetArgs([]string{"required"})
	err := classifyExecutionError(root.Execute())
	require.Error(t, err)
	require.Equal(t, 2, exitCode(err))
}

func TestCommandEmbeddingHelper(_ *testing.T) {
	if os.Getenv("COMMAND_EMBED_HELPER") == "1" {
		os.Exit(7)
	}
}

func TestEmbeddedCommandErrorsFollowCLILanguage(t *testing.T) {
	for language, want := range map[string]string{"zh-CN": "第 1 项执行失败", "en": "Batch item 1 failed"} {
		t.Run(language, func(t *testing.T) {
			home := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			deps := &Dependencies{Greet: func(ctx context.Context, opts greeting.Options) (greeting.Result, error) {
				_, err := command.RunBatch(ctx, []command.Invocation{{
					Program: executable,
					Args:    []string{"-test.run=^TestCommandEmbeddingHelper$"},
				}}, command.Execution{
					Dir: home,
					Env: []string{
						"COMMAND_EMBED_HELPER=1",
						"GORACE=atexit_sleep_ms=0",
						"MY_HOME=" + home,
						"HOME=" + home,
						"USERPROFILE=" + home,
						"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
					},
					Localizer: opts.Localizer,
				}, command.BatchOptions{})
				return greeting.Result{}, err
			}}
			code := Run(t.Context(), []string{"greet", "--name", "fixture", "--lang", language, "--json"}, Options{
				Out:          &stdout,
				ErrOut:       &stderr,
				Dependencies: deps,
				LookupEnv: func(key string) (string, bool) {
					if key == "MY_HOME" {
						return home, true
					}
					return "", false
				},
			})
			if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), want) ||
				!strings.Contains(stderr.String(), "exit status 7") {
				t.Fatalf("嵌入工具诊断 code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}
