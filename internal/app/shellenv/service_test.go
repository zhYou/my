package shellenv

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/zhYou/my/internal/base/fault"

	"github.com/stretchr/testify/require"
)

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
		Cache:      NewFileCache(cache, "workstation"),
		MachineID:  "workstation",
	})
	require.NoError(t, download.SyncCache(t.Context()))
	activated := New(Options{
		Cache:     NewFileCache(cache, "workstation"),
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
		Cache:         NewFileCache(cache, "workstation"),
		AgeKey:        first.options.AgeKey,
		MachineID:     "workstation",
		KnownMachines: []string{"workstation", "laptop"},
	})
	require.NoError(t, second.SyncCache(t.Context()))
	output, err := second.Activate(t.Context(), "bash")
	require.NoError(t, err)
	require.Empty(t, output)

	laptop := New(Options{
		Repository:    repository,
		Cache:         NewFileCache(cache, "laptop"),
		AgeKey:        first.options.AgeKey,
		MachineID:     "laptop",
		KnownMachines: []string{"workstation", "laptop"},
	})
	require.NoError(t, laptop.SyncCache(t.Context()))
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
		Cache:          NewFileCache(cache, "workstation"),
		MachineID:      "workstation",
		RepositoryName: "owner/other",
		Branch:         "main",
		KnownMachines:  []string{"workstation"},
	})
	_, err = other.Activate(t.Context(), "bash")
	require.Error(t, err)
}

func TestActivateRejectsCacheFromOtherRepositoryID(t *testing.T) {
	service, _ := newEnvironmentService(t, "workstation")
	_, err := service.Set(t.Context(), SetOptions{Name: "EDITOR", Value: []byte("nvim")})
	require.NoError(t, err)
	service.options.RepositoryName = "owner/data"
	service.options.RepositoryID = 999
	service.options.Branch = "main"
	output, err := service.Activate(t.Context(), "bash")
	require.Error(t, err)
	require.Empty(t, output)
}

func TestEnvironmentCachesAreIsolatedByMachine(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	first, repository := newEnvironmentServiceAt(t, root, "workstation")
	_, err := first.Set(t.Context(), SetOptions{Name: "EDITOR", Value: []byte("nvim")})
	require.NoError(t, err)
	second := New(Options{Repository: repository, Cache: NewFileCache(root, "laptop"), MachineID: "laptop"})
	output, err := second.Activate(t.Context(), "bash")
	require.Error(t, err)
	require.Empty(t, output)
	require.NoError(t, second.SyncCache(t.Context()))
	output, err = second.Activate(t.Context(), "bash")
	require.NoError(t, err)
	require.Equal(t, "export EDITOR='nvim'\n", output)
}

func TestCacheFailureReportsConfirmedCommitAndPreservesCause(t *testing.T) {
	service, repository := newEnvironmentService(t, "workstation")
	cause := errors.New("synthetic-secret-marker")
	service.options.Cache = failingCache{Cache: service.options.Cache, cause: cause}
	_, err := service.Set(t.Context(), SetOptions{Name: "EDITOR", Value: []byte("nvim")})
	require.ErrorIs(t, err, cause)
	require.NotContains(t, err.Error(), cause.Error())
	diagnostic, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Contains(t, diagnostic.CompletedEffects, "data.commit")
	require.Equal(t, repository.snapshot.Metadata.Commit, diagnostic.Recovery.Params["Commit"])
}

func TestOrdinaryChangePreservesSecretVersion(t *testing.T) {
	service, repository := newEnvironmentService(t, "workstation")
	_, err := service.Set(t.Context(), SetOptions{Name: "API_KEY", Secret: true, Value: []byte("fixture")})
	require.NoError(t, err)
	ciphertext := append([]byte(nil), repository.snapshot.Files[secretDataPath()]...)
	metadata := append([]byte(nil), repository.snapshot.Files[secretMetadataPath()]...)
	_, err = service.Set(t.Context(), SetOptions{Name: "EDITOR", Value: []byte("nvim")})
	require.NoError(t, err)
	require.Equal(t, ciphertext, repository.snapshot.Files[secretDataPath()])
	require.Equal(t, metadata, repository.snapshot.Files[secretMetadataPath()])
}

func TestSetRejectsDangerousVariableNames(t *testing.T) {
	service, repository := newEnvironmentService(t, "workstation")
	for _, name := range []string{"PATH", "BASH_ENV", "LD_PRELOAD", "NODE_OPTIONS", "PYTHONSTARTUP"} {
		_, err := service.Set(t.Context(), SetOptions{Name: name, Value: []byte("fixture")})
		require.Error(t, err)
	}
	require.Zero(t, repository.commits)
}

func TestSetRejectsInvalidValues(t *testing.T) {
	service, _ := newEnvironmentService(t, "workstation")
	for _, value := range [][]byte{nil, []byte("line\nfeed"), {0}, make([]byte, MaxValueSize+1)} {
		_, err := service.Set(t.Context(), SetOptions{Name: "EDITOR", Value: value})
		require.Error(t, err)
	}
}
