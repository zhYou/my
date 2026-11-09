package configuration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTargetIDsAndUnitIDs(t *testing.T) {
	for _, id := range []TargetID{
		TargetCodexSettings,
		TargetCodexRules,
		TargetCodexDeepSeek,
		TargetCodexAuth,
		TargetMise,
		TargetTopgrade,
		TargetClashGlobal,
		TargetClashLAN,
		TargetShellEnv,
		TargetShellEnvSecret,
	} {
		require.NoError(t, id.Validate())
	}
	require.Error(t, TargetID("../escape").Validate())
	require.Error(t, TargetID("Codex").Validate())

	require.Equal(t, UnitID("field:tools.go"), FieldUnitID("tools", "go"))
	require.Equal(t, UnitID("file:go.rules"), FileUnitID("go.rules"))
	require.Equal(t, SecretUnitID, UnitID("secret:current"))
	require.Error(t, FieldUnitID("tools", "../go").Validate())
	require.Error(t, FileUnitID("../go.rules").Validate())
}

func TestMachineScope(t *testing.T) {
	all := MachineScope{All: true}
	require.True(t, all.Applies("workstation"))

	selected := MachineScope{IDs: []string{"workstation", "laptop"}}
	require.True(t, selected.Applies("laptop"))
	require.False(t, selected.Applies("server"))
	require.Error(t, selected.Validate([]string{"workstation"}))

	require.NoError(t, (MachineScope{IDs: []string{"workstation"}}).Validate([]string{"workstation", "laptop"}))
	require.False(t, (MachineScope{All: true, IDs: []string{"workstation"}}).Applies("workstation"))
}
