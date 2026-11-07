package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zhYou/my/internal/base/fault"

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

func TestJoinedErrorsKeepSafeBranchDiagnostics(t *testing.T) {
	var out bytes.Buffer
	log := New(RunInfo{}, Options{Config: Defaults(), Writer: &out}, nil)
	cause := errors.New("private-fixture-cause")
	first := (fault.Definition{Code: "fixture.partial", Kind: fault.Runtime}).Wrap(
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
	second := (fault.Definition{Code: "fixture.other", Kind: fault.Runtime}).New(
		fault.WithDiagnostic(fault.Diagnostic{Phase: "fixture.cleanup"}),
	)
	log.Record(CommandResult{ExitCode: 1, Err: errors.Join(first, second)})
	require.NotContains(t, out.String(), "private-fixture")
	var record struct {
		Details []struct {
			Code      string
			Phase     string
			Completed []string `json:"completed_effects"`
		} `json:"error_details"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &record))
	require.Len(t, record.Details, 2)
	require.Equal(t, "fixture.publish", record.Details[0].Phase)
	require.Equal(t, []string{"fixture.saved"}, record.Details[0].Completed)
	require.Equal(t, "fixture.cleanup", record.Details[1].Phase)
	require.Nil(t, record.Details[1].Completed)
}
