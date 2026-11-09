package shellenv

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/agecrypt"
	"github.com/zhYou/my/internal/datarepo"
)

type memoryRepository struct {
	snapshot datarepo.Snapshot
	commits  int
	reads    int
}

func (r *memoryRepository) Read(context.Context) (datarepo.Snapshot, error) {
	r.reads++
	return cloneSnapshot(r.snapshot), nil
}

func (r *memoryRepository) CommitChanges(
	_ context.Context,
	snapshot datarepo.Snapshot,
	changes datarepo.Changes,
) (datarepo.Metadata, error) {
	if snapshot.Metadata.Commit != r.snapshot.Metadata.Commit {
		return snapshot.Metadata, fmt.Errorf("cas conflict")
	}
	r.commits++
	for name, data := range changes.Files {
		r.snapshot.Files[name] = append([]byte(nil), data...)
	}
	for _, name := range changes.Deletions {
		delete(r.snapshot.Files, name)
	}
	r.snapshot.Metadata.Commit = fmt.Sprintf("commit-%d", r.commits)
	r.snapshot.ConfigInitialized = true
	return r.snapshot.Metadata, nil
}

func TestSetOrdinaryEnvironmentWritesRepositoryAndOfflineActivate(t *testing.T) {
	service, repository := newEnvironmentService(t, "workstation")
	result, err := service.Set(t.Context(), SetOptions{
		Name:  "EDITOR",
		Value: []byte("nvim"),
	})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, 1, repository.commits)
	require.Contains(t, repository.snapshot.Files, "configs/targets/shell-env/data/env.toml")

	repository.reads = 0
	output, err := service.Activate(t.Context(), "bash")
	require.NoError(t, err)
	require.Equal(t, "export EDITOR='nvim'\n", output)
	require.Zero(t, repository.reads)
}

func TestSetSecretEnvironmentEncryptsValueAndListHidesValue(t *testing.T) {
	service, repository := newEnvironmentService(t, "workstation")
	_, err := service.Set(t.Context(), SetOptions{
		Name:   "DEEPSEEK_API_KEY",
		Secret: true,
		Value:  []byte("secret-marker"),
	})
	require.NoError(t, err)
	require.NotContains(
		t,
		string(repository.snapshot.Files["configs/targets/shell-env-secret/data/env.toml.age"]),
		"secret-marker",
	)

	list, err := service.List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Variable{{
		Name: "DEEPSEEK_API_KEY", Secret: true, Machines: []string{"*"},
	}}, list.Variables)
	output, err := service.Activate(t.Context(), "bash")
	require.NoError(t, err)
	require.Contains(t, output, "export DEEPSEEK_API_KEY='secret-marker'")
	require.NotContains(t, fmt.Sprint(list), "secret-marker")
}

func TestSetRejectsDuplicateAcrossKindsWithoutOverwrite(t *testing.T) {
	service, _ := newEnvironmentService(t, "workstation")
	_, err := service.Set(t.Context(), SetOptions{Name: "EXAMPLE_KEY", Value: []byte("one")})
	require.NoError(t, err)
	_, err = service.Set(t.Context(), SetOptions{
		Name:      "EXAMPLE_KEY",
		Secret:    true,
		Value:     []byte("two"),
		Overwrite: true,
	})
	require.Error(t, err)
}

func TestSyncCacheCopiesSecretCiphertextWithoutAgeIdentity(t *testing.T) {
	service, repository := newEnvironmentService(t, "workstation")
	_, err := service.Set(t.Context(), SetOptions{
		Name: "DEEPSEEK_API_KEY", Secret: true, Value: []byte("secret-marker"),
	})
	require.NoError(t, err)
	cache := filepath.Join(t.TempDir(), "cache")
	download := New(Options{
		Repository: repository,
		Cache:      NewFileCache(cache),
		MachineID:  "workstation",
	})
	require.NoError(t, download.SyncCache(t.Context()))
	activated := New(Options{
		Cache:     NewFileCache(cache),
		AgeKey:    service.options.AgeKey,
		MachineID: "workstation",
	})
	output, err := activated.Activate(t.Context(), "bash")
	require.NoError(t, err)
	require.Contains(t, output, "DEEPSEEK_API_KEY")
}

func TestMachineScopeFiltersActivate(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	first, repository := newEnvironmentServiceAt(t, cache, "workstation")
	_, err := first.Set(t.Context(), SetOptions{
		Name:     "EDITOR",
		Value:    []byte("nvim"),
		Machines: []string{"laptop"},
	})
	require.NoError(t, err)

	second := New(Options{
		Repository:    repository,
		Cache:         NewFileCache(cache),
		AgeKey:        first.options.AgeKey,
		MachineID:     "workstation",
		KnownMachines: []string{"workstation", "laptop"},
	})
	output, err := second.Activate(t.Context(), "bash")
	require.NoError(t, err)
	require.Empty(t, output)

	laptop := New(Options{
		Repository:    repository,
		Cache:         NewFileCache(cache),
		AgeKey:        first.options.AgeKey,
		MachineID:     "laptop",
		KnownMachines: []string{"workstation", "laptop"},
	})
	output, err = laptop.Activate(t.Context(), "zsh")
	require.NoError(t, err)
	require.Equal(t, "export EDITOR='nvim'\n", output)
}

func TestActivateRejectsCacheFromDifferentBinding(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	service, repository := newEnvironmentServiceAt(t, cache, "workstation")
	_, err := service.Set(t.Context(), SetOptions{Name: "EDITOR", Value: []byte("nvim")})
	require.NoError(t, err)
	other := New(Options{
		Repository:     repository,
		Cache:          NewFileCache(cache),
		MachineID:      "workstation",
		RepositoryName: "owner/other",
		Branch:         "main",
		KnownMachines:  []string{"workstation"},
	})
	_, err = other.Activate(t.Context(), "bash")
	require.Error(t, err)
}

func newEnvironmentService(t *testing.T, machineID string) (*Service, *memoryRepository) {
	t.Helper()
	return newEnvironmentServiceAt(t, filepath.Join(t.TempDir(), "cache"), machineID)
}

func newEnvironmentServiceAt(t *testing.T, cache, machineID string) (*Service, *memoryRepository) {
	t.Helper()
	repository := &memoryRepository{snapshot: datarepo.Snapshot{
		Metadata: datarepo.Metadata{
			Repository:   "owner/data",
			RepositoryID: 42,
			Branch:       "main",
			Commit:       "base",
		},
		Files:       map[string][]byte{"my.toml": []byte("schema_version = 1\n")},
		Initialized: true,
	}}
	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	key, err := agecrypt.ParseIdentity([]byte(identity.String()))
	require.NoError(t, err)
	return New(Options{
		Repository:    repository,
		Cache:         NewFileCache(cache),
		AgeKey:        key,
		MachineID:     machineID,
		KnownMachines: []string{"workstation", "laptop"},
	}), repository
}

func cloneSnapshot(snapshot datarepo.Snapshot) datarepo.Snapshot {
	result := snapshot
	result.Files = make(map[string][]byte, len(snapshot.Files))
	for name, data := range snapshot.Files {
		result.Files[name] = append([]byte(nil), data...)
	}
	return result
}
