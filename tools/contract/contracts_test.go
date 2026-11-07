package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/tools/internal/contract"
)

func TestBaselineRefreshStillRequiresChangeRecord(t *testing.T) {
	old := []byte(`{"result":{"message":"string"}}`)
	updated := []byte(`{"result":{"message":"number"}}`)
	require.Error(t, validateContract(old, updated, updated, nil, false))
	record := contract.Record{
		ID:              "change",
		Compatibility:   "compatible",
		Summary:         "改变类型",
		Paths:           []string{"/result/message"},
		PreviousVersion: "v1.2.0",
		TargetVersion:   "v1.3.0",
		Migration:       "修改消费方",
	}
	require.Error(t, validateContract(old, updated, updated, []contract.Record{record}, false))
	record.Compatibility = "breaking"
	record.TargetVersion = "v2.0.0"
	require.NoError(t, validateContract(old, updated, updated, []contract.Record{record}, false))
	added := []byte(`{"result":{"message":"string"},"result_schema":{"type":"object"}}`)
	record.Compatibility = "compatible"
	record.Paths = []string{"/result_schema"}
	record.TargetVersion = "v1.3.0"
	require.NoError(t, validateContract(old, added, added, []contract.Record{record}, false))
}

func TestCaptureIncludesDiagnosticShapes(t *testing.T) {
	data, err := captureContract(t.Context())
	require.NoError(t, err)
	var captured struct {
		Results struct {
			Diagnostics map[string]map[string]any `json:"diagnostics"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(data, &captured))
	require.NotEmpty(t, captured.Results.Diagnostics)
	require.Equal(t, "number", captured.Results.Diagnostics["error"]["exit_code"])
	require.Equal(t, "array", captured.Results.Diagnostics["warning"]["diagnostics"])
}
