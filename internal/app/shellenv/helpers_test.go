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
		Cache:         NewFileCache(cache, machineID),
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

type failingCache struct {
	Cache
	cause error
}

func (c failingCache) Write(CacheState) error {
	return c.cause
}
