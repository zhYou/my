package logging_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/logging"
)

type featureEvent struct {
	count int
}

func (e featureEvent) Emit(log *logging.Logger) {
	log.Infow("示例业务事件", "event", "feature_finished", "count", e.count)
}

func TestOtherPackagesCanExtendEventsWithoutBackendImports(t *testing.T) {
	var out bytes.Buffer
	log := logging.New(logging.RunInfo{}, logging.Options{Config: logging.Defaults(), Writer: &out}, nil)
	log.Record(featureEvent{count: 3})
	var row map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &row))
	require.Equal(t, "feature_finished", row["event"])
	require.Equal(t, float64(3), row["count"])
}
