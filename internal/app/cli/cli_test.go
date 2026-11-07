package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/paths"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/greeting"
)

func TestInvalidArgumentsFailWithoutOutputOrPrompt(t *testing.T) {
	for _, args := range [][]string{
		{"greet"},
		{"greet", "--name", ""},
		{"greet", "--name", "x", "extra"},
		{"unknown"},
		{"greet", "--unknown"},
		{"--unknown", "--json"},
		{"completion", "bogus"},
		{"completion", "bash", "extra"},
		{"help", "unknown"},
		{"help", "greet", "unknown"},
	} {
		code, out, errOut := invoke(t, args...)
		if code != 2 || out != "" ||
			(strings.Count(errOut, "Error:") != 1 && strings.Count(errOut, `"type":"error"`) != 1) {
			t.Errorf("args=%v code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
}

func TestCancellationExitCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, out, errOut := invokeContext(ctx, t, "greet", "--name", "Codex")
	if code != 130 || out != "" || !strings.Contains(errOut, "Operation cancelled") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestArgvCancellationReturns130(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, stderr bytes.Buffer
	if code := Run(ctx, []string{"greet", "--name", "你好"}, isolatedOptions(home, &out, &stderr)); code != 130 {
		t.Fatalf("取消退出码=%d", code)
	}
	if out.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("取消输出不满足约定：stdout=%q stderr=%q", out.String(), stderr.String())
	}
}

func TestDebugLogsDoNotPolluteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[logging]\noutput = \"stderr\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := invoke(t, "greet", "--name", "Codex", "--json", "--log-level", "debug", "--config", path)
	if code != 0 || !json.Valid([]byte(out)) || !strings.Contains(errOut, `"event":"greeting_finished"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestBusinessCreatesPersistentEvents(t *testing.T) {
	home := t.TempDir()
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"greet", "--name", "Codex", "--json"}, Options{
		Out:    &out,
		ErrOut: &stderr,
		LookupEnv: func(key string) (string, bool) {
			if key == "MY_HOME" {
				return home, true
			}
			return "", false
		},
	})
	require.Zero(t, code, stderr.String())
	require.JSONEq(t, `{"message":"Hello, Codex!"}`, out.String())
	files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	data, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Contains(t, string(data), "command_started")
	require.Contains(t, string(data), "command_finished")
}

func TestDiscoveryAndInvalidCallsCreateNoState(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"greet"}, 2},
		{[]string{"greet", "--name", " "}, 2},
		{[]string{"greet", "--name", "x", "--log-level", "bad"}, 2},
		{[]string{"help"}, 0},
		{[]string{"version"}, 0},
		{[]string{"schema"}, 0},
		{[]string{"completion", "bash"}, 0},
		{[]string{"__complete", "greet", ""}, 0},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			home := t.TempDir()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), tc.args, Options{
				Out:    &out,
				ErrOut: &stderr,
				LookupEnv: func(key string) (string, bool) {
					return home, key == "MY_HOME"
				},
			})
			require.Equal(t, tc.code, code, stderr.String())
			if tc.code != 0 {
				require.Empty(t, out.String())
				require.NotEmpty(t, stderr.String())
			}
			_, err := os.Stat(filepath.Join(home, "state"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestFileTraceCorrelatesActualCommandAndBusinessLogs(t *testing.T) {
	home := t.TempDir()
	var out, stderr bytes.Buffer
	env := map[string]string{
		"MY_HOME":               home,
		"MY_TELEMETRY_EXPORTER": "file",
		"MY_LOG_LEVEL":          "debug",
	}
	code := Run(t.Context(), []string{"greet", "--name", "private-name", "--json", "--lang", "en"}, Options{
		Out:    &out,
		ErrOut: &stderr,
		LookupEnv: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
	})
	require.Zero(t, code, stderr.String())
	require.Empty(t, stderr.String())
	files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "events.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	logs, err := os.ReadFile(files[0])
	require.NoError(t, err)
	traces, err := os.ReadFile(filepath.Join(filepath.Dir(files[0]), "trace.jsonl"))
	require.NoError(t, err)
	require.NotContains(t, string(logs), "private-name")
	require.NotContains(t, string(traces), "private-name")
	spans := map[string]map[string]any{}
	for line := range bytes.SplitSeq(bytes.TrimSpace(traces), []byte("\n")) {
		var row map[string]any
		require.NoError(t, json.Unmarshal(line, &row))
		name, ok := row["Name"].(string)
		require.True(t, ok)
		spans[name] = row
	}
	require.Len(t, spans, 2)
	business := spans["greeting.run"]
	require.NotNil(t, business)
	businessContext, ok := business["SpanContext"].(map[string]any)
	require.True(t, ok)
	var rootContext map[string]any
	for name, row := range spans {
		if name != "greeting.run" {
			rootContext, ok = row["SpanContext"].(map[string]any)
			require.True(t, ok)
		}
	}
	require.Equal(t, rootContext["TraceID"], businessContext["TraceID"])
	parent, ok := business["Parent"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, rootContext["SpanID"], parent["SpanID"])
	events := map[string]map[string]any{}
	for line := range bytes.SplitSeq(bytes.TrimSpace(logs), []byte("\n")) {
		var row map[string]any
		require.NoError(t, json.Unmarshal(line, &row))
		event, ok := row["event"].(string)
		require.True(t, ok)
		events[event] = row
	}
	require.Equal(t, "Command started", events["command_started"]["msg"])
	require.Equal(t, rootContext["TraceID"], events["command_started"]["trace_id"])
	require.Equal(t, rootContext["SpanID"], events["command_finished"]["span_id"])
	require.Equal(t, businessContext["SpanID"], events["greeting_finished"]["span_id"])
}

func TestLogAndTraceOutputsAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		log   string
		trace string
		files []string
	}{{"none", "none", nil}, {"stderr", "none", nil}, {"none", "file", []string{"trace.jsonl"}}, {"file", "none", []string{"events.jsonl"}}} {
		t.Run(tc.log+"-"+tc.trace, func(t *testing.T) {
			home := t.TempDir()
			var out, stderr bytes.Buffer
			env := map[string]string{
				"MY_HOME":               home,
				"MY_LOGGING_OUTPUT":     tc.log,
				"MY_TELEMETRY_EXPORTER": tc.trace,
			}
			code := Run(t.Context(), []string{"greet", "--name", "x"}, Options{
				Out:    &out,
				ErrOut: &stderr,
				LookupEnv: func(k string) (string, bool) {
					v, ok := env[k]
					return v, ok
				},
			})
			require.Zero(t, code, stderr.String())
			files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "*.jsonl"))
			require.NoError(t, err)
			require.Len(t, files, len(tc.files))
			for i, file := range files {
				require.Equal(t, tc.files[i], filepath.Base(file))
			}
			if tc.log == "stderr" {
				require.Contains(t, stderr.String(), "command_started")
			} else {
				require.Empty(t, stderr.String())
			}
		})
	}
}

func TestConfigFailureCreatesNoDiagnostics(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte("[logging]\noutput='invalid'\n"), 0o600))
	var out, stderr bytes.Buffer
	code := Run(t.Context(), []string{"greet", "--name", "x"}, Options{
		Out:    &out,
		ErrOut: &stderr,
		LookupEnv: func(k string) (string, bool) {
			return home, k == "MY_HOME"
		},
	})
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	_, err := os.Stat(filepath.Join(home, "state"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDiagnosticFailurePreservesBusinessResultAndWarnsOncePerSubsystem(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "state"), []byte("blocked"), 0o600))
	var out, stderr bytes.Buffer
	code := Run(t.Context(), []string{"greet", "--name", "x", "--json", "--lang", "en"}, Options{
		Out:    &out,
		ErrOut: &stderr,
		LookupEnv: func(k string) (string, bool) {
			return home, k == "MY_HOME"
		},
	})
	require.Zero(t, code)
	require.JSONEq(t, `{"message":"Hello, x!"}`, out.String())
	require.Equal(t, 1, strings.Count(stderr.String(), "Diagnostic files unavailable"))
	require.Equal(t, 1, strings.Count(stderr.String(), "Logging diagnostics unavailable"))
}

func TestCanceledBusinessStillFlushesTrace(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out, stderr bytes.Buffer
	env := map[string]string{"MY_HOME": home, "MY_TELEMETRY_EXPORTER": "file"}
	deps := Dependencies{Greet: func(ctx context.Context, _ greeting.Options) (greeting.Result, error) {
		cancel()
		return greeting.Result{}, ctx.Err()
	}}
	code := Run(ctx, []string{"greet", "--name", "x", "--json"}, Options{
		Out:          &out,
		ErrOut:       &stderr,
		Dependencies: &deps,
		LookupEnv: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
	})
	require.Equal(t, 130, code)
	require.Empty(t, out.String())
	files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", "trace.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	data, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Contains(t, string(data), "greeting.run")
	require.Contains(t, string(data), "Error")
	logs, err := os.ReadFile(filepath.Join(filepath.Dir(files[0]), "events.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(logs), `"exit_code":130`)
}

type overlappingStderr struct {
	bytes.Buffer
	entered  chan struct{}
	release  chan struct{}
	active   atomic.Int32
	overlaps atomic.Int32
	syncs    atomic.Int32
	closes   atomic.Int32
}

func (s *overlappingStderr) Write(p []byte) (int, error) {
	if s.active.Add(1) > 1 {
		s.overlaps.Add(1)
	}
	defer s.active.Add(-1)
	if bytes.Contains(p, []byte("测试告警")) {
		close(s.entered)
		<-s.release
	}
	return s.Buffer.Write(p)
}

func (s *overlappingStderr) Sync() error {
	s.syncs.Add(1)
	return nil
}

func (s *overlappingStderr) Close() error {
	s.closes.Add(1)
	return nil
}

func TestCLIErrorAndDiagnosticWarningShareStderrSerialization(t *testing.T) {
	sink := &overlappingStderr{entered: make(chan struct{}), release: make(chan struct{})}
	var out bytes.Buffer
	var warn func(error)
	warningDone := make(chan struct{})
	deps := Dependencies{
		LoadConfig: func(context.Context, config.Options) (config.Config, error) {
			cfg := config.Defaults()
			cfg.Logging.Output = "stderr"
			return cfg, nil
		},
		NewLogger: func(info logging.RunInfo, opts logging.Options, report func(error)) *logging.Logger {
			warn = report
			return logging.New(info, opts, report)
		},
		Greet: func(context.Context, greeting.Options) (greeting.Result, error) {
			go func() {
				defer close(warningDone)
				warn(errors.New("测试告警"))
			}()
			<-sink.entered
			// 告警保持进行时返回业务错误；独立定时器释放它，避免将错误写入成功作为同步条件。
			time.AfterFunc(25*time.Millisecond, func() {
				close(sink.release)
			})
			return greeting.Result{}, errors.New("测试业务失败")
		},
	}
	code := Run(t.Context(), []string{"greet", "--name", "x"}, Options{
		Out:          &out,
		ErrOut:       sink,
		Dependencies: &deps,
		Directories:  &paths.Directories{State: t.TempDir()},
		LookupEnv: func(string) (string, bool) {
			return "", false
		},
	})
	<-warningDone
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.Zero(t, sink.overlaps.Load(), "业务错误输出与诊断告警并发调用底层 stderr")
	require.Zero(t, sink.syncs.Load())
	require.Zero(t, sink.closes.Load())
	require.Contains(t, sink.String(), "测试业务失败")
	require.Contains(t, sink.String(), "测试告警")
}
