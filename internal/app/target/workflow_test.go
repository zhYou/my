package target

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/datarepo"
)

func TestCaptureCreatesTargetAndBaseline(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = true\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)

	result, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{Topgrade}})
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
		Topgrade,
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

func TestDiffReportsLocalRemoteAndUnifiedText(t *testing.T) {
	_, _, service := conflictingTopgrade(t)
	diff, err := service.Diff(t.Context(), DiffOptions{Targets: []ID{Topgrade}})
	require.NoError(t, err)
	require.Len(t, diff.Changes, 1)
	require.Equal(t, "verbose = \"local\"\n", diff.Changes[0].Local)
	require.Equal(t, "verbose = \"remote\"\n", diff.Changes[0].Remote)
	require.Contains(t, diff.Changes[0].Diff, `-verbose = "local"`)
	require.Contains(t, diff.Changes[0].Diff, `+verbose = "remote"`)
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
			Target: Topgrade,
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
			Target: Topgrade,
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

	result, err := service.Remove(t.Context(), RemoveOptions{Targets: []ID{Topgrade}})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.NotContains(t, repository.snapshot.Files, "configs/targets/topgrade/target.toml")
	require.Equal(t, "verbose = true\n", readFixture(t, localPath))
	_, exists, err := service.options.State.Read(repository.snapshot.Metadata, Topgrade)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestCaptureCodexAuthEncryptsFullFileAndStoresVersionOnly(t *testing.T) {
	home := t.TempDir()
	authPath := filepath.Join(home, ".codex", "auth.json")
	auth := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"private-fixture-token"}}`)
	writeFixture(t, authPath, string(auth))
	key := generatedAgeKey(t)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newAuthTestService(home, repository, key)

	result, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{CodexAuth}})
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.NotContains(
		t,
		string(repository.snapshot.Files["configs/targets/codex-auth/data/auth.json.age"]),
		"private-fixture-token",
	)

	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, CodexAuth)
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
	diff, err := service.Diff(t.Context(), DiffOptions{Targets: []ID{CodexAuth}})
	require.NoError(t, err)
	require.Len(t, diff.Changes, 1)
	require.Equal(t, ActionApplyRemote, diff.Changes[0].Action)
	require.True(t, diff.Changes[0].Secret)
	require.NotEmpty(t, diff.Changes[0].Remote)
	require.NotContains(t, diff.Changes[0].Diff, "distributed-version")
	_, plans, err := service.plan(t.Context(), []ID{CodexAuth})
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

func TestCaptureMiseBackendNamesRoundTrip(t *testing.T) {
	home := t.TempDir()
	localPath := filepath.Join(home, ".config", "mise", "config.toml")
	writeFixture(
		t,
		localPath,
		"[tools]\n\"go:golang.org/x/tools/gopls\" = \"0.24.0\"\n\"custom.with.dot\" = \"1.0.0\"\n[tasks.keep]\nrun = \"echo keep\"\n",
	)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	result, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{Mise}})
	require.NoError(t, err)
	require.True(t, result.Changed)
	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, Mise)
	require.NoError(t, err)
	require.True(t, exists)
	require.Contains(t, baseline.Units, UnitID(`field:tools."go:golang.org/x/tools/gopls"`))
	require.Contains(t, baseline.Units, UnitID(`field:tools."custom.with.dot"`))
	remotePath := "configs/targets/mise/data/values.toml"
	repository.snapshot.Files[remotePath] = bytes.ReplaceAll(
		repository.snapshot.Files[remotePath],
		[]byte("0.24.0"),
		[]byte("0.25.0"),
	)
	repository.snapshot.Metadata.Commit = "remote-mise-version"
	_, err = service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	var actual struct {
		Tools map[string]string `toml:"tools"`
		Tasks map[string]struct {
			Run string `toml:"run"`
		} `toml:"tasks"`
	}
	require.NoError(t, toml.Unmarshal([]byte(readFixture(t, localPath)), &actual))
	require.Equal(t, "0.25.0", actual.Tools["go:golang.org/x/tools/gopls"])
	require.Equal(t, "1.0.0", actual.Tools["custom.with.dot"])
	require.Equal(t, "echo keep", actual.Tasks["keep"].Run)
}

func TestSyncRejectsMalformedRemoteFilesBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name    string
		format  FileFormat
		valid   string
		invalid string
	}{
		{"toml", FormatTOML, "enabled = true\n", "enabled = ["},
		{"json", FormatJSON, "{\"enabled\":true}", "{broken"},
		{"yaml", FormatYAML, "enabled: true\n", "enabled: [unterminated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			file := File{
				Name:         "settings." + test.name,
				RelativePath: ".fixture/settings." + test.name,
				Format:       test.format,
				Required:     true,
			}
			definition := Definition{ID: "fixture-settings", Kind: UnitFile, Files: []File{file}}
			localPath := filepath.Join(home, filepath.FromSlash(file.RelativePath))
			writeFixture(t, localPath, test.valid)
			repository := &memoryRepository{snapshot: initializedSnapshot()}
			service := New(
				Options{
					Repository:    repository,
					State:         NewFileState(filepath.Join(home, "state"), "workstation"),
					Home:          home,
					MachineID:     "workstation",
					KnownMachines: []string{"workstation"},
					Definitions:   []Definition{definition},
				},
			)
			_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{definition.ID}})
			require.NoError(t, err)
			repository.commits = nil
			repository.snapshot.Files["configs/targets/fixture-settings/data/"+file.Name] = []byte(test.invalid)
			repository.snapshot.Metadata.Commit = "malformed-remote"
			_, err = service.Sync(t.Context(), SyncOptions{})
			require.Error(t, err)
			require.Equal(t, test.valid, readFixture(t, localPath))
			require.Empty(t, repository.commits)
		})
	}
}

func TestResolveRemoteAuthCreatesMissingLocalFile(t *testing.T) {
	_, repository, existing := capturedCodexAuth(t)
	home := t.TempDir()
	service := newAuthTestService(home, repository, existing.options.AgeKey)
	diff, err := service.Diff(t.Context(), DiffOptions{Targets: []ID{CodexAuth}})
	require.NoError(t, err)
	require.Len(t, diff.Conflicts, 1)
	_, err = service.Resolve(
		t.Context(),
		ResolveOptions{Target: CodexAuth, Unit: SecretUnitID, From: ResolveRemote},
	)
	require.NoError(t, err)
	authPath := filepath.Join(home, ".codex", "auth.json")
	require.JSONEq(t, `{"auth_mode":"chatgpt","tokens":{"access_token":"initial-version"}}`, readFixture(t, authPath))
	refreshed := `{"auth_mode":"chatgpt","tokens":{"access_token":"local-refresh"}}`
	writeFixture(t, authPath, refreshed)
	_, err = service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	require.Equal(t, refreshed, readFixture(t, authPath))
}

type failingFinalBaseline struct {
	State
	fail bool
}

func (s *failingFinalBaseline) Write(metadata datarepo.Metadata, baseline Baseline) error {
	if s.fail && baseline.Pending == nil {
		s.fail = false
		return errors.New("synthetic final baseline failure")
	}
	return s.State.Write(metadata, baseline)
}

func TestCodexAuthResumesAfterFileWasReplaced(t *testing.T) {
	home, repository, service := capturedCodexAuth(t)
	updated := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"pending-version"}}`)
	setRemoteAuthVersion(t, repository, service.options.AgeKey, updated)
	service.options.State = &failingFinalBaseline{State: service.options.State, fail: true}
	_, err := service.Sync(t.Context(), SyncOptions{})
	require.Error(t, err)
	authPath := filepath.Join(home, ".codex", "auth.json")
	require.Equal(t, string(updated), readFixture(t, authPath))
	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, CodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	require.NotNil(t, baseline.Pending)
	resumed := newAuthTestService(home, repository, service.options.AgeKey)
	_, err = resumed.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	require.Equal(t, string(updated), readFixture(t, authPath))
	baseline, exists, err = resumed.options.State.Read(repository.snapshot.Metadata, CodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	require.Nil(t, baseline.Pending)
}

func TestCodexAuthRejectsMalformedRemotePlaintext(t *testing.T) {
	home, repository, service := capturedCodexAuth(t)
	authPath := filepath.Join(home, ".codex", "auth.json")
	before := readFixture(t, authPath)
	setRemoteAuthVersion(t, repository, service.options.AgeKey, []byte("{broken"))
	_, err := service.Sync(t.Context(), SyncOptions{})
	require.Error(t, err)
	require.Equal(t, before, readFixture(t, authPath))
	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, CodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	require.Nil(t, baseline.Pending)
}

func TestSyncKeepsUnitsBelongingToOtherPlatforms(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	writeFixture(t, configPath, "model = \"local-model\"\n[features]\nnetwork_proxy = false\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{CodexSettings}})
	require.NoError(t, err)
	remotePath := "configs/targets/codex-settings/data/values.toml"
	units, err := decodeFieldValues(repository.snapshot.Files[remotePath])
	require.NoError(t, err)
	foreignUnit := UnitID("field:filesystem.nuget-data-local")
	units[foreignUnit] = UnitValueFromBytes([]byte(`"write"`))
	repository.snapshot.Files[remotePath], err = encodeFieldValues(units)
	require.NoError(t, err)
	repository.snapshot.Metadata.Commit = "another-platform"
	diff, err := service.Diff(t.Context(), DiffOptions{})
	require.NoError(t, err)
	require.Empty(t, diff.Conflicts)
	_, err = service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	writeFixture(t, configPath, "model = \"local-model\"\n[features]\nnetwork_proxy = true\n")
	_, err = service.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	remote, err := decodeFieldValues(repository.snapshot.Files[remotePath])
	require.NoError(t, err)
	require.Equal(t, []byte(`"write"`), remote[foreignUnit].Data)
	require.Equal(t, []byte("true"), remote[UnitID("field:features.network_proxy")].Data)
	require.Contains(t, readFixture(t, configPath), `model = "local-model"`)
}

func TestResolveRejectsFieldsOutsideDeclaredOwnership(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".codex", "config.toml")
	before := "model = \"local-model\"\n[features]\nnetwork_proxy = false\n"
	writeFixture(t, configPath, before)
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{CodexSettings}})
	require.NoError(t, err)
	remotePath := "configs/targets/codex-settings/data/values.toml"
	units, err := decodeFieldValues(repository.snapshot.Files[remotePath])
	require.NoError(t, err)
	units[UnitID("field:model")] = UnitValueFromBytes([]byte(`"remote-model"`))
	repository.snapshot.Files[remotePath], err = encodeFieldValues(units)
	require.NoError(t, err)
	_, err = service.Resolve(
		t.Context(),
		ResolveOptions{Target: CodexSettings, Unit: UnitID("field:model"), From: ResolveRemote},
	)
	require.Error(t, err)
	require.Equal(t, before, readFixture(t, configPath))
}

type blockedAuthApply struct {
	State
	directory string
}

func (s *blockedAuthApply) Write(metadata datarepo.Metadata, baseline Baseline) error {
	if err := s.State.Write(metadata, baseline); err != nil {
		return err
	}
	if baseline.Pending != nil {
		return os.WriteFile(s.directory, []byte("synthetic blocking file"), 0o600)
	}
	return nil
}

func TestCodexAuthResumesWhenFirstFileWriteFailed(t *testing.T) {
	_, repository, existing := capturedCodexAuth(t)
	home := t.TempDir()
	service := newAuthTestService(home, repository, existing.options.AgeKey)
	directory := filepath.Join(home, ".codex")
	service.options.State = &blockedAuthApply{State: service.options.State, directory: directory}
	_, err := service.Resolve(
		t.Context(),
		ResolveOptions{Target: CodexAuth, Unit: SecretUnitID, From: ResolveRemote},
	)
	require.Error(t, err)
	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, CodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	require.NotNil(t, baseline.Pending)
	require.NoError(t, os.Remove(directory))
	resumed := newAuthTestService(home, repository, existing.options.AgeKey)
	_, err = resumed.Sync(t.Context(), SyncOptions{})
	require.NoError(t, err)
	require.JSONEq(
		t,
		`{"auth_mode":"chatgpt","tokens":{"access_token":"initial-version"}}`,
		readFixture(t, filepath.Join(directory, "auth.json")),
	)
}

func TestCaptureFailureReportsConfirmedCommit(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = true\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	service.options.State = &failingFinalBaseline{State: service.options.State, fail: true}
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{Topgrade}})
	require.Error(t, err)
	diagnostic, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Contains(t, diagnostic.CompletedEffects, "data.commit")
	require.Equal(t, repository.snapshot.Metadata.Commit, diagnostic.Recovery.Params["Commit"])
	require.Contains(t, string(repository.snapshot.Files["configs/targets/topgrade/data/topgrade.toml"]), "verbose")
}

func TestAuthFailureReportsConfirmedReplacement(t *testing.T) {
	_, repository, service := capturedCodexAuth(t)
	setRemoteAuthVersion(t, repository, service.options.AgeKey, []byte(`{"tokens":{"access_token":"new-version"}}`))
	service.options.State = &failingFinalBaseline{State: service.options.State, fail: true}
	_, err := service.Sync(t.Context(), SyncOptions{})
	require.Error(t, err)
	diagnostic, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Contains(t, diagnostic.CompletedEffects, "target.local_write:codex-auth")
	require.Contains(t, diagnostic.CompletedEffects, "target.pending:codex-auth")
}

func TestCanceledCaptureDoesNotReadOrCommit(t *testing.T) {
	home := t.TempDir()
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, repository)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := service.Capture(ctx, CaptureOptions{Targets: []ID{Topgrade}})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, repository.commits)
}

func TestPersistedBaselineNeverKeepsSecretContent(t *testing.T) {
	_, repository, service := capturedCodexAuth(t)
	baseline, exists, err := service.options.State.Read(repository.snapshot.Metadata, CodexAuth)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, baseline.Units[SecretUnitID].Secret)
	require.Empty(t, baseline.Units[SecretUnitID].Data)
	state, ok := service.options.State.(*FileState)
	require.True(t, ok)
	paths, err := filepath.Glob(filepath.Join(state.targetDirectory(), "*", "*", "*", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		data, err := os.ReadFile(path) // #nosec G304 -- 仅读取测试临时目录的持久基线。
		require.NoError(t, err)
		require.NotContains(t, string(data), "initial-version")
		require.NotContains(t, string(data), "access_token")
	}
}

func TestCapturePreservesUnknownCommitAcrossUnlock(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, ".config", "topgrade.toml"), "verbose = true\n")
	repository := &memoryRepository{snapshot: initializedSnapshot()}
	service := newTestService(home, unknownCommitRepository{repository})
	_, err := service.Capture(t.Context(), CaptureOptions{Targets: []ID{Topgrade}})
	require.Error(t, err)
	diagnostic, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Nil(t, diagnostic.CompletedEffects)
	require.Equal(t, "data.recover_verify", diagnostic.Recovery.Code)
	require.Len(t, repository.commits, 1)
}
