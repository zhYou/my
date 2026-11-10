package configuration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareThreeWayUnits(t *testing.T) {
	oldValue := UnitValueFromBytes([]byte("old"))
	localValue := UnitValueFromBytes([]byte("local"))
	remoteValue := UnitValueFromBytes([]byte("remote"))

	tests := []struct {
		name     string
		kind     UnitKind
		baseline *BaselineValue
		local    UnitValue
		remote   UnitValue
		action   Action
		code     string
	}{
		{
			name:   "first takeover equal values adopts baseline",
			kind:   UnitField,
			local:  oldValue,
			remote: oldValue,
			action: ActionAdopt,
		},
		{
			name:   "first takeover different values conflicts",
			kind:   UnitField,
			local:  localValue,
			remote: remoteValue,
			action: ActionConflict,
			code:   "configuration.first_takeover",
		},
		{
			name:     "remote only change applies locally",
			kind:     UnitField,
			baseline: baselineOf(oldValue),
			local:    oldValue,
			remote:   remoteValue,
			action:   ActionApplyRemote,
		},
		{
			name:     "local only change uploads",
			kind:     UnitField,
			baseline: baselineOf(oldValue),
			local:    localValue,
			remote:   oldValue,
			action:   ActionUploadLocal,
		},
		{
			name:     "same concurrent result converges",
			kind:     UnitField,
			baseline: baselineOf(oldValue),
			local:    localValue,
			remote:   localValue,
			action:   ActionConverged,
		},
		{
			name:     "different concurrent changes conflict",
			kind:     UnitField,
			baseline: baselineOf(oldValue),
			local:    localValue,
			remote:   remoteValue,
			action:   ActionConflict,
			code:     "configuration.changed_both",
		},
		{
			name:     "missing whole file requires explicit resolution",
			kind:     UnitFile,
			baseline: baselineOf(oldValue),
			local:    UnitValue{},
			remote:   oldValue,
			action:   ActionConflict,
			code:     "configuration.file_missing",
		},
		{
			name:     "missing field uploads deletion",
			kind:     UnitField,
			baseline: baselineOf(oldValue),
			local:    UnitValue{},
			remote:   oldValue,
			action:   ActionUploadLocal,
		},
		{
			name:     "secret remains opaque and applies remote version",
			kind:     UnitSecret,
			baseline: baselineOf(oldValue),
			local:    oldValue,
			remote:   remoteValue,
			action:   ActionApplyRemote,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comparison := Compare(tc.kind, tc.baseline, tc.local, tc.remote)
			require.Equal(t, tc.action, comparison.Action)
			require.Equal(t, tc.code, comparison.Code)
		})
	}
}

func TestBaselineValueNeverKeepsSecretContent(t *testing.T) {
	secret := UnitValueFromBytes([]byte("secret-marker"))
	baseline := BaselineValue{
		Exists: true,
		Secret: true,
		Digest: secret.Digest,
	}
	require.Empty(t, baseline.Data)
	require.NotEmpty(t, baseline.Digest)
}

func baselineOf(value UnitValue) *BaselineValue {
	return &BaselineValue{
		Exists: value.Exists,
		Digest: value.Digest,
		Data:   value.Data,
	}
}
