package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/telemetry"
	"github.com/zhYou/my/internal/locales"
)

func TestCommandLogsUseExplicitLanguageAndStableFields(t *testing.T) {
	bundle, err := locales.LoadBundle()
	if err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"zh-CN", "en", "default"} {
		t.Run(language, func(t *testing.T) {
			var sink bytes.Buffer
			logger := logging.New(logging.RunInfo{}, logging.Options{
				Config: logging.Config{Level: "debug", Output: "stderr"},
				Writer: &sink,
			}, func(err error) {
				t.Error(err)
			})
			call, options := helper(t, "bytes")
			options.Logger = logger
			if language != "default" {
				options.Localizer = bundle.Localizer(language)
			}
			result := Capture(t.Context(), call, options, CaptureOptions{})
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			lines := bytes.Split(bytes.TrimSpace(sink.Bytes()), []byte("\n"))
			if len(lines) != 2 {
				t.Fatalf("日志数量 %s", sink.Bytes())
			}
			var begin, end map[string]any
			if err := json.Unmarshal(lines[0], &begin); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(lines[1], &end); err != nil {
				t.Fatal(err)
			}
			wantBegin, wantEnd := "开始执行外部工具", "外部工具执行结束"
			if language != "zh-CN" {
				wantBegin, wantEnd = "External tool started", "External tool finished"
			}
			if begin["msg"] != wantBegin || end["msg"] != wantEnd {
				t.Fatalf("工具日志语言未生效 %s", sink.Bytes())
			}
			if end["event"] != "tool_finished" || end["operation"] != "test.helper" || end["outcome"] != "success" ||
				end["exit_code"] != float64(0) {
				t.Fatalf("机器字段变化 %v", end)
			}
		})
	}
}

func TestOnlyStartedItemsEmitSpansAndNoRawData(t *testing.T) {
	var traceOutput bytes.Buffer
	cfg := telemetry.Defaults()
	cfg.Exporter = telemetry.File
	provider := telemetry.Start(
		t.Context(),
		cfg,
		telemetry.Info{Service: "command-test"},
		&traceOutput,
		func(err error) {
			t.Error(err)
		},
	)
	t.Cleanup(func() {
		require.NoError(t, provider.Close(t.Context()))
	})
	var logs bytes.Buffer
	logger := logging.New(logging.RunInfo{}, logging.Options{
		Config: logging.Config{Level: "info", Output: "stderr"},
		Writer: &logs,
	}, func(err error) {
		t.Error(err)
	})
	call, options := helper(t, "exit")
	call.Args = append(call.Args, "private-argv")
	options.Env = append(options.Env, "VALUE=private-env")
	options.Logger = logger
	options.Tracer = provider.Tracer
	results, err := RunBatch(
		t.Context(),
		[]Invocation{call, call, call},
		options,
		BatchOptions{Concurrency: 1, FailFast: true},
	)
	if err == nil || results[1].Status != Skipped {
		t.Fatalf("意外结果 %v %+v", err, results)
	}
	if err := provider.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	var spans []struct {
		SpanContext struct {
			TraceID string
			SpanID  string
		}
		Attributes []struct{ Value struct{ Value any } }
	}
	for line := range bytes.SplitSeq(bytes.TrimSpace(traceOutput.Bytes()), []byte("\n")) {
		var span struct {
			SpanContext struct {
				TraceID string
				SpanID  string
			}
			Attributes []struct{ Value struct{ Value any } }
		}
		if err := json.Unmarshal(line, &span); err != nil {
			t.Fatal(err)
		}
		spans = append(spans, span)
	}
	if len(spans) != 1 {
		t.Fatalf("排队项创建了 span：%d", len(spans))
	}
	if bytes.Contains(logs.Bytes(), []byte("private-")) {
		t.Fatalf("日志暴露原始参数 %s", logs.Bytes())
	}
	var event map[string]any
	if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["trace_id"] != spans[0].SpanContext.TraceID || event["span_id"] != spans[0].SpanContext.SpanID ||
		event["event"] != "tool_finished" {
		t.Fatalf("日志与 span 不关联 %v", event)
	}
	for _, a := range spans[0].Attributes {
		if bytes.Contains([]byte(fmt.Sprint(a.Value.Value)), []byte("private-")) {
			t.Fatal("span 暴露原始参数")
		}
	}
}
