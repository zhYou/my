package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"
	appconfig "github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/diagnostics"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/base/telemetry"
	"github.com/zhYou/my/internal/locales"
	"go.opentelemetry.io/otel/trace"
)

func TestSessionUsesDefaultEnglishAndExplicitChinese(t *testing.T) {
	bundle, err := locales.LoadBundle()
	require.NoError(t, err)
	cases := []struct {
		name      string
		localizer *messages.Localizer
		start     string
		finish    string
	}{
		{"default", nil, "Command started", "Command finished"},
		{"chinese", bundle.Localizer("zh-CN"), "命令开始", "命令结束"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Logging.Output = "stderr"
			var sink bytes.Buffer
			ctx, s := Begin(
				t.Context(),
				cfg,
				logging.RunInfo{},
				Options{Stderr: &sink, Localizer: tc.localizer},
				func(err error) {
					t.Error(err)
				},
			)
			s.Finish(ctx, logging.CommandResult{})
			lines := bytes.Split(bytes.TrimSpace(sink.Bytes()), []byte("\n"))
			require.Len(t, lines, 2)
			var start, finish map[string]any
			require.NoError(t, json.Unmarshal(lines[0], &start))
			require.NoError(t, json.Unmarshal(lines[1], &finish))
			require.Equal(t, tc.start, start["msg"])
			require.Equal(t, tc.finish, finish["msg"])
			require.Equal(t, "command_finished", finish["event"])
		})
	}
}

func TestSessionBrokenDiagnosticsPreserveBusinessFailure(t *testing.T) {
	bundle, err := messages.NewBundle(
		fstest.MapFS{"active.en.toml": {Data: []byte("[\"session.started\"]\nother = \"partial {{.Missing}}\"\n")}},
	)
	require.NoError(t, err)
	cfg := defaultConfig()
	cfg.Logging.Output = "stderr"
	var sink bytes.Buffer
	ctx, s := Begin(
		t.Context(),
		cfg,
		logging.RunInfo{},
		Options{Stderr: &sink, Localizer: bundle.Localizer("en")},
		func(err error) {
			t.Error(err)
		},
	)
	businessErr := (fault.Definition{Code: "example.business", Kind: fault.Runtime}).New()
	s.Finish(ctx, logging.CommandResult{ExitCode: 1, Err: businessErr})
	lines := bytes.Split(bytes.TrimSpace(sink.Bytes()), []byte("\n"))
	require.Len(t, lines, 2)
	var start, finish map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &start))
	require.NoError(t, json.Unmarshal(lines[1], &finish))
	require.Equal(t, "Operation failed [session.started]", start["msg"])
	require.Equal(t, "Operation failed [session.finished]", finish["msg"])
	require.Equal(t, float64(1), finish["exit_code"])
	require.Equal(t, "example.business", finish["error_code"])
	require.NotContains(t, sink.String(), "partial")
}

func TestSessionConcurrentLocalizersRemainIsolated(t *testing.T) {
	bundle, err := locales.LoadBundle()
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 20 {
		for _, tc := range []struct {
			lang string
			want string
		}{{"en", "Command finished"}, {"zh-CN", "命令结束"}} {
			wg.Go(func() {
				cfg := defaultConfig()
				cfg.Logging.Output = "stderr"
				var sink bytes.Buffer
				ctx, s := Begin(
					t.Context(),
					cfg,
					logging.RunInfo{},
					Options{Stderr: &sink, Localizer: bundle.Localizer(tc.lang)},
					func(err error) {
						t.Error(err)
					},
				)
				s.Finish(ctx, logging.CommandResult{})
				if !bytes.Contains(sink.Bytes(), []byte(tc.want)) {
					t.Errorf("%s: %s", tc.lang, sink.Bytes())
				}
			})
		}
	}
	wg.Wait()
}

func TestSessionUninitializedLocalizerUsesEnglishFallback(t *testing.T) {
	cfg := defaultConfig()
	cfg.Logging.Output = "stderr"
	var sink bytes.Buffer
	ctx, s := Begin(
		t.Context(),
		cfg,
		logging.RunInfo{},
		Options{Stderr: &sink, Localizer: &messages.Localizer{}},
		func(err error) {
			t.Error(err)
		},
	)
	s.Finish(ctx, logging.CommandResult{})
	require.Contains(t, sink.String(), "Operation failed [session.started]")
	require.Contains(t, sink.String(), "Operation failed [session.finished]")
}

type countingSink struct {
	bytes.Buffer
	syncs int
}

func (s *countingSink) Sync() error {
	s.syncs++
	return nil
}

func TestBorrowedLoggerRemainsUsableWithoutSyncOrDirectories(t *testing.T) {
	home := filepath.Join(t.TempDir(), "unused")
	t.Setenv("MY_HOME", home)
	sink := &countingSink{}
	external := logging.New(
		logging.RunInfo{},
		logging.Options{Config: logging.Defaults(), Writer: sink, RunID: "external"},
		nil,
	)
	ctx, s := Begin(
		t.Context(),
		defaultConfig(),
		logging.RunInfo{Component: "my", Command: "doctor"},
		Options{ExternalLogger: external},
		func(err error) {
			t.Error(err)
		},
	)
	s.Finish(ctx, logging.CommandResult{})
	s.Finish(ctx, logging.CommandResult{})
	require.Zero(t, sink.syncs)
	_, err := os.Stat(home)
	require.ErrorIs(t, err, os.ErrNotExist)
	sink.Reset()
	external.Infow("still_usable")
	require.Contains(t, sink.String(), "still_usable")
	require.NotContains(t, sink.String(), "doctor")
}

func TestConcurrentSessionsOwnPrivateFilesAndFinishOnce(t *testing.T) {
	t.Setenv("MY_HOME", t.TempDir())
	var runPaths sync.Map
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			ctx, s := Begin(
				t.Context(),
				defaultConfig(),
				logging.RunInfo{Component: "my", Command: "doctor"},
				Options{Paths: paths.Options{AppName: "my"}},
				func(err error) {
					t.Error(err)
				},
			)
			path := s.files.Path
			if _, duplicate := runPaths.LoadOrStore(path, true); duplicate {
				t.Error("运行目录重复")
			}
			s.Finish(ctx, logging.CommandResult{})
			s.Finish(ctx, logging.CommandResult{})
		})
	}
	group.Wait()
	runPaths.Range(func(key, _ any) bool {
		path, ok := key.(string)
		require.True(t, ok)
		info, err := os.Stat(path)
		require.NoError(t, err)
		if runtime.GOOS != "windows" {
			require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
		}
		file := filepath.Join(path, diagnostics.EventsFile)
		info, err = os.Stat(file)
		require.NoError(t, err)
		if runtime.GOOS != "windows" {
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		}
		// #nosec G304 -- 路径仅来自本测试创建并登记的私有诊断目录。
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, 1, bytes.Count(data, []byte("command_finished")))
		// #nosec G304 -- 仅回读本测试诊断目录中的固定元数据文件。
		data, err = os.ReadFile(filepath.Join(path, ".run.json"))
		require.NoError(t, err)
		var metadata map[string]any
		require.NoError(t, json.Unmarshal(data, &metadata))
		require.Contains(t, metadata, "finished")
		require.NotEqual(t, "0001-01-01T00:00:00Z", metadata["finished"])
		lock := flock.New(filepath.Join(path, ".lock"))
		locked, err := lock.TryLock()
		require.NoError(t, err)
		require.True(t, locked)
		require.NoError(t, lock.Close())
		return true
	})
}

func TestWriteAndFinishFailuresWarnOnce(t *testing.T) {
	t.Setenv("MY_HOME", t.TempDir())
	var warnings atomic.Int32
	ctx, s := Begin(
		t.Context(),
		defaultConfig(),
		logging.RunInfo{},
		Options{Paths: paths.Options{AppName: "my"}},
		func(error) {
			warnings.Add(1)
		},
	)
	writer, err := s.files.Writer(diagnostics.EventsFile)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	s.Logger.Infow("cannot write")
	s.Logger.Errorw("still cannot write")
	s.Finish(ctx, logging.CommandResult{})
	require.Equal(t, int32(1), warnings.Load())
}

func TestUnavailableWriterDegradesWithoutTypedNil(t *testing.T) {
	files, err := diagnostics.Open(t.TempDir(), diagnostics.Defaults(), nil)
	require.NoError(t, err)
	require.NoError(t, files.Close())
	s := &Session{files: files}
	warnings := 0
	warn := reporter(loggingFailed, func(error) {
		warnings++
	})
	writer := s.writer(diagnostics.EventsFile, warn)
	require.Nil(t, writer)
	logger := logging.New(
		logging.RunInfo{},
		logging.Options{Config: logging.Defaults(), Writer: writer, RunID: files.ID},
		warn,
	)
	logger.Started()
	logger.Record(logging.CommandResult{ExitCode: 0})
	require.NoError(t, logger.Sync())
	require.Equal(t, 1, warnings)
}

func TestFinishWaitsForRegisteredWorkBeforeEndingSpanAndClosingFiles(t *testing.T) {
	cfg := defaultConfig()
	cfg.Telemetry.Exporter = telemetry.File
	ctx, s := Begin(
		t.Context(),
		cfg,
		logging.RunInfo{Component: "test", Command: "work"},
		Options{Directories: &paths.Directories{State: t.TempDir()}},
		func(err error) {
			t.Error(err)
		},
	)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	workerDone := make(chan struct{})
	registered := false
	var releaseOnce, finishOnce sync.Once
	releaseWork := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	finish := func() {
		finishOnce.Do(func() {
			go func() {
				s.Finish(ctx, logging.CommandResult{})
				close(finished)
			}()
		})
	}
	t.Cleanup(func() {
		releaseWork()
		finish()
		if registered {
			select {
			case <-workerDone:
			case <-time.After(3 * time.Second):
				t.Error("已登记 worker 未完成收尾")
			}
		}
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("已登记工作或 Session 未完成收尾")
		}
	})
	registered = s.Go(ctx, func(ctx context.Context) {
		defer close(workerDone)
		_, span := s.Tracer.Start(ctx, "worker")
		defer span.End()
		close(started)
		<-release
		s.Logger.WithContext(ctx).Infow("worker_completed")
	})
	require.True(t, registered)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("登记的工作未启动")
	}
	finish()
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.finishing
	}, time.Second, time.Millisecond)
	require.False(t, s.Go(ctx, func(context.Context) {
		t.Error("关闭后不应接受新工作")
	}))
	select {
	case <-finished:
		t.Fatal("业务尚未结束就关闭资源")
	default:
	}
	require.True(t, trace.SpanFromContext(ctx).IsRecording())
	releaseWork()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("工作结束后未完成清理")
	}
	require.False(t, trace.SpanFromContext(ctx).IsRecording())
	data, err := os.ReadFile(filepath.Join(s.files.Path, diagnostics.EventsFile))
	require.NoError(t, err)
	workerIndex := bytes.Index(data, []byte("worker_completed"))
	finishedIndex := bytes.Index(data, []byte("command_finished"))
	require.NotEqual(t, -1, workerIndex, "缺少工作完成事件")
	require.NotEqual(t, -1, finishedIndex, "缺少命令完成事件")
	require.Less(t, workerIndex, finishedIndex)
	spans, err := os.ReadFile(filepath.Join(s.files.Path, diagnostics.TraceFile))
	require.NoError(t, err)
	require.Contains(t, string(spans), "worker")
}

func TestStderrIsBorrowedAndNeverSyncedOrClosed(t *testing.T) {
	cfg := defaultConfig()
	cfg.Logging.Output = "stderr"
	sink := &countingSink{}
	ctx, s := Begin(t.Context(), cfg, logging.RunInfo{}, Options{Stderr: sink}, func(err error) {
		t.Error(err)
	})
	s.Finish(ctx, logging.CommandResult{})
	require.Zero(t, sink.syncs)
	require.Contains(t, sink.String(), "command_finished")
	require.Nil(t, s.files)
}

var errTestWrite = errors.New("diagnostic write failed")

type failedDiagnosticWriter struct{}

func (failedDiagnosticWriter) Write([]byte) (int, error) {
	return 0, errTestWrite
}

func TestCorruptCatalogProcess(t *testing.T) {
	if os.Getenv("TEST_CORRUPT_I18N_CATALOG") != "1" {
		t.Skip("仅由损坏目录子进程执行")
	}
	cfg := defaultConfig()
	cfg.Logging.Output = "stderr"
	var sink bytes.Buffer
	var warnings []error
	ctx, s := Begin(t.Context(), cfg, logging.RunInfo{}, Options{Stderr: &sink}, func(err error) {
		warnings = append(warnings, err)
	})
	businessErr := (fault.Definition{Code: "example.business", Kind: fault.Runtime}).New()
	s.Finish(ctx, logging.CommandResult{ExitCode: 1, Err: businessErr})
	require.Len(t, warnings, 1)
	require.Error(t, warnings[0])
	lines := bytes.Split(bytes.TrimSpace(sink.Bytes()), []byte("\n"))
	require.Len(t, lines, 2)
	var start, finish map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &start))
	require.NoError(t, json.Unmarshal(lines[1], &finish))
	require.Equal(t, "Operation failed [session.started]", start["msg"])
	require.Equal(t, "Operation failed [session.finished]", finish["msg"])
	require.Equal(t, float64(1), finish["exit_code"])
	require.Equal(t, "example.business", finish["error_code"])

	warnings = nil
	ctx, s = Begin(t.Context(), cfg, logging.RunInfo{}, Options{Stderr: failedDiagnosticWriter{}}, func(err error) {
		warnings = append(warnings, err)
	})
	s.Finish(ctx, logging.CommandResult{})
	require.Len(t, warnings, 2, "目录加载失败不得吞掉后续日志写入故障")
	require.Contains(t, warnings[1].Error(), errTestWrite.Error())

	bundle, err := messages.NewBundle(
		fstest.MapFS{
			"active.en.toml": {
				Data: []byte(
					"[\"session.started\"]\nother = \"Injected start\"\n[\"session.finished\"]\nother = \"Injected finish\"\n",
				),
			},
		},
	)
	require.NoError(t, err)
	sink.Reset()
	warnings = nil
	ctx, s = Begin(
		t.Context(),
		cfg,
		logging.RunInfo{},
		Options{Stderr: &sink, Localizer: bundle.Localizer("en")},
		func(err error) {
			warnings = append(warnings, err)
		},
	)
	s.Finish(ctx, logging.CommandResult{})
	require.Empty(t, warnings)
	require.Contains(t, sink.String(), "Injected start")
	require.Contains(t, sink.String(), "Injected finish")
}

func TestTraceWarningAndStderrLogsShareSerialization(t *testing.T) {
	cfg := defaultConfig()
	cfg.Logging.Output = "stderr"
	cfg.Telemetry.Exporter = telemetry.File
	cfg.Telemetry.BatchSize = 1
	var sink bytes.Buffer
	var warnings atomic.Int32
	ctx, s := Begin(
		t.Context(),
		cfg,
		logging.RunInfo{},
		Options{Directories: &paths.Directories{State: t.TempDir()}, Stderr: &sink},
		func(err error) {
			_, _ = fmt.Fprintln(&sink, "测试警告", err)
			warnings.Add(1)
		},
	)
	w, err := s.files.Writer(diagnostics.TraceFile)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, span := s.Tracer.Start(ctx, "failed-export")
	span.End()
	for range 10000 {
		s.Logger.Infow("业务日志")
		if warnings.Load() > 0 {
			break
		}
	}
	s.Finish(ctx, logging.CommandResult{})
	require.Equal(t, int32(1), warnings.Load())
	require.Equal(t, 1, strings.Count(sink.String(), "测试警告"))
	for line := range strings.SplitSeq(strings.TrimSpace(sink.String()), "\n") {
		if !strings.HasPrefix(line, "测试警告 ") {
			require.True(t, json.Valid([]byte(line)), "stderr 日志被并发写入破坏：%q", line)
		}
	}
}

func TestRegisteredWorkReceivesExplicitContextAndCancellation(t *testing.T) {
	cfg := defaultConfig()
	cfg.Logging.Output = "none"
	finishCtx, s := Begin(t.Context(), cfg, logging.RunInfo{}, Options{}, func(err error) {
		t.Error(err)
	})
	defer s.Finish(finishCtx, logging.CommandResult{})
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	received := make(chan context.Context, 1)
	stopped := make(chan error, 1)
	require.False(t, s.Go(ctx, nil))
	require.True(t, s.Go(ctx, func(workerCtx context.Context) {
		received <- workerCtx
		<-workerCtx.Done()
		stopped <- context.Cause(workerCtx)
	}))
	select {
	case workerCtx := <-received:
		require.Same(t, ctx, workerCtx)
	case <-time.After(time.Second):
		t.Fatal("工作未收到显式 Context")
	}
	cause := errors.New("工作已取消")
	cancel(cause)
	select {
	case err := <-stopped:
		require.ErrorIs(t, err, cause)
	case <-time.After(time.Second):
		t.Fatal("工作未收到取消原因")
	}
}

func defaultConfig() appconfig.Config {
	return appconfig.Config{
		Logging:     logging.Defaults(),
		Diagnostics: diagnostics.Defaults(),
		Telemetry:   telemetry.Defaults(),
	}
}
