package logging

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestChildLoggersKeepParentAndSiblingFieldsIndependent(t *testing.T) {
	var output bytes.Buffer
	backend := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.Lock(zapcore.AddSync(&output)), zap.InfoLevel)).
		Sugar()
	parent := &Logger{sugar: backend}
	var workers sync.WaitGroup
	for _, name := range []string{"go", "git"} {
		child := parent.With("tool", name)
		workers.Go(func() {
			child.Record(CommandResult{ExitCode: 0})
			child.Infow("diagnostic")
		})
	}
	workers.Wait()
	parent.Infow("parent")
	counts := map[string]int{}
	for line := range bytes.SplitSeq(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var row map[string]any
		require.NoError(t, json.Unmarshal(line, &row))
		if row["msg"] == "parent" {
			require.NotContains(t, row, "tool")
			continue
		}
		name, ok := row["tool"].(string)
		require.True(t, ok)
		counts[name]++
	}
	require.Equal(t, map[string]int{"go": 2, "git": 2}, counts)
	NewNop().Record(CommandResult{})
}

func TestDerivedTraceContextReplacesIdentifiersWithoutDuplicateJSONKeys(t *testing.T) {
	var output bytes.Buffer
	logger := New(
		RunInfo{Component: "my", Command: "greet"},
		Options{Config: Defaults(), Writer: &output, RunID: "fixture"},
		nil,
	)
	traceID, err := trace.TraceIDFromHex("11111111111111111111111111111111")
	require.NoError(t, err)
	parentID, err := trace.SpanIDFromHex("1111111111111111")
	require.NoError(t, err)
	childID, err := trace.SpanIDFromHex("2222222222222222")
	require.NoError(t, err)
	parentCtx := trace.ContextWithSpanContext(
		t.Context(),
		trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: parentID}),
	)
	childCtx := trace.ContextWithSpanContext(
		parentCtx,
		trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: childID}),
	)
	parent := logger.WithContext(parentCtx)
	parent.With("stage", "child").WithContext(childCtx).Infow("child")
	parent.Infow("parent")
	require.NoError(t, logger.Sync())
	data := output.Bytes()
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	require.Len(t, lines, 2)
	require.Equal(t, 1, bytes.Count(lines[0], []byte("\"span_id\"")))
	require.Equal(t, 1, bytes.Count(lines[0], []byte("\"trace_id\"")))
	var child, root map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &child))
	require.NoError(t, json.Unmarshal(lines[1], &root))
	require.Equal(t, "2222222222222222", child["span_id"])
	require.Equal(t, "1111111111111111", root["span_id"])
	require.NotContains(t, root, "stage")
}
