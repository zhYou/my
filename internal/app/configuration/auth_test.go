package configuration

import (
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/agecrypt"
)

func TestCaptureCodexAuthEncryptsFullFileAndStoresVersionOnly(t *testing.T) {
	home := t.TempDir()
	authPath := filepath.Join(home, ".codex", "auth.json")
	auth := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"private-fixture-token"}}`)
	writeFixture(t, authPath, string(auth))
	key := generatedAgeKey(t)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newAuthTestService(home, repository, key)

	result, err := service.Capture(t.Context(), CaptureOptions{Targets: []TargetID{TargetCodexAuth}})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.NotContains(
		t,
		string(repository.snapshot.Files["configs/targets/codex-auth/data/auth.json.age"]),
		"private-fixture-token",
	)

	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, TargetCodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, baseline.Units[SecretUnitID].Secret)
	require.Empty(t, baseline.Units[SecretUnitID].Data)
}

func TestCodexAuthSameVersionDoesNotOverwriteLocalRefresh(t *testing.T) {
	home, repository, service := capturedCodexAuth(t)
	authPath := filepath.Join(home, ".codex", "auth.json")
	refreshed := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"locally-refreshed"}}`)
	writeFixture(t, authPath, string(refreshed))

	result, err := service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.Equal(t, string(refreshed), readFixture(t, authPath))
	require.Empty(t, repository.commits)
}

func TestCodexAuthNewVersionReplacesExistingFile(t *testing.T) {
	home, repository, service := capturedCodexAuth(t)
	authPath := filepath.Join(home, ".codex", "auth.json")
	updated := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"distributed-version"}}`)
	setRemoteAuthVersion(t, repository, service.options.AgeKey, updated)
	diff, err := service.Diff(t.Context(), DiffOptions{Targets: []TargetID{TargetCodexAuth}})
	require.NoError(t, err)
	require.Len(t, diff.Changes, 1)
	require.Equal(t, ActionApplyRemote, diff.Changes[0].Action)
	_, plans, err := service.plan(t.Context(), []TargetID{TargetCodexAuth})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	decrypted, err := service.options.AgeKey.Decrypt(plans[0].ciphertext, plans[0].recipient)
	require.NoError(t, err)
	require.Equal(t, string(updated), string(decrypted))

	result, err := service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, string(updated), readFixture(t, authPath))
}

func TestCodexAuthPendingVersionResumesAfterInterruptedApply(t *testing.T) {
	home, repository, service := capturedCodexAuth(t)
	authPath := filepath.Join(home, ".codex", "auth.json")
	original := readFixture(t, authPath)
	updated := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"pending-version"}}`)
	setRemoteAuthVersion(t, repository, service.options.AgeKey, updated)
	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, TargetCodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	baseline.Pending = &PendingSecret{
		Version:        UnitValueFromBytes(updated).Digest,
		BeforeDigest:   UnitValueFromBytes([]byte(original)).Digest,
		SnapshotDigest: UnitValueFromBytes(updated).Digest,
	}
	require.NoError(t, service.options.State.Write(repository.snapshot.Metadata, baseline))

	result, err := service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.Equal(t, string(updated), readFixture(t, authPath))
	finalBaseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, TargetCodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	require.Nil(t, finalBaseline.Pending)
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
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []TargetID{TargetCodexAuth}})
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
		State:         NewFileState(filepath.Join(home, "state")),
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
	metadata, err := encodeRemoteMetadataWithSecret(TargetCodexAuth, MachineScope{All: true}, version, key.Recipient())
	require.NoError(t, err)
	repository.snapshot.Files["configs/targets/codex-auth/target.toml"] = metadata
}
