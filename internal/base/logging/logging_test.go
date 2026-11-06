package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type failedSink struct{}

func (failedSink) Write([]byte) (int, error) {
	return 0, errors.New("fixture write failure")
}

func (failedSink) Sync() error {
	return errors.New("fixture sync failure")
}

func TestWriterFailuresReachReporterAndSyncCaller(t *testing.T) {
	warnings := 0
	logger := New(RunInfo{}, Options{
		Config: Defaults(),
		Writer: failedSink{},
		RunID:  "fixture",
	}, func(error) {
		warnings++
	})
	logger.Infow("cannot write")
	require.Error(t, logger.Sync())
	require.Positive(t, warnings)
}

type countedSink struct {
	bytes.Buffer
	syncs int
}

func (s *countedSink) Sync() error {
	s.syncs++
	return nil
}

func TestNewWritesBoundFieldsAndPreservesWriterSync(t *testing.T) {
	sink := &countedSink{}
	logger := New(RunInfo{Component: "my", Command: "doctor", Version: "fixture"}, Options{
		Config: Defaults(),
		Writer: sink,
		RunID:  "fixture-run",
	}, func(err error) {
		t.Error(err)
	})
	logger.Infow("fixture", "tool", "git")
	require.NoError(t, logger.Sync())
	require.Equal(t, 1, sink.syncs)
	var entry map[string]any
	require.NoError(t, json.Unmarshal(sink.Bytes(), &entry))
	require.Equal(t, "fixture-run", entry["run_id"])
	require.Equal(t, "my", entry["component"])
	require.Equal(t, "doctor", entry["command"])
	require.Equal(t, "fixture", entry["version"])
	require.Equal(t, "git", entry["tool"])
}

func TestNewWithoutWriterReturnsUsableFallback(t *testing.T) {
	warnings := 0
	logger := New(RunInfo{}, Options{Config: Defaults(), RunID: "fixture"}, func(error) {
		warnings++
	})
	logger.Infow("discarded")
	require.NoError(t, logger.Sync())
	require.Equal(t, 1, warnings)
}
