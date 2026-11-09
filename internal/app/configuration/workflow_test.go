package configuration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/datarepo"
)

type memoryRepository struct {
	snapshot datarepo.Snapshot
	commits  []datarepo.Changes
}

func (r *memoryRepository) Read(context.Context) (datarepo.Snapshot, error) {
	return cloneSnapshot(r.snapshot), nil
}

func (r *memoryRepository) CommitChanges(
	_ context.Context,
	snapshot datarepo.Snapshot,
	changes datarepo.Changes,
) (datarepo.Metadata, error) {
	if snapshot.Metadata.Commit != r.snapshot.Metadata.Commit {
		return snapshot.Metadata, problem(codeStateInvalid, phaseStateWrite, nil)
	}
	r.commits = append(r.commits, cloneChanges(changes))
	for _, name := range changes.Deletions {
		delete(r.snapshot.Files, name)
	}
	for name, data := range changes.Files {
		r.snapshot.Files[name] = append([]byte(nil), data...)
	}
	r.snapshot.Metadata.Commit = fmt.Sprintf("commit-%d", len(r.commits))
	r.snapshot.Initialized = true
	r.snapshot.ConfigInitialized = true
	return r.snapshot.Metadata, nil
}

func TestCaptureCreatesTargetAndBaseline(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = true\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)

	result, err := service.Capture(t.Context(), CaptureOptions{Targets: []TargetID{TargetTopgrade}})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Contains(t, repository.snapshot.Files, "configs/manifest.toml")
	require.Contains(t, repository.snapshot.Files, "configs/targets/topgrade/target.toml")
	require.Equal(
		t,
		"verbose = true\n",
		string(repository.snapshot.Files["configs/targets/topgrade/data/topgrade.toml"]),
	)

	baseline, exists, err := service.options.State.Read(
		repository.snapshot.Metadata,
		TargetTopgrade,
	)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, baseline.Units[FileUnitID("topgrade.toml")].Exists)
}

func TestSyncAppliesRemoteAndUploadsLocal(t *testing.T) {
	t.Run("remote", func(t *testing.T) {
		home, repository, service := capturedTopgrade(t, "verbose = true\n")
		repository.snapshot.Metadata.Commit = "remote-change"
		repository.snapshot.Files["configs/targets/topgrade/data/topgrade.toml"] = []byte("verbose = false\n")

		result, err := service.Sync(t.Context(), SyncOptions{})
		require.NoError(t, err)
		require.True(t, result.Changed)
		require.Equal(t, "verbose = false\n", readFixture(t, filepath.Join(home, ".config", "topgrade.toml")))
		require.Empty(t, repository.commits)
	})

	t.Run("local", func(t *testing.T) {
		home, repository, service := capturedTopgrade(t, "verbose = true\n")
		writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = false\n")

		result, err := service.Sync(t.Context(), SyncOptions{})
		require.NoError(t, err)
		require.True(t, result.Changed)
		require.Len(t, repository.commits, 1)
		require.Equal(
			t,
			"verbose = false\n",
			string(repository.snapshot.Files["configs/targets/topgrade/data/topgrade.toml"]),
		)
	})
}

func TestSyncConflictDoesNotWriteAnySide(t *testing.T) {
	home, repository, service := capturedTopgrade(t, "verbose = true\n")
	localPath := filepath.Join(home, ".config", "topgrade.toml")
	writeFixture(t, localPath, "verbose = \"local\"\n")
	repository.snapshot.Metadata.Commit = "remote-change"
	repository.snapshot.Files["configs/targets/topgrade/data/topgrade.toml"] = []byte("verbose = \"remote\"\n")

	_, err := service.Sync(t.Context(), SyncOptions{})
	require.Error(t, err)
	conflicts := &ConflictError{}
	require.ErrorAs(t, err, &conflicts)
	require.Len(t, conflicts.Conflicts, 1)
	require.Empty(t, repository.commits)
	require.Equal(t, "verbose = \"local\"\n", readFixture(t, localPath))
}

func TestSyncSkipsRemoteAuthoritativeEnvironmentTarget(t *testing.T) {
	home, repository, service := capturedTopgrade(t, "verbose = true\n")
	repository.snapshot.Files["configs/targets/shell-env/target.toml"] = []byte(
		"schema_version = 1\ntarget = \"shell-env\"\nmachines = [\"*\"]\n",
	)
	repository.snapshot.Files["configs/targets/shell-env/data/env.toml"] = []byte(
		"schema_version = 1\n[env.EDITOR]\nvalue = \"nvim\"\nmachines = [\"*\"]\n",
	)

	result, err := service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, "verbose = true\n", readFixture(t, filepath.Join(home, ".config", "topgrade.toml")))
}

func TestSyncRejectsUnknownConfigurationTarget(t *testing.T) {
	home := t.TempDir()
	snapshot := initializedSnapshot()
	snapshot.ConfigInitialized = true
	snapshot.Files["configs/manifest.toml"] = []byte("schema_version = 1\n")
	snapshot.Files["configs/targets/unknown/target.toml"] = []byte(
		"schema_version = 1\ntarget = \"unknown\"\nmachines = [\"*\"]\n",
	)
	service := newTestService(home, &memoryRepository{snapshot: snapshot})

	_, err := service.Sync(t.Context(), SyncOptions{})
	require.Error(t, err)
}

func TestResolveRemoteAndLocal(t *testing.T) {
	t.Run("remote", func(t *testing.T) {
		home, repository, service := conflictingTopgrade(t)
		localPath := filepath.Join(home, ".config", "topgrade.toml")

		result, err := service.Resolve(t.Context(), ResolveOptions{
			Target: TargetTopgrade,
			Unit:   FileUnitID("topgrade.toml"),
			From:   ResolveRemote,
		})
		require.NoError(t, err)
		require.True(t, result.Changed)
		require.Equal(t, "verbose = \"remote\"\n", readFixture(t, localPath))
		require.Empty(t, repository.commits)
	})

	t.Run("local", func(t *testing.T) {
		home, repository, service := conflictingTopgrade(t)
		localPath := filepath.Join(home, ".config", "topgrade.toml")
		require.Equal(t, "verbose = \"local\"\n", readFixture(t, localPath))

		result, err := service.Resolve(t.Context(), ResolveOptions{
			Target: TargetTopgrade,
			Unit:   FileUnitID("topgrade.toml"),
			From:   ResolveLocal,
		})
		require.NoError(t, err)
		require.True(t, result.Changed)
		require.Len(t, repository.commits, 1)
		require.Equal(
			t,
			"verbose = \"local\"\n",
			string(repository.snapshot.Files["configs/targets/topgrade/data/topgrade.toml"]),
		)
	})
}

func TestRemoveDeletesRemoteTargetAndKeepsLocalFile(t *testing.T) {
	home, repository, service := capturedTopgrade(t, "verbose = true\n")
	localPath := filepath.Join(home, ".config", "topgrade.toml")

	result, err := service.Remove(t.Context(), RemoveOptions{Targets: []TargetID{TargetTopgrade}})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.NotContains(t, repository.snapshot.Files, "configs/targets/topgrade/target.toml")
	require.Equal(t, "verbose = true\n", readFixture(t, localPath))
	_, exists, err := service.options.State.Read(repository.snapshot.Metadata, TargetTopgrade)
	require.NoError(t, err)
	require.False(t, exists)
}

func capturedTopgrade(t *testing.T, content string) (string, *memoryRepository, *Service) {
	t.Helper()
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), content)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []TargetID{TargetTopgrade}})
	require.NoError(t, err)
	repository.commits = nil
	return home, repository, service
}

func conflictingTopgrade(t *testing.T) (string, *memoryRepository, *Service) {
	t.Helper()
	home, repository, service := capturedTopgrade(t, "verbose = true\n")
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = \"local\"\n")
	repository.snapshot.Metadata.Commit = "remote-change"
	repository.snapshot.Files["configs/targets/topgrade/data/topgrade.toml"] = []byte("verbose = \"remote\"\n")
	return home, repository, service
}

func newTestService(home string, repository Repository) *Service {
	return New(Options{
		Repository:    repository,
		State:         NewFileState(filepath.Join(home, "state")),
		Home:          home,
		OS:            "linux",
		MachineID:     "workstation",
		KnownMachines: []string{"workstation", "laptop"},
	})
}

func initializedSnapshot() datarepo.Snapshot {
	return datarepo.Snapshot{
		Metadata: datarepo.Metadata{
			Repository:   "owner/data",
			RepositoryID: 42,
			Branch:       "main",
			Commit:       "base",
		},
		Files:       map[string][]byte{"my.toml": []byte("schema_version = 1\n")},
		Initialized: true,
	}
}

func cloneSnapshot(snapshot datarepo.Snapshot) datarepo.Snapshot {
	result := snapshot
	result.Files = make(map[string][]byte, len(snapshot.Files))
	for name, data := range snapshot.Files {
		result.Files[name] = append([]byte(nil), data...)
	}
	return result
}

func cloneChanges(changes datarepo.Changes) datarepo.Changes {
	result := changes
	result.Files = make(map[string][]byte, len(changes.Files))
	for name, data := range changes.Files {
		result.Files[name] = append([]byte(nil), data...)
	}
	result.Deletions = append([]string(nil), changes.Deletions...)
	return result
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // 路径来自 t.TempDir()。
	require.NoError(t, err)
	return string(data)
}
