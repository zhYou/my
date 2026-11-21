package target

import (
	"context"
	"errors"
	"slices"

	"github.com/zhYou/my/internal/datarepo"
)

// UpdateOptions 指定一个已纳管目标及其唯一的新机器范围。
type UpdateOptions struct {
	Target ID
	Scope  MachineScope
}

// UpdateResult 描述范围修正后的目标、范围和读取版本。
type UpdateResult struct {
	Snapshot datarepo.Metadata `json:"snapshot"`
	Target   ID                `json:"target"`
	Scope    []string          `json:"scope"`
	Changed  bool              `json:"changed"`
}

// Update 只重写已纳管目标的机器范围，保留单位和数据文件。
func (s *Service) Update(ctx context.Context, input UpdateOptions) (result UpdateResult, err error) {
	ctx, progress := s.begin(ctx, "target.update")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if err := input.Validate(s.options.Definitions); err != nil {
		return result, err
	}
	if err = s.validate(); err != nil {
		return result, err
	}
	if err := input.Scope.Validate(s.options.KnownMachines); err != nil {
		return result, err
	}
	unlock, err := s.options.State.Lock(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return result, err
	}
	progress.step(ctx, phaseCompare)
	remote, err := parseRemoteTargets(snapshot, s.definitions())
	if err != nil {
		return result, err
	}
	existing, managed := remote[input.Target]
	if !managed {
		return result, notManaged(input.Target, phaseValidate)
	}
	if existing.target.RemoteAuthoritative {
		return result, remoteAuthoritative(input.Target, phaseValidate)
	}
	scope := input.Scope.Canonical()
	result = UpdateResult{
		Snapshot: snapshot.Metadata,
		Target:   input.Target,
		Scope:    scope.Values(),
	}
	if slices.Equal(existing.scope.Values(), result.Scope) {
		return result, nil
	}
	metadata, err := encodeRemoteMetadata(existing.id, scope)
	if existing.target.Kind == UnitSecret {
		metadata, err = encodeRemoteMetadataWithSecret(
			existing.id,
			scope,
			existing.version,
			existing.recipient,
		)
	}
	if err != nil {
		return result, err
	}
	progress.commit = snapshot.Metadata.Commit
	progress.step(ctx, phaseCommit)
	committed, err := s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
		Message: "my: 更新目标范围",
		Files: map[string][]byte{
			remoteTargetMetadataPath(input.Target): metadata,
		},
	})
	if err != nil {
		return result, err
	}
	if committed.Commit != snapshot.Metadata.Commit {
		progress.confirmed(effectDataCommit)
	}
	progress.commit = committed.Commit
	result.Snapshot = committed
	result.Changed = true
	return result, nil
}
