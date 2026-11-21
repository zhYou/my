package target

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
)

func TestUpdateScopePreservesTargetContent(t *testing.T) {
	_, repository, service := capturedTopgrade(t, "verbose = true\n")
	dataPath := "configs/targets/topgrade/data/topgrade.toml"
	beforeData := repository.snapshot.Files[dataPath]
	beforeBaseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, Topgrade)
	require.NoError(t, err)
	require.True(t, exists)

	result, err := service.Update(t.Context(), UpdateOptions{
		Target: Topgrade,
		Scope:  MachineScope{IDs: []string{"laptop"}},
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, Topgrade, result.Target)
	require.Equal(t, []string{"laptop"}, result.Scope)
	require.Len(t, repository.commits, 1)
	require.Equal(t, beforeData, repository.snapshot.Files[dataPath])

	remote, err := parseRemoteTargets(repository.snapshot, service.definitions())
	require.NoError(t, err)
	require.Equal(t, MachineScope{IDs: []string{"laptop"}}, remote[Topgrade].scope)

	afterBaseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, Topgrade)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, beforeBaseline, afterBaseline)
}

func TestUpdateUnchangedScopeDoesNotCommit(t *testing.T) {
	t.Run("all machines", func(t *testing.T) {
		_, repository, service := capturedTopgrade(t, "verbose = true\n")
		before := repository.snapshot.Metadata

		result, err := service.Update(t.Context(), UpdateOptions{
			Target: Topgrade,
			Scope:  MachineScope{All: true},
		})
		require.NoError(t, err)
		require.False(t, result.Changed)
		require.Equal(t, []string{"*"}, result.Scope)
		require.Equal(t, before, result.Snapshot)
		require.Empty(t, repository.commits)
	})

	t.Run("same machine set in another order", func(t *testing.T) {
		_, repository, service := capturedTopgrade(t, "verbose = true\n")
		setRemoteTargetScope(t, repository, Topgrade, MachineScope{IDs: []string{"laptop", "workstation"}})
		repository.snapshot.Metadata.Commit = "scope-change"
		before := repository.snapshot.Metadata

		result, err := service.Update(t.Context(), UpdateOptions{
			Target: Topgrade,
			Scope:  MachineScope{IDs: []string{"workstation", "laptop"}},
		})
		require.NoError(t, err)
		require.False(t, result.Changed)
		require.Equal(t, []string{"laptop", "workstation"}, result.Scope)
		require.Equal(t, before, result.Snapshot)
		require.Empty(t, repository.commits)
	})
}

func TestUpdateAllMachinesRestoresOutOfScopeTarget(t *testing.T) {
	_, repository, service := capturedTopgrade(t, "verbose = true\n")
	dataPath := "configs/targets/topgrade/data/topgrade.toml"
	beforeData := repository.snapshot.Files[dataPath]
	_, err := service.Update(t.Context(), UpdateOptions{
		Target: Topgrade,
		Scope:  MachineScope{IDs: []string{"laptop"}},
	})
	require.NoError(t, err)
	require.Empty(t, repository.commits[1:])

	result, err := service.Update(t.Context(), UpdateOptions{
		Target: Topgrade,
		Scope:  MachineScope{All: true},
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, []string{"*"}, result.Scope)
	require.Equal(t, beforeData, repository.snapshot.Files[dataPath])
}

func TestUpdateUnmanagedTargetDoesNotCommit(t *testing.T) {
	home := t.TempDir()
	localPath := filepath.Join(home, ".config", "topgrade.toml")
	writeFixture(t, localPath, "verbose = true\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)

	_, err := service.Update(t.Context(), UpdateOptions{
		Target: Topgrade,
		Scope:  MachineScope{All: true},
	})
	require.Error(t, err)
	details := fault.Details(err)
	require.Len(t, details, 1)
	require.Equal(t, fault.Code("target.not_managed"), details[0].Code)
	diagnostic, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Equal(t, "target.recover_not_managed", diagnostic.Recovery.Code)
	require.Equal(t, "topgrade", diagnostic.Recovery.Params["Target"])
	require.Empty(t, repository.commits)
	require.Equal(t, "verbose = true\n", readFixture(t, localPath))
}

func TestUpdateRejectsInvalidScopeBeforeCommit(t *testing.T) {
	_, repository, service := capturedTopgrade(t, "verbose = true\n")
	before := repository.snapshot
	cases := map[string]MachineScope{
		"empty":          {},
		"all with ids":   {All: true, IDs: []string{"laptop"}},
		"duplicate":      {IDs: []string{"laptop", "laptop"}},
		"unknown":        {IDs: []string{"desktop"}},
		"invalid syntax": {IDs: []string{"INVALID"}},
	}
	for name, scope := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := service.Update(t.Context(), UpdateOptions{
				Target: Topgrade,
				Scope:  scope,
			})
			require.Error(t, err)
			require.Equal(t, fault.Usage, fault.KindOf(err))
			require.Equal(t, fault.Code("target.invalid_input"), fault.Details(err)[0].Code)
			require.Empty(t, repository.commits)
			require.Equal(t, before, repository.snapshot)
		})
	}
}

func TestUpdateFieldScopePreservesValuesFile(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "mise", "config.toml")
	writeFixture(t, configPath, "[tools]\ngo = \"latest\"\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{Mise}})
	require.NoError(t, err)
	valuesPath := "configs/targets/mise/data/values.toml"
	beforeData := repository.snapshot.Files[valuesPath]
	before, err := parseRemoteTargets(repository.snapshot, service.definitions())
	require.NoError(t, err)
	repository.commits = nil

	result, err := service.Update(t.Context(), UpdateOptions{
		Target: Mise,
		Scope:  MachineScope{IDs: []string{"laptop"}},
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Len(t, repository.commits, 1)
	require.Equal(t, beforeData, repository.snapshot.Files[valuesPath])
	after, err := parseRemoteTargets(repository.snapshot, service.definitions())
	require.NoError(t, err)
	require.Equal(t, before[Mise].units, after[Mise].units)
	require.Equal(t, MachineScope{IDs: []string{"laptop"}}, after[Mise].scope)
}

func TestUpdateSecretScopePreservesVersionRecipientAndCiphertext(t *testing.T) {
	_, repository, service := capturedCodexAuth(t)
	before, err := parseRemoteTargets(repository.snapshot, service.definitions())
	require.NoError(t, err)
	beforeSecret := before[CodexAuth]

	result, err := service.Update(t.Context(), UpdateOptions{
		Target: CodexAuth,
		Scope:  MachineScope{IDs: []string{"laptop"}},
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, []string{"laptop"}, result.Scope)

	after, err := parseRemoteTargets(repository.snapshot, service.definitions())
	require.NoError(t, err)
	afterSecret := after[CodexAuth]
	require.Equal(t, MachineScope{IDs: []string{"laptop"}}, afterSecret.scope)
	require.Equal(t, beforeSecret.version, afterSecret.version)
	require.Equal(t, beforeSecret.recipient, afterSecret.recipient)
	require.Equal(t, beforeSecret.files, afterSecret.files)
}

func TestUpdateRejectsRemoteAuthoritativeTarget(t *testing.T) {
	home := t.TempDir()
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	repository.snapshot.ConfigInitialized = true
	repository.snapshot.Files["configs/manifest.toml"] = []byte("schema_version = 1\n")
	repository.snapshot.Files["configs/targets/shell-env/target.toml"] = []byte(
		"schema_version = 1\ntarget = \"shell-env\"\nmachines = [\"*\"]\n",
	)
	envPath := "configs/targets/shell-env/data/env.toml"
	envData := []byte("schema_version = 1\n[env.EDITOR]\nvalue = \"nvim\"\nmachines = [\"*\"]\n")
	repository.snapshot.Files[envPath] = envData
	service := newTestService(home, repository)

	_, err := service.Update(t.Context(), UpdateOptions{
		Target: ShellEnv,
		Scope:  MachineScope{IDs: []string{"laptop"}},
	})
	require.Error(t, err)
	require.Equal(t, fault.Usage, fault.KindOf(err))
	require.Equal(t, fault.Code("target.invalid_input"), fault.Details(err)[0].Code)
	require.Empty(t, repository.commits)
	require.Equal(t, envData, repository.snapshot.Files[envPath])
}

func setRemoteTargetScope(
	t *testing.T,
	repository *memoryRepository,
	id ID,
	scope MachineScope,
) {
	t.Helper()
	metadata, err := encodeRemoteMetadata(id, scope)
	require.NoError(t, err)
	repository.snapshot.Files[remoteTargetMetadataPath(id)] = metadata
}
