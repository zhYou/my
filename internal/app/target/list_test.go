package target

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListShowsManagedLocalOutOfScopeAndHidesAbsent(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = true\n")
	writeFixture(
		t,
		filepath.Join(home, ".config", "clash-verge", "zeus-global-extend.js"),
		"console.log('local')\n",
	)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{Topgrade}})
	require.NoError(t, err)
	baselineCommit := repository.snapshot.Metadata.Commit
	repository.commits = nil
	setRemoteFieldTarget(
		t,
		repository,
		Mise,
		MachineScope{IDs: []string{"laptop", "desktop"}},
		map[UnitID]UnitValue{
			FieldUnitID("tools", "go"): UnitValueFromBytes([]byte(`"latest"`)),
		},
	)

	result, err := service.List(t.Context(), ListOptions{})
	require.NoError(t, err)
	require.Empty(t, repository.commits)
	require.Equal(t, baselineCommit, result.Snapshot.Commit)
	require.Equal(t, []ID{ClashGlobal, Mise, Topgrade}, listTargetIDs(result.Targets))
	require.Equal(
		t,
		[]ListStatus{StatusLocal, StatusOutOfScope, StatusManaged},
		listTargetStatuses(result.Targets),
	)

	topgrade := listItem(t, result.Targets, Topgrade)
	require.Equal(t, []string{"*"}, topgrade.Scope)
	require.Equal(t, baselineCommit, topgrade.BaselineCommit)
	require.Equal(t, 1, topgrade.Units)

	mise := listItem(t, result.Targets, Mise)
	require.Equal(t, []string{"desktop", "laptop"}, mise.Scope)
	require.Empty(t, mise.BaselineCommit)
	require.Equal(t, 1, mise.Units)

	clash := listItem(t, result.Targets, ClashGlobal)
	require.Empty(t, clash.Scope)
	require.NotNil(t, clash.Scope)
	require.Empty(t, clash.BaselineCommit)
	require.Zero(t, clash.Units)
	require.NotContains(t, listTargetIDs(result.Targets), CodexSettings)

	all, err := service.List(t.Context(), ListOptions{All: true})
	require.NoError(t, err)
	require.Len(t, all.Targets, len(DefaultDefinitions()))
	require.Equal(t, sortedDefinitionIDs(DefaultDefinitions()), listTargetIDs(all.Targets))
	absent := listItem(t, all.Targets, CodexSettings)
	require.Equal(t, StatusAbsent, absent.Status)
	require.Empty(t, absent.Scope)
	require.NotNil(t, absent.Scope)
	require.Empty(t, absent.BaselineCommit)
	require.Zero(t, absent.Units)
}

func TestListDoesNotCreateLocalState(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = true\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)

	result, err := service.List(t.Context(), ListOptions{All: true})
	require.NoError(t, err)
	require.Equal(t, StatusLocal, listItem(t, result.Targets, Topgrade).Status)
	_, err = os.Stat(filepath.Join(home, "state"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, repository.commits)
}

func setRemoteFieldTarget(
	t *testing.T,
	repository *memoryRepository,
	id ID,
	scope MachineScope,
	units map[UnitID]UnitValue,
) {
	t.Helper()
	metadata, err := encodeRemoteMetadata(id, scope)
	require.NoError(t, err)
	values, err := encodeFieldValues(units)
	require.NoError(t, err)
	repository.snapshot.Files[remoteTargetPrefix(id)+"/"+targetMetadataFile] = metadata
	repository.snapshot.Files[remoteTargetPrefix(id)+"/"+configDataDirectory+"/"+fieldValuesFile] = values
	repository.snapshot.ConfigInitialized = true
}

func listTargetIDs(items []ListItem) []ID {
	result := make([]ID, 0, len(items))
	for _, item := range items {
		result = append(result, item.Target)
	}
	return result
}

func listTargetStatuses(items []ListItem) []ListStatus {
	result := make([]ListStatus, 0, len(items))
	for _, item := range items {
		result = append(result, item.Status)
	}
	return result
}

func listItem(t *testing.T, items []ListItem, id ID) ListItem {
	t.Helper()
	for _, item := range items {
		if item.Target == id {
			return item
		}
	}
	t.Fatalf("清单中没有目标 %s", id)
	return ListItem{}
}

func sortedDefinitionIDs(definitions []Definition) []ID {
	result := make([]ID, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition.ID)
	}
	slices.Sort(result)
	return result
}
