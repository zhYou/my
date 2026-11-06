package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/locales"
)

func TestCommandErrorsHaveOwnedBilingualDefinitions(t *testing.T) {
	bundle, err := locales.LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	emptySpec, err := (Spec[string]{}).Build("")
	_ = emptySpec
	_, argErr := (Spec[string]{
		Program: "tool",
		Args:    []Arg[string]{nil},
	}).Build("")
	_, batchErr := RunBatch(t.Context(), nil, Execution{}, BatchOptions{Concurrency: -1})
	cases := []struct {
		code string
		zh   string
		en   string
		err  error
	}{
		{
			"command.windows_suspend_invalid",
			"Windows 主线程挂起计数异常",
			"Unexpected suspend count on the Windows primary thread",
			windowsSuspendInvalid.New(),
		},
		{
			"command.windows_job_not_empty",
			"Windows Job 终止后仍有活动进程",
			"Windows Job still has active processes after termination",
			windowsJobNotEmpty.New(),
		},
		{"command.program_required", "必须指定执行程序", "An executable program is required", err},
		{"command.argument_required", "参数构造函数不能为空", "An argument builder is required", argErr},
		{
			"command.program_required",
			"必须指定执行程序",
			"An executable program is required",
			Run(t.Context(), Invocation{}, Execution{}),
		},
		{
			"command.wait_delay_invalid",
			"进程收尾宽限不能为负数",
			"Process cleanup grace must not be negative",
			Run(t.Context(), Invocation{Program: "unused"}, Execution{WaitDelay: -time.Second}),
		},
		{
			"command.capture_limit_invalid",
			"捕获上限不能为负数",
			"Capture limit must not be negative",
			Capture(t.Context(), Invocation{}, Execution{}, CaptureOptions{Limit: -1}).Err,
		},
		{
			"command.batch_options_invalid",
			"批量并发数、超时和捕获上限不能为负数",
			"Batch concurrency, timeout and capture limit must not be negative",
			batchErr,
		},
		{
			"command.batch_item_incomplete",
			"第 2 项执行未完成",
			"Batch item 2 did not complete",
			batchFailure(1, context.DeadlineExceeded),
		},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			var typed *fault.Error
			if !errors.As(tc.err, &typed) || string(typed.Code) != tc.code || typed.Kind != fault.Runtime {
				t.Fatalf("缺少所属层稳定错误定义: %v", tc.err)
			}
			if got := bundle.Localizer("zh-CN").Render(tc.err); got != tc.zh {
				t.Fatalf("中文 %q", got)
			}
			if got := bundle.Localizer("en").Render(tc.err); got != tc.en {
				t.Fatalf("英文 %q", got)
			}
		})
	}
	call, options := helper(t, "exit")
	result, err := RunBatch(t.Context(), []Invocation{call}, options, BatchOptions{})
	if err == nil || len(result) != 1 {
		t.Fatalf("真实失败未返回 %v", err)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("丢失原生错误 %v", err)
	}
	var typed *fault.Error
	if !errors.As(err, &typed) || typed.Code != "command.batch_item_failed" || typed.Params["Index"] != 1 {
		t.Fatalf("批量结构化字段 %v", err)
	}
	for language, prefix := range map[string]string{"zh-CN": "第 1 项执行失败", "en": "Batch item 1 failed"} {
		if got := bundle.Localizer(language).
			Render(err); !strings.HasPrefix(got, prefix) ||
			!strings.Contains(got, "exit status 7") {
			t.Fatalf("%s: %s", language, got)
		}
	}
	if errors.Is(cases[len(cases)-1].err, context.DeadlineExceeded) {
		t.Fatal("结构化整体错误重新引入派生 deadline")
	}
}

func TestCommandDefaultLocalizerUsesCentralPluralMessages(t *testing.T) {
	localizer := commandLocalizer(nil)
	definition := fault.Definition{Code: "cli.shell_count", Kind: fault.Usage}
	for _, tc := range []struct {
		count int
		want  string
	}{
		{1, "Expected at most one shell name, received 1 argument"},
		{2, "Expected at most one shell name, received 2 arguments"},
	} {
		err := definition.New(fault.WithParams(fault.Params{"Count": tc.count}))
		require.Equal(t, tc.want, localizer.Render(err))
	}
}

func TestCommandBrokenDiagnosticsPreserveChildFailure(t *testing.T) {
	bundle, err := messages.NewBundle(
		fstest.MapFS{"active.en.toml": {Data: []byte("[\"command.started\"]\nother = \"partial {{.Missing}}\"\n")}},
	)
	require.NoError(t, err)
	var sink bytes.Buffer
	logger := logging.New(logging.RunInfo{}, logging.Options{
		Config: logging.Config{Level: "debug", Output: "stderr"},
		Writer: &sink,
	}, func(err error) {
		t.Error(err)
	})
	call, options := helper(t, "exit")
	options.Logger, options.Localizer = logger, bundle.Localizer("en")
	err = Run(t.Context(), call, options)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode())
	lines := bytes.Split(bytes.TrimSpace(sink.Bytes()), []byte("\n"))
	require.Len(t, lines, 2)
	var start, finish map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &start))
	require.NoError(t, json.Unmarshal(lines[1], &finish))
	require.Equal(t, "Operation failed [command.started]", start["msg"])
	require.Equal(t, "Operation failed [command.finished]", finish["msg"])
	require.Equal(t, "failed", finish["outcome"])
	require.Equal(t, float64(7), finish["exit_code"])
	require.NotContains(t, sink.String(), "partial")
}

func TestCommandUninitializedLocalizerUsesEnglishFallback(t *testing.T) {
	var sink bytes.Buffer
	logger := logging.New(logging.RunInfo{}, logging.Options{
		Config: logging.Config{Level: "debug", Output: "stderr"},
		Writer: &sink,
	}, func(err error) {
		t.Error(err)
	})
	call, options := helper(t, "exit")
	options.Logger, options.Localizer = logger, &messages.Localizer{}
	err := Run(t.Context(), call, options)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode())
	require.Contains(t, sink.String(), "Operation failed [command.started]")
	require.Contains(t, sink.String(), "Operation failed [command.finished]")
}

func TestCorruptCatalogProcess(t *testing.T) {
	if os.Getenv("TEST_CORRUPT_I18N_CATALOG") != "1" {
		t.Skip("仅由损坏目录子进程执行")
	}
	var sink bytes.Buffer
	logger := logging.New(logging.RunInfo{}, logging.Options{
		Config: logging.Config{Level: "debug", Output: "stderr"},
		Writer: &sink,
	}, func(err error) {
		t.Error(err)
	})
	call, options := helper(t, "exit")
	options.Logger = logger
	err := Run(t.Context(), call, options)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode())
	lines := bytes.Split(bytes.TrimSpace(sink.Bytes()), []byte("\n"))
	require.Len(t, lines, 2)
	var start, finish map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &start))
	require.NoError(t, json.Unmarshal(lines[1], &finish))
	require.Equal(t, "Operation failed [command.started]", start["msg"])
	require.Equal(t, "Operation failed [command.finished]", finish["msg"])
	require.Equal(t, "failed", finish["outcome"])
	require.Equal(t, float64(7), finish["exit_code"])

	bundle, err := messages.NewBundle(
		fstest.MapFS{
			"active.en.toml": {
				Data: []byte(
					"[\"command.started\"]\nother = \"Injected start\"\n[\"command.finished\"]\nother = \"Injected finish\"\n",
				),
			},
		},
	)
	require.NoError(t, err)
	sink.Reset()
	options.Localizer = bundle.Localizer("en")
	err = Run(t.Context(), call, options)
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode())
	require.Contains(t, sink.String(), "Injected start")
	require.Contains(t, sink.String(), "Injected finish")
}
