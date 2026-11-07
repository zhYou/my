package machine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/datarepo"
)

type memoryRepository struct {
	snapshot datarepo.Snapshot
	commits  int
	lost     bool
}

func (r *memoryRepository) Read(context.Context) (datarepo.Snapshot, error) {
	return r.snapshot, nil
}

func (r *memoryRepository) Commit(
	_ context.Context,
	s datarepo.Snapshot,
	files map[string][]byte,
) (datarepo.Metadata, error) {
	if s.Metadata.Commit != r.snapshot.Metadata.Commit {
		return datarepo.Metadata{}, errors.New("stale head")
	}
	r.commits++
	maps.Copy(r.snapshot.Files, files)
	r.snapshot.Initialized = true
	r.snapshot.Metadata.Commit = fmt.Sprintf("%040x", r.commits+10)
	if r.lost {
		r.lost = false
		return datarepo.Metadata{}, (fault.Definition{Code: "data.commit_unknown", Kind: fault.Runtime}).New(
			fault.WithDiagnostic(
				fault.Diagnostic{Phase: "data.commit", Recovery: &fault.Recovery{Code: "data.recover_verify"}},
			),
		)
	}
	return r.snapshot.Metadata, nil
}

func TestLifecyclePreservesIdentityAndRegistrationTime(t *testing.T) {
	r := newMemoryRepository()
	local := NewFileState(filepath.Join(t.TempDir(), "state"))
	s := serviceFixture(t, r, local)
	_, err := s.Initialize(t.Context())
	require.NoError(t, err)
	input := RegisterOptions{ID: "fixture", Kind: "vm", Note: "private-note-marker"}
	first, err := s.Register(t.Context(), input)
	require.NoError(t, err)
	again, err := s.Register(t.Context(), input)
	require.NoError(t, err)
	require.False(t, again.Changed)
	require.Equal(t, 2, r.commits)
	require.Equal(t, first.Machine.RegisteredAt, again.Machine.RegisteredAt)
	fresh := serviceFixture(t, r, NewFileState(filepath.Join(t.TempDir(), "state")))
	_, err = fresh.Bind(t.Context(), BindOptions{ID: "fixture"})
	require.NoError(t, err)
	_, err = fresh.Retire(t.Context(), "fixture")
	require.NoError(t, err)
	_, ok, err := fresh.options.State.Read()
	require.NoError(t, err)
	require.False(t, ok)
	_, err = fresh.Bind(t.Context(), BindOptions{ID: "fixture"})
	require.Error(t, err)
	record, err := fresh.Show(t.Context(), "fixture")
	require.NoError(t, err)
	record.Machine.OS = "windows"
	encoded, err := encodeRecord(record.Machine)
	require.NoError(t, err)
	r.snapshot.Files["machines/fixture.toml"] = encoded
	_, err = fresh.Bind(t.Context(), BindOptions{ID: "fixture", Reactivate: true})
	require.Error(t, err)
	restored, err := fresh.Bind(t.Context(), BindOptions{ID: "fixture", Reactivate: true, UpdatePlatform: true})
	require.NoError(t, err)
	require.Equal(t, "active", restored.Machine.Status)
	require.Equal(t, "linux", restored.Machine.OS)
	require.Nil(t, restored.Machine.RetiredAt)
	require.Equal(t, first.Machine.RegisteredAt, restored.Machine.RegisteredAt)
	require.Equal(t, first.Machine.Note, restored.Machine.Note)
}

type failedWriteState struct {
	State
	failure error
}

func (s failedWriteState) Write(Binding) error { return s.failure }

func TestConfirmedCommitAndFailedBindingCanBeRecoveredWithoutNewCommit(t *testing.T) {
	r := newMemoryRepository()
	good := NewFileState(filepath.Join(t.TempDir(), "state"))
	sentinel := errors.New("private-local-cause")
	s := serviceFixture(t, r, failedWriteState{State: good, failure: sentinel})
	_, err := s.Initialize(t.Context())
	require.NoError(t, err)
	_, err = s.Register(t.Context(), RegisterOptions{ID: "fixture", Kind: "vm", Note: "fixture"})
	require.ErrorIs(t, err, sentinel)
	require.NotContains(t, err.Error(), "private-local-cause")
	d, ok := fault.DiagnosticOf(err)
	require.True(t, ok)
	require.Equal(t, []string{"data.commit"}, d.CompletedEffects)
	require.Equal(t, "machine.binding.write", d.Phase)
	s = serviceFixture(t, r, good)
	_, err = s.Bind(t.Context(), BindOptions{ID: "fixture"})
	require.NoError(t, err)
	require.Equal(t, 2, r.commits)
}

func TestUnknownCommitIsNotRetriedAndReexecutionVerifiesRemote(t *testing.T) {
	r := newMemoryRepository()
	s := serviceFixture(t, r, NewFileState(filepath.Join(t.TempDir(), "state")))
	_, err := s.Initialize(t.Context())
	require.NoError(t, err)
	r.lost = true
	input := RegisterOptions{ID: "fixture", Kind: "vm", Note: "fixture"}
	_, err = s.Register(t.Context(), input)
	require.Error(t, err)
	d, _ := fault.DiagnosticOf(err)
	require.Nil(t, d.CompletedEffects)
	_, bound, err := s.options.State.Read()
	require.NoError(t, err)
	require.False(t, bound)
	require.Equal(t, 2, r.commits)
	_, err = s.Register(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, 2, r.commits)
}

func TestForeignBindingStopsBeforeRemoteMutation(t *testing.T) {
	r := newMemoryRepository()
	local := NewFileState(filepath.Join(t.TempDir(), "state"))
	s := serviceFixture(t, r, local)
	_, err := s.Initialize(t.Context())
	require.NoError(t, err)
	_, err = s.Register(t.Context(), RegisterOptions{ID: "one", Kind: "vm", Note: "fixture"})
	require.NoError(t, err)
	_, err = s.Register(t.Context(), RegisterOptions{ID: "two", Kind: "vm", Note: "fixture"})
	require.Error(t, err)
	require.Equal(t, 2, r.commits)
	binding, err := s.Current(t.Context())
	require.NoError(t, err)
	require.Equal(t, "one", binding.MachineID)
}

func TestStateAllowsReadableParentButKeepsStatePrivate(t *testing.T) {
	parent := t.TempDir()
	require.NoError(t, os.Chmod(parent, 0o755)) // #nosec G302 -- 专门验证安全可读父目录的回归。
	state := NewFileState(filepath.Join(parent, "state"))
	unlock, err := state.Lock(t.Context())
	require.NoError(t, err)
	require.NoError(t, unlock())
	info, err := os.Stat(filepath.Join(parent, "state"))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	}
}

func TestStateLockSerializesAndDoesNotReplaceOtherBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	one := NewFileState(path)
	two := NewFileState(path)
	release, err := one.Lock(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	_, err = two.Lock(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	binding := Binding{SchemaVersion: 1, Repository: "owner/data", RepositoryID: 42, Branch: "main", MachineID: "one"}
	require.NoError(t, one.Write(binding))
	require.NoError(t, release())
	release, err = two.Lock(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, release()) }()
	other := binding
	other.MachineID = "two"
	require.Error(t, two.Write(other))
	require.Error(t, two.Clear(other))
	current, bound, err := two.Read()
	require.NoError(t, err)
	require.True(t, bound)
	require.Equal(t, binding, current)
}

func TestStateRejectsSymlinkAndMalformedBindingWithoutChangingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink privileges are not guaranteed")
	}
	path := filepath.Join(t.TempDir(), "state")
	state := NewFileState(path)
	release, err := state.Lock(t.Context())
	require.NoError(t, err)
	require.NoError(t, release())
	target := filepath.Join(t.TempDir(), "private.json")
	require.NoError(t, os.WriteFile(target, []byte("private-marker"), 0o600))
	link := filepath.Join(path, bindingFile)
	require.NoError(t, os.Symlink(target, link))
	_, _, err = state.Read()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-marker")
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.WriteFile(link, []byte(`{"schema_version":1,"private-marker":true}`), 0o600))
	_, _, err = state.Read()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-marker")
	data, err := os.ReadFile(target) // #nosec G304 -- 仅测试创建的临时目标。
	require.NoError(t, err)
	require.Equal(t, "private-marker", string(data))
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		snapshot: datarepo.Snapshot{
			Metadata: datarepo.Metadata{
				Repository:   "owner/data",
				RepositoryID: 42,
				Branch:       "main",
				Commit:       strings.Repeat("a", 40),
			},
			Files: map[string][]byte{},
		},
	}
}

func serviceFixture(t *testing.T, r *memoryRepository, state State) *Service {
	t.Helper()
	return New(
		Options{
			Repository: r,
			Config:     datarepo.Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
			State:      state,
			OS:         "linux",
			Arch:       "amd64",
			Now:        func() time.Time { return time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC) },
		},
	)
}
