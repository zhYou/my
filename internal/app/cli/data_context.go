package cli

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/app/shellenv"
	"github.com/zhYou/my/internal/app/target"
	"github.com/zhYou/my/internal/datarepo"
)

type businessContext struct {
	binding    machine.Binding
	repository *snapshotRepository
	machines   []string
	guard      bindingGuard
}

// businessData 固定一次远端快照，并核对本机绑定的不可变仓库身份。
func (call *invocation) businessData(ctx context.Context) (businessContext, error) {
	binding, err := call.requireBinding()
	if err != nil {
		return businessContext{}, err
	}
	client, err := call.dataRepository(ctx)
	if err != nil {
		return businessContext{}, err
	}
	snapshot, err := client.Read(ctx)
	if err != nil {
		return businessContext{}, err
	}
	if binding.RepositoryID != snapshot.Metadata.RepositoryID ||
		!strings.EqualFold(binding.Repository, snapshot.Metadata.Repository) ||
		binding.Branch != snapshot.Metadata.Branch {
		return businessContext{}, problemForCLI(codeBindingConflict, nil)
	}
	records, err := machine.Records(snapshot)
	if err != nil {
		return businessContext{}, err
	}
	record, exists := records[binding.MachineID]
	if !exists || record.Status != machine.Active {
		return businessContext{}, problemForCLI("machine.not_active", nil)
	}
	known := make([]string, 0, len(records))
	for id, record := range records {
		if record.Status == machine.Active {
			known = append(known, id)
		}
	}
	slices.Sort(known)
	return businessContext{
		binding:    binding,
		repository: &snapshotRepository{repository: client, snapshot: snapshot},
		machines:   known,
		guard:      bindingGuard{state: call.machineState(), expected: binding},
	}, nil
}

func (call *invocation) machineState() machine.State {
	if call.deps.State != nil {
		return call.deps.State(call.directories.State)
	}
	return machine.NewFileState(call.directories.State)
}

type snapshotRepository struct {
	repository target.Repository
	snapshot   datarepo.Snapshot
}

func (r *snapshotRepository) Read(ctx context.Context) (datarepo.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return datarepo.Snapshot{}, err
	}
	result := r.snapshot
	result.Files = maps.Clone(result.Files)
	for name, data := range result.Files {
		result.Files[name] = slices.Clone(data)
	}
	return result, nil
}

func (r *snapshotRepository) CommitChanges(
	ctx context.Context,
	before datarepo.Snapshot,
	changes datarepo.Changes,
) (datarepo.Metadata, error) {
	metadata, err := r.repository.CommitChanges(ctx, before, changes)
	if err != nil {
		return metadata, err
	}
	for name, data := range changes.Files {
		r.snapshot.Files[name] = slices.Clone(data)
	}
	for _, name := range changes.Deletions {
		delete(r.snapshot.Files, name)
	}
	r.snapshot.Metadata = metadata
	_, r.snapshot.ConfigInitialized = r.snapshot.Files["configs/manifest.toml"]
	return metadata, nil
}

type bindingGuard struct {
	state    machine.State
	expected machine.Binding
}

func (g bindingGuard) lock(
	ctx context.Context,
	inner func(context.Context) (func() error, error),
) (func() error, error) {
	unlockBinding, err := g.state.Lock(ctx)
	if err != nil {
		return nil, err
	}
	binding, exists, err := g.state.Read()
	if err == nil && (!exists || !equalBinding(binding, g.expected)) {
		err = problemForCLI(codeBindingConflict, nil)
	}
	if err != nil {
		return nil, errors.Join(err, unlockBinding())
	}
	unlockInner, err := inner(ctx)
	if err != nil {
		return nil, errors.Join(err, unlockBinding())
	}
	return func() error {
		return errors.Join(unlockInner(), unlockBinding())
	}, nil
}

func equalBinding(a, b machine.Binding) bool {
	a.Repository, b.Repository = strings.ToLower(a.Repository), strings.ToLower(b.Repository)
	return a == b
}

type guardedTargetState struct {
	target.State
	guard bindingGuard
}

func (s guardedTargetState) Lock(ctx context.Context) (func() error, error) {
	return s.guard.lock(ctx, s.State.Lock)
}

type guardedCache struct {
	shellenv.Cache
	guard bindingGuard
}

func (c guardedCache) Lock(ctx context.Context) (func() error, error) {
	return c.guard.lock(ctx, c.Cache.Lock)
}
