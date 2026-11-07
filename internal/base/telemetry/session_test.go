package telemetry

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhYou/my/internal/base/fault"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestFileTracePreservesParentChildWithoutStdout(t *testing.T) {
	var out bytes.Buffer
	cfg := Defaults()
	cfg.Exporter = File
	session := Start(t.Context(), cfg, Info{Service: "my", RunID: "test"}, &out, func(err error) {
		t.Error(err)
	})
	ctx, root := session.Tracer.Start(t.Context(), "my.doctor")
	_, child := session.Tracer.Start(ctx, "doctor.check")
	child.End()
	root.End()
	require.NoError(t, session.Close(t.Context()))
	require.NoError(t, session.Close(t.Context()))
	require.Contains(t, out.String(), "my.doctor")
	require.Contains(t, out.String(), "doctor.check")
	require.Contains(t, out.String(), root.SpanContext().TraceID().String())
}

func TestFileTraceFlushesAfterCanceledBusinessContextAndFiltersResources(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "private.context=not-collected")
	var out bytes.Buffer
	cfg := Defaults()
	cfg.Exporter = File
	ctx, cancel := context.WithCancel(t.Context())
	s := Start(ctx, cfg, Info{Service: "my"}, &out, func(err error) {
		t.Error(err)
	})
	_, span := s.Tracer.Start(ctx, "fixture")
	span.End()
	cancel()
	require.NoError(t, s.Close(ctx))
	require.Contains(t, out.String(), "fixture")
	require.Contains(t, out.String(), "service.name")
	require.Contains(t, out.String(), "my")
	require.NotContains(t, out.String(), "not-collected")
}

func TestDisabledTraceIgnoresSDKEnvironment(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "invalid-private-fixture")
	var out bytes.Buffer
	s := Start(t.Context(), Defaults(), Info{}, &out, func(err error) {
		t.Error(err)
	})
	_, span := s.Tracer.Start(t.Context(), "disabled")
	require.False(t, span.IsRecording())
	require.False(t, trace.SpanContextFromContext(t.Context()).IsValid())
	span.End()
	require.NoError(t, s.Close(t.Context()))
	require.Empty(t, out.String())
}

func TestFileExporterFailureWarnsOnce(t *testing.T) {
	cfg := Defaults()
	cfg.Exporter = File
	cfg.BatchSize = 1
	var warnings atomic.Int32
	s := Start(t.Context(), cfg, Info{}, failingWriter{}, func(error) {
		warnings.Add(1)
	})
	for range 4 {
		_, span := s.Tracer.Start(t.Context(), "failed-export")
		span.End()
	}
	require.NoError(t, s.Close(t.Context()))
	require.Equal(t, int32(1), warnings.Load())
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture write failure")
}

func TestFileShutdownWaitIsBounded(t *testing.T) {
	writer := &blockingWriter{started: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() {
			close(writer.release)
		})
	})
	cfg := Defaults()
	cfg.Exporter = File
	cfg.BatchSize = 1
	cfg.ShutdownTimeout = 30 * time.Millisecond
	var warnings atomic.Int32
	s := Start(t.Context(), cfg, Info{}, writer, func(error) {
		warnings.Add(1)
	})
	_, span := s.Tracer.Start(t.Context(), "slow")
	span.End()
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("本地 Trace 写入未开始")
	}
	start := time.Now()
	require.ErrorIs(t, s.Close(t.Context()), context.DeadlineExceeded)
	require.Less(t, time.Since(start), 300*time.Millisecond)
	require.Equal(t, int32(1), warnings.Load())
	release.Do(func() {
		close(writer.release)
	})
	select {
	case <-writer.done:
	case <-time.After(time.Second):
		t.Fatal("本地 Trace 写入未结束")
	}
}

type blockingWriter struct {
	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

func (w *blockingWriter) Write(data []byte) (int, error) {
	close(w.started)
	<-w.release
	close(w.done)
	return len(data), nil
}

func TestGuardedExporterCancelsWhenSessionEnds(t *testing.T) {
	life, cancel := context.WithCancel(t.Context())
	defer cancel()
	target := &contextExporter{started: make(chan struct{}), canceled: make(chan struct{})}
	guarded := guardedExporter{
		SpanExporter: target,
		life:         life,
		warn:         func(error) {},
	}
	done := make(chan struct{})
	go func() {
		assert.NoError(t, guarded.ExportSpans(t.Context(), nil))
		close(done)
	}()
	select {
	case <-target.started:
	case <-time.After(time.Second):
		t.Fatal("Trace 导出未开始")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("会话结束未取消导出 Context")
	}
	select {
	case <-target.canceled:
	default:
		t.Fatal("导出未观察到 Context 取消")
	}
}

type contextExporter struct {
	started  chan struct{}
	canceled chan struct{}
}

func (e *contextExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	close(e.started)
	<-ctx.Done()
	close(e.canceled)
	return ctx.Err()
}

func (*contextExporter) Shutdown(context.Context) error {
	return nil
}

func TestErrorSpansContainSafeDiagnosticMetadata(t *testing.T) {
	var out bytes.Buffer
	cfg := Defaults()
	cfg.Exporter = File
	session := Start(t.Context(), cfg, Info{RunID: "fixture-run"}, &out, nil)
	_, span := session.Tracer.Start(t.Context(), "fixture")
	cause := errors.New("private-fixture-token")
	err := (fault.Definition{Code: "fixture.partial", Kind: fault.Runtime}).Wrap(
		cause,
		fault.WithSensitiveCause(),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            "fixture.publish",
				CompletedEffects: []string{"fixture.saved"},
				Recovery:         &fault.Recovery{Code: "config.recover"},
			},
		),
	)
	RecordError(span, err)
	span.End()
	require.NoError(t, session.Close(t.Context()))
	require.NotContains(t, out.String(), "private-fixture-token")
	require.Contains(t, out.String(), "error.phase")
	require.Contains(t, out.String(), "fixture.publish")
	require.Contains(t, out.String(), "fixture.saved")
	require.Contains(t, out.String(), "config.recover")
}
