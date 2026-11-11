package target

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/agecrypt"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/datarepo"
)

type memoryRepository struct {
	snapshot datarepo.Snapshot
	commits  []datarepo.Changes
	sequence int
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
	r.sequence++
	r.snapshot.Metadata.Commit = fmt.Sprintf("commit-%d", r.sequence)
	r.snapshot.Initialized = true
	r.snapshot.ConfigInitialized = true
	return r.snapshot.Metadata, nil
}

func capturedTopgrade(t *testing.T, content string) (string, *memoryRepository, *Service) {
	t.Helper()
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), content)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{Topgrade}})
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
		State:         NewFileState(filepath.Join(home, "state"), "workstation"),
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

func capturedCodexAuth(t *testing.T) (string, *memoryRepository, *Service) {
	t.Helper()
	home := t.TempDir()
	writeFixture(
		t,
		filepath.Join(home, ".codex", "auth.json"),
		`{"auth_mode":"chatgpt","tokens":{"access_token":"initial-version"}}`,
	)
	key := generatedAgeKey(t)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newAuthTestService(home, repository, key)
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{CodexAuth}})
	require.NoError(t, err)
	repository.commits = nil
	return home, repository, service
}

func newAuthTestService(
	home string,
	repository Repository,
	key *agecrypt.Key,
) *Service {
	return New(Options{
		Repository:    repository,
		State:         NewFileState(filepath.Join(home, "state"), "workstation"),
		Home:          home,
		OS:            "linux",
		MachineID:     "workstation",
		KnownMachines: []string{"workstation", "laptop"},
		AgeKey:        key,
	})
}

func generatedAgeKey(t *testing.T) *agecrypt.Key {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	key, err := agecrypt.ParseIdentity([]byte(identity.String() + "\n"))
	require.NoError(t, err)
	return key
}

func setRemoteAuthVersion(
	t *testing.T,
	repository *memoryRepository,
	key *agecrypt.Key,
	plaintext []byte,
) {
	t.Helper()
	version := UnitValueFromBytes(plaintext).Digest
	ciphertext, err := key.Encrypt(plaintext)
	require.NoError(t, err)
	repository.snapshot.Metadata.Commit = "auth-version-2"
	repository.snapshot.Files["configs/targets/codex-auth/data/auth.json.age"] = ciphertext
	metadata, err := encodeRemoteMetadataWithSecret(CodexAuth, MachineScope{All: true}, version, key.Recipient())
	require.NoError(t, err)
	repository.snapshot.Files["configs/targets/codex-auth/target.toml"] = metadata
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

type unknownCommitRepository struct{ *memoryRepository }

func (r unknownCommitRepository) CommitChanges(
	ctx context.Context,
	snapshot datarepo.Snapshot,
	changes datarepo.Changes,
) (datarepo.Metadata, error) {
	metadata, err := r.memoryRepository.CommitChanges(ctx, snapshot, changes)
	if err != nil {
		return metadata, err
	}
	return datarepo.Metadata{}, (fault.Definition{Code: "data.commit_unknown", Kind: fault.Runtime}).New(
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            "data.commit",
				CompletedEffects: nil,
				Recovery:         &fault.Recovery{Code: "data.recover_verify"},
			},
		),
	)
}
