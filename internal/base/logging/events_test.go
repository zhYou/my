package logging

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfiguredLevelRetainsFailedCommandWithoutErrorCause(t *testing.T) {
	var output bytes.Buffer
	logger := New(RunInfo{Component: "my", Command: "greet"}, Options{
		Config: Config{Level: "error"},
		Writer: &output,
		RunID:  "fixture",
	}, nil)
	logger.Infow("filtered")
	logger.Record(CommandResult{ExitCode: 1, Duration: time.Second})
	require.NoError(t, logger.Sync())
	b := output.Bytes()
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	require.Len(t, lines, 1)
	var e map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &e))
	require.Equal(t, "error", e["level"])
	require.Equal(t, float64(1), e["exit_code"])
}
