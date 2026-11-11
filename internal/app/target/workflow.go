package target

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/zhYou/my/internal/base/agecrypt"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/datarepo"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Repository 是配置业务使用的一致快照和 CAS 提交边界。
type Repository interface {
	Read(context.Context) (datarepo.Snapshot, error)
	CommitChanges(context.Context, datarepo.Snapshot, datarepo.Changes) (datarepo.Metadata, error)
}

// EnvironmentCacheSyncer 把远端环境声明下载到本机离线缓存。
type EnvironmentCacheSyncer interface {
	SyncCache(context.Context) error
}

// Options 显式注入配置业务依赖。
type Options struct {
	Logger           *logging.Logger
	Tracer           trace.Tracer
	Localizer        *messages.Localizer
	Repository       Repository
	State            State
	Home             string
	OS               string
	MachineID        string
	KnownMachines    []string
	Definitions      []Definition
	AgeKey           *agecrypt.Key
	LoadAgeKey       func() (*agecrypt.Key, error)
	EnvironmentCache EnvironmentCacheSyncer
}

// Service 执行目标收录、差异、同步、冲突解决和移除。
type Service struct {
	options Options
}

// CaptureOptions 声明首次收录目标和机器范围。
type CaptureOptions struct {
	Targets  []ID
	All      bool
	Machines []string
}

// DiffOptions 选择只读比较的目标。
type DiffOptions struct {
	Targets []ID
}

// SyncOptions 选择双向同步的目标。
type SyncOptions struct {
	Targets []ID
}

// ResolveSource 选择冲突的本地或远端来源。
type ResolveSource string

const (
	// ResolveLocal 采用本机值。
	ResolveLocal ResolveSource = "local"
	// ResolveRemote 采用远端值。
	ResolveRemote ResolveSource = "remote"
)

// ResolveOptions 指定一个稳定冲突单位。
type ResolveOptions struct {
	Target ID
	Unit   UnitID
	From   ResolveSource
}

// RemoveOptions 指定需要取消纳管的目标。
type RemoveOptions struct {
	Targets []ID
}

// ItemResult 汇总一个目标的变化。
type ItemResult struct {
	Target    ID   `json:"target"`
	Changed   bool `json:"changed"`
	Changes   int  `json:"changes"`
	Conflicts int  `json:"conflicts"`
}

// Result 是写操作完成后返回的稳定结果。
type Result struct {
	Snapshot datarepo.Metadata `json:"snapshot"`
	Targets  []ItemResult      `json:"targets"`
	Changed  bool              `json:"changed"`
}

// DiffChange 描述一个单位的只读比较结果。
type DiffChange struct {
	Target   ID     `json:"target"`
	Unit     UnitID `json:"unit"`
	Action   Action `json:"action"`
	Baseline string `json:"baseline,omitempty"`
	Local    string `json:"local,omitempty"`
	Remote   string `json:"remote,omitempty"`
	Diff     string `json:"diff,omitempty"`
	Secret   bool   `json:"secret"`
}

// Conflict 描述需要显式来源选择的单位。
type Conflict struct {
	Target ID     `json:"target"`
	Unit   UnitID `json:"unit"`
	Code   string `json:"code"`
}

// DiffResult 是只读差异报告。
type DiffResult struct {
	Snapshot  datarepo.Metadata `json:"snapshot"`
	Changes   []DiffChange      `json:"changes"`
	Conflicts []Conflict        `json:"conflicts"`
}

// ConflictError 表示同步在写任何一侧前停止。
type ConflictError struct {
	Conflicts []Conflict
}

// Error 实现 error。
func (e *ConflictError) Error() string {
	return "target.conflicts"
}

type targetPlan struct {
	target             Definition
	scope              MachineScope
	local              map[UnitID]UnitValue
	remote             map[UnitID]UnitValue
	allRemote          map[UnitID]UnitValue
	baseline           Baseline
	hasBaseline        bool
	missingLocal       bool
	comparisons        map[UnitID]Comparison
	ciphertext         []byte
	version            string
	recipient          string
	localFileDigest    string
	secretStateUnknown bool
	preparedRemote     map[UnitID]UnitValue
}

// New 补齐运行平台和默认目标，资源仍在调用时使用。
func New(options Options) *Service {
	if options.Logger == nil {
		options.Logger = logging.NewNop()
	}
	if options.Tracer == nil {
		options.Tracer = noop.NewTracerProvider().Tracer("my/target")
	}
	if options.OS == "" {
		options.OS = runtime.GOOS
	}
	if len(options.Definitions) == 0 {
		options.Definitions = DefaultDefinitions()
	}
	return &Service{options: options}
}

// Capture 首次把显式目标的本机值发布到数据仓。
func (s *Service) Capture(ctx context.Context, input CaptureOptions) (result Result, err error) {
	ctx, progress := s.begin(ctx, "target.capture")
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
	if (len(input.Targets) == 0) == !input.All {
		return result, invalidInput()
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
	if !snapshot.Initialized {
		return result, problem(codeStateInvalid, phaseValidate, nil)
	}
	remote, err := parseRemoteTargets(snapshot, s.definitions())
	if err != nil {
		return result, err
	}
	definitions := s.definitions()
	targetIDs := append([]ID(nil), input.Targets...)
	if input.All {
		targetIDs = targetIDs[:0]
		for _, definition := range s.options.Definitions {
			if definition.CaptureAll {
				targetIDs = append(targetIDs, definition.ID)
			}
		}
	}
	if len(targetIDs) == 0 {
		return result, invalidInput()
	}
	files := map[string][]byte{}
	if !snapshot.ConfigInitialized {
		files[configManifestFile] = []byte("schema_version = 1\n")
	}
	baselines := map[ID]Baseline{}
	changedTargets := map[ID]bool{}
	for _, id := range targetIDs {
		definition, ok := definitions[id]
		if !ok || definition.RemoteAuthoritative {
			return result, invalidInput()
		}
		existing, remoteExists := remote[id]
		if remoteExists && !definition.Secret {
			return result, problem(codeInvalidInput, phaseValidate, nil)
		}
		local, readErr := definition.ReadLocal(s.options.Home, s.options.OS)
		if readErr != nil {
			if input.All && isNotExist(readErr) {
				continue
			}
			return result, readErr
		}
		scope, scopeErr := s.captureScope(definition, input.Machines)
		if scopeErr != nil {
			return result, scopeErr
		}
		if remoteExists && len(input.Machines) == 0 {
			scope = existing.scope
		}
		target := remoteTarget{id: id, scope: scope, units: existingUnits(local.Units), target: definition}
		if definition.Secret {
			key, err := s.ageKey()
			if err != nil {
				return result, err
			}
			unit, exists := local.Units[SecretUnitID]
			if !exists || !unit.Exists {
				return result, problem(codeInvalidInput, phaseValidate, nil)
			}
			if remoteExists &&
				existing.units[SecretUnitID].Digest == unit.Digest {
				baselines[id] = baselineFromUnits(id, local.Units)
				continue
			}
			ciphertext, encryptErr := key.Encrypt(unit.Data)
			if encryptErr != nil {
				return result, problem(codeStateInvalid, phaseStateWrite, encryptErr)
			}
			target.units = map[UnitID]UnitValue{
				SecretUnitID: {Exists: true, Digest: unit.Digest},
			}
			target.files = map[string][]byte{definition.Files[0].Name: ciphertext}
			target.version = unit.Digest
			target.recipient = key.Recipient()
		}
		targetFiles, encodeErr := remoteFilesForTarget(target)
		if encodeErr != nil {
			return result, encodeErr
		}
		maps.Copy(files, targetFiles)
		baselines[id] = baselineFromUnits(id, local.Units)
		changedTargets[id] = true
	}
	if len(baselines) == 0 {
		return result, invalidInput()
	}
	for _, baseline := range baselines {
		if err := baseline.Validate(); err != nil {
			return result, err
		}
	}
	progress.commit = snapshot.Metadata.Commit
	progress.step(ctx, "target.commit")
	metadata := snapshot.Metadata
	if len(files) != 0 {
		metadata, err = s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
			Message: "my: 收录目标",
			Files:   files,
		})
		if err != nil {
			return result, err
		}
	}
	if metadata.Commit != snapshot.Metadata.Commit {
		progress.confirmed(effectDataCommit)
	}
	progress.commit = metadata.Commit
	progress.step(ctx, phaseStateWrite)
	result = Result{Snapshot: metadata, Changed: len(files) != 0, Targets: make([]ItemResult, 0, len(baselines))}
	for _, id := range targetIDs {
		baseline, selected := baselines[id]
		if !selected {
			continue
		}
		if err := s.options.State.Write(metadata, baseline); err != nil {
			return result, err
		}
		progress.confirmed("target.baseline:" + string(id))
		result.Targets = append(result.Targets, ItemResult{
			Target: id, Changed: changedTargets[id], Changes: len(baseline.Units),
		})
		if changedTargets[id] {
			result.Changed = true
		}
	}
	return result, nil
}

// Diff 返回只读三方比较结果，不修改远端或本机状态。
func (s *Service) Diff(ctx context.Context, input DiffOptions) (result DiffResult, err error) {
	ctx, progress := s.begin(ctx, "target.diff")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if err := input.Validate(s.options.Definitions); err != nil {
		return DiffResult{}, err
	}

	if err := s.validate(); err != nil {
		return DiffResult{}, err
	}
	progress.step(ctx, phaseCompare)
	snapshot, plans, err := s.plan(ctx, input.Targets)
	result = DiffResult{Snapshot: snapshot.Metadata, Changes: []DiffChange{}, Conflicts: []Conflict{}}
	if err != nil {
		return result, err
	}
	for _, plan := range plans {
		for _, id := range sortedUnitIDs(plan.comparisons) {
			comparison := plan.comparisons[id]
			if comparison.Action == ActionUnchanged {
				continue
			}
			result.Changes = append(result.Changes, diffChange(plan, id, comparison))
			if comparison.Action == ActionConflict {
				result.Conflicts = append(
					result.Conflicts,
					Conflict{Target: plan.target.ID, Unit: id, Code: comparison.Code},
				)
			}
		}
	}
	return result, nil
}

func diffChange(plan targetPlan, id UnitID, comparison Comparison) DiffChange {
	result := DiffChange{
		Target: plan.target.ID,
		Unit:   id,
		Action: comparison.Action,
		Secret: plan.target.Kind == UnitSecret,
	}
	baseline, hasBaseline := plan.baseline.Units[id]
	local := plan.local[id]
	remote := plan.remote[id]
	result.Baseline = displayValue(plan.target.Kind, unitValueFromBaseline(baseline, hasBaseline))
	result.Local = displayValue(plan.target.Kind, local)
	result.Remote = displayValue(plan.target.Kind, remote)
	if plan.target.Kind == UnitFile {
		result.Diff = UnifiedTextDiff(
			string(plan.target.ID)+"/"+string(id),
			local.Data,
			remote.Data,
		)
	}
	return result
}

func displayValue(kind UnitKind, value UnitValue) string {
	if !value.Exists {
		return "<absent>"
	}
	if kind == UnitSecret {
		return value.Digest
	}
	return string(value.Data)
}

func unitValueFromBaseline(value BaselineValue, exists bool) UnitValue {
	if !exists {
		return UnitValue{}
	}
	return UnitValue{Exists: value.Exists, Digest: value.Digest, Data: value.Data}
}

// Sync 自动应用无冲突变化；存在冲突时不写本机或远端。
func (s *Service) Sync(ctx context.Context, input SyncOptions) (result Result, err error) {
	ctx, progress := s.begin(ctx, "target.sync")
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
	unlock, err := s.options.State.Lock(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()

	progress.step(ctx, phaseCompare)
	snapshot, plans, err := s.plan(ctx, input.Targets)
	if err != nil {
		return result, err
	}
	conflicts := planConflicts(plans)
	if len(conflicts) != 0 {
		return result, &ConflictError{Conflicts: conflicts}
	}
	// 在提交本轮普通变更前，先验证所有需要实际应用的秘密快照。
	for i := range plans {
		plan := &plans[i]
		values := valuesForAction(plan.comparisons, plan.remote, ActionApplyRemote)
		if plan.target.Kind == UnitSecret && len(values) != 0 {
			plan.preparedRemote, err = s.decryptSecretUnits(*plan, values)
			if err != nil {
				return result, err
			}
		}
	}
	remoteChanges := map[string][]byte{}
	for i := range plans {
		plan := &plans[i]
		changedUnits := map[UnitID]UnitValue{}
		for id, comparison := range plan.comparisons {
			if comparison.Action == ActionUploadLocal {
				changedUnits[id] = plan.local[id]
			}
		}
		if len(changedUnits) == 0 {
			continue
		}
		target := remoteTarget{
			id:     plan.target.ID,
			scope:  plan.scope,
			units:  unitOverlay(plan.allRemote, changedUnits),
			target: plan.target,
		}
		files, err := remoteFilesForTarget(target)
		if err != nil {
			return result, err
		}
		maps.Copy(remoteChanges, files)
		plan.allRemote = target.units
		plan.remote = unitOverlay(plan.remote, changedUnits)
	}
	progress.commit = snapshot.Metadata.Commit
	progress.step(ctx, "target.commit")
	metadata := snapshot.Metadata
	remoteCommitted := len(remoteChanges) != 0
	if remoteCommitted {
		metadata, err = s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
			Message: "my: 同步目标",
			Files:   remoteChanges,
		})
		if err != nil {
			return result, err
		}
	}
	if metadata.Commit != snapshot.Metadata.Commit {
		progress.confirmed(effectDataCommit)
	}
	progress.commit = metadata.Commit
	progress.step(ctx, phaseStateWrite)
	result = Result{Snapshot: metadata, Changed: remoteCommitted, Targets: []ItemResult{}}
	for _, plan := range plans {
		targetChanged := remoteCommitted && targetHasRemoteAction(plan)
		for _, comparison := range plan.comparisons {
			if comparison.Action == ActionAdopt || comparison.Action == ActionConverged {
				targetChanged = true
			}
		}
		remoteOnly := valuesForAction(plan.comparisons, plan.remote, ActionApplyRemote)
		if len(remoteOnly) != 0 {
			if plan.target.Kind == UnitSecret {
				decrypted := plan.preparedRemote
				plaintext := decrypted[SecretUnitID].Data
				pendingBaseline := plan.baseline
				pendingBaseline.Pending = &PendingSecret{
					Version:        plan.remote[SecretUnitID].Digest,
					BeforeDigest:   plan.localFileDigest,
					BeforeExists:   !plan.missingLocal,
					SnapshotDigest: UnitValueFromBytes(plaintext).Digest,
				}
				if err := s.options.State.Write(metadata, pendingBaseline); err != nil {
					return result, err
				}
				progress.confirmed("target.pending:" + string(plan.target.ID))
				remoteOnly = decrypted
			}
			progress.step(ctx, "target.local_write")
			if err := s.applyLocalUnits(plan.target, remoteOnly, progress); err != nil {
				return result, err
			}
			targetChanged = true
		}
		finalUnits := unitOverlay(plan.local, plan.remote)
		if err := s.options.State.Write(metadata, baselineFromUnits(plan.target.ID, finalUnits)); err != nil {
			return result, err
		}
		progress.confirmed("target.baseline:" + string(plan.target.ID))
		result.Targets = append(result.Targets, ItemResult{
			Target:  plan.target.ID,
			Changed: targetChanged,
			Changes: len(plan.comparisons),
		})
		if targetChanged {
			result.Changed = true
		}
	}
	if s.options.EnvironmentCache != nil {
		progress.step(ctx, "target.environment_cache")
		if err := s.options.EnvironmentCache.SyncCache(ctx); err != nil {
			return result, err
		}
	}
	return result, nil
}

// Resolve 显式采用一个冲突单位的一侧来源。
func (s *Service) Resolve(ctx context.Context, input ResolveOptions) (result Result, err error) {
	ctx, progress := s.begin(ctx, "target.resolve")
	defer progress.finish(&err)
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if err := input.Validate(s.options.Definitions); err != nil {
		return result, err
	}

	if err = input.Target.Validate(); err != nil {
		return result, err
	}
	if err := input.Unit.Validate(); err != nil {
		return result, err
	}
	if input.From != ResolveLocal && input.From != ResolveRemote {
		return result, invalidInput()
	}
	if err = s.validate(); err != nil {
		return result, err
	}
	unlock, err := s.options.State.Lock(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	snapshot, plans, err := s.plan(ctx, []ID{input.Target})
	if err != nil {
		return result, err
	}
	if len(plans) != 1 {
		return result, invalidInput()
	}
	plan := &plans[0]
	comparison, exists := plan.comparisons[input.Unit]
	if !exists || comparison.Action != ActionConflict {
		return result, invalidInput()
	}
	baseline, baselineExists, err := s.options.State.Read(snapshot.Metadata, input.Target)
	if err != nil {
		return result, err
	}
	if !baselineExists {
		baseline = Baseline{Target: input.Target, Units: map[UnitID]BaselineValue{}}
	}
	var chosen UnitValue
	switch input.From {
	case ResolveLocal:
		chosen = plan.local[input.Unit]
		if !chosen.Exists && plan.target.Kind == UnitFile {
			return result, invalidInput()
		}
		updated := remoteTarget{
			id:     plan.target.ID,
			scope:  plan.scope,
			units:  unitOverlay(plan.allRemote, map[UnitID]UnitValue{input.Unit: chosen}),
			target: plan.target,
		}
		if plan.target.Kind == UnitSecret {
			key, err := s.ageKey()
			if err != nil {
				return result, err
			}
			if len(chosen.Data) == 0 {
				return result, problem(codeInvalidInput, phaseValidate, nil)
			}
			ciphertext, encryptErr := key.Encrypt(chosen.Data)
			if encryptErr != nil {
				return result, problem(codeStateInvalid, phaseStateWrite, encryptErr)
			}
			updated.version = chosen.Digest
			updated.recipient = key.Recipient()
			updated.files = map[string][]byte{plan.target.Files[0].Name: ciphertext}
			updated.units[input.Unit] = UnitValue{Exists: true, Digest: chosen.Digest}
		}
		files, encodeErr := remoteFilesForTarget(updated)
		if encodeErr != nil {
			return result, encodeErr
		}
		metadata, commitErr := s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
			Message: "my: 解决目标冲突",
			Files:   files,
		})
		if commitErr != nil {
			return result, commitErr
		}
		progress.confirmed(effectDataCommit)
		progress.commit = metadata.Commit
		snapshot.Metadata = metadata
		plan.remote = updated.units
	case ResolveRemote:
		chosen = plan.remote[input.Unit]
		if plan.target.Kind == UnitSecret {
			decrypted, decryptErr := s.decryptSecretUnits(
				*plan,
				map[UnitID]UnitValue{input.Unit: chosen},
			)
			if decryptErr != nil {
				return result, decryptErr
			}
			pendingBaseline := baseline
			pendingBaseline.Pending = &PendingSecret{
				Version:        chosen.Digest,
				BeforeDigest:   plan.localFileDigest,
				BeforeExists:   !plan.missingLocal,
				SnapshotDigest: chosen.Digest,
			}
			if err := s.options.State.Write(snapshot.Metadata, pendingBaseline); err != nil {
				return result, err
			}
			progress.confirmed("target.pending:" + string(plan.target.ID))
			chosen = decrypted[input.Unit]
		}
		progress.step(ctx, "target.local_write")
		if err := s.applyLocalUnits(plan.target, map[UnitID]UnitValue{input.Unit: chosen}, progress); err != nil {
			return result, err
		}
		plan.local[input.Unit] = chosen
	}
	progress.commit = snapshot.Metadata.Commit
	progress.step(ctx, phaseStateWrite)
	baseline.Pending = nil
	baseline.Units[input.Unit] = baselineValueFromUnit(
		chosen,
		input.Target == CodexAuth || input.Target == ShellEnvSecret,
	)
	if err := s.options.State.Write(snapshot.Metadata, baseline); err != nil {
		return result, err
	}
	return Result{
		Snapshot: snapshot.Metadata,
		Changed:  true,
		Targets:  []ItemResult{{Target: input.Target, Changed: true, Changes: 1}},
	}, nil
}

// Remove 取消目标纳管并删除远端声明，保留本机文件。
func (s *Service) Remove(ctx context.Context, input RemoveOptions) (result Result, err error) {
	ctx, progress := s.begin(ctx, "target.remove")
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
	if len(input.Targets) == 0 {
		return result, invalidInput()
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
	deletions := []string{}
	for _, target := range input.Targets {
		if err := target.Validate(); err != nil {
			return result, err
		}
		paths := targetPaths(snapshot, target)
		if len(paths) == 0 {
			return result, invalidInput()
		}
		deletions = append(deletions, paths...)
	}
	metadata, err := s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
		Message:   "my: 移除目标",
		Deletions: deletions,
	})
	if err != nil {
		return result, err
	}
	if metadata.Commit != snapshot.Metadata.Commit {
		progress.confirmed(effectDataCommit)
	}
	progress.commit = metadata.Commit
	progress.step(ctx, phaseStateWrite)
	result = Result{Snapshot: metadata, Changed: true, Targets: []ItemResult{}}
	for _, target := range input.Targets {
		if err := s.options.State.Clear(metadata, target); err != nil {
			return result, err
		}
		progress.confirmed("target.baseline_removed:" + string(target))
		result.Targets = append(result.Targets, ItemResult{Target: target, Changed: true})
	}
	return result, nil
}

// String 返回用于诊断的稳定服务摘要。
func (s *Service) String() string {
	return fmt.Sprintf("target service for %s", s.options.MachineID)
}

func (s *Service) plan(
	ctx context.Context,
	targets []ID,
) (datarepo.Snapshot, []targetPlan, error) {
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return snapshot, nil, err
	}
	remote, err := parseRemoteTargets(snapshot, s.definitions())
	if err != nil {
		return snapshot, nil, err
	}
	selected := targets
	if len(selected) == 0 {
		for id := range remote {
			selected = append(selected, id)
		}
		slices.Sort(selected)
	}
	plans := make([]targetPlan, 0, len(selected))
	for _, id := range selected {
		remoteTarget, exists := remote[id]
		if !exists {
			continue
		}
		if !remoteTarget.scope.Applies(s.options.MachineID) {
			continue
		}
		definition := remoteTarget.target
		local, readErr := definition.ReadLocal(s.options.Home, s.options.OS)
		missingLocal := readErr != nil && isNotExist(readErr)
		if readErr != nil && !missingLocal {
			return snapshot, nil, readErr
		}
		baseline, hasBaseline, err := s.options.State.Read(snapshot.Metadata, id)
		if err != nil {
			return snapshot, nil, err
		}
		if !hasBaseline {
			baseline = Baseline{Target: id, Units: map[UnitID]BaselineValue{}}
		}
		localUnits := map[UnitID]UnitValue{}
		if !missingLocal {
			localUnits = local.Units
		}
		localFileDigest := ""
		secretStateUnknown := false
		if definition.Kind == UnitSecret {
			localFileDigest = localUnits[SecretUnitID].Digest
		}
		if definition.Kind == UnitSecret && hasBaseline {
			localSecret := localUnits[SecretUnitID]
			if !localSecret.Exists {
				missingLocal = true
			} else {
				effectiveVersion := baseline.Units[SecretUnitID].Digest
				if baseline.Pending != nil {
					switch localSecret.Digest {
					case baseline.Pending.SnapshotDigest:
						effectiveVersion = baseline.Pending.Version
					case baseline.Pending.BeforeDigest:
					default:
						secretStateUnknown = true
					}
				}
				localUnits = map[UnitID]UnitValue{
					SecretUnitID: {
						Exists: true,
						Digest: effectiveVersion,
					},
				}
			}
		}
		remoteUnits := applicableUnits(definition, remoteTarget.units, s.options.OS)
		baselineUnits := make(map[UnitID]BaselineValue, len(baseline.Units))
		for unit, value := range baseline.Units {
			if definition.Kind == UnitField {
				if _, ok := localTOMLPath(definition, unit, s.options.OS); !ok {
					continue
				}
			}
			baselineUnits[unit] = value
		}
		baseline.Units = baselineUnits
		ids := unitIDSet(localUnits, remoteUnits, baseline.Units)
		comparisons := make(map[UnitID]Comparison, len(ids))
		for _, unit := range ids {
			var baselinePtr *BaselineValue
			if value, ok := baseline.Units[unit]; ok {
				baselineCopy := value
				baselinePtr = &baselineCopy
			}
			localValue := localUnits[unit]
			remoteValue := remoteUnits[unit]
			comparison := Compare(definition.Kind, baselinePtr, localValue, remoteValue)
			if missingLocal && hasBaseline {
				comparison = Comparison{Action: ActionConflict, Code: fileMissingCode}
			}
			if secretStateUnknown {
				comparison = Comparison{Action: ActionConflict, Code: "target.secret_state_unknown"}
			}
			if definition.Kind == UnitSecret && missingLocal && hasBaseline && baseline.Pending != nil &&
				!baseline.Pending.BeforeExists && baseline.Pending.Version == remoteValue.Digest {
				comparison = Comparison{Action: ActionApplyRemote}
			}

			comparisons[unit] = comparison
		}
		plans = append(plans, targetPlan{
			target:             definition,
			scope:              remoteTarget.scope,
			local:              localUnits,
			remote:             remoteUnits,
			allRemote:          remoteTarget.units,
			baseline:           baseline,
			hasBaseline:        hasBaseline,
			missingLocal:       missingLocal,
			comparisons:        comparisons,
			ciphertext:         append([]byte(nil), remoteTarget.files[definition.Files[0].Name]...),
			version:            remoteTarget.version,
			recipient:          remoteTarget.recipient,
			localFileDigest:    localFileDigest,
			secretStateUnknown: secretStateUnknown,
		})
	}
	return snapshot, plans, nil
}

func (s *Service) applyLocalUnits(
	definition Definition,
	units map[UnitID]UnitValue,
	progress *operation,
) error {
	if definition.Kind == UnitSecret {
		value, exists := units[SecretUnitID]
		if !exists || !value.Exists || len(value.Data) == 0 || len(definition.Files) != 1 {
			return invalidInput()
		}
		err := AtomicWriteFile(
			localFilePath(s.options.Home, definition.Files[0]),
			value.Data,
			0o600,
		)
		if err == nil {
			progress.confirmed("target.local_write:" + string(definition.ID))
		}
		return err
	}
	if definition.Kind == UnitFile {
		for _, id := range sortedUnitIDs(units) {
			body := strings.TrimPrefix(string(id), fileUnitPrefix)
			file, ok := targetFileByName(definition, body)
			if !ok || !units[id].Exists {
				return invalidInput()
			}
			if err := AtomicWriteFile(localFilePath(s.options.Home, file), units[id].Data, 0o600); err != nil {
				return err
			}
			progress.confirmed("target.local_write:" + string(definition.ID))
		}
		return nil
	}
	for _, file := range definition.Files {
		patches := []TOMLPatch{}
		for id, value := range units {
			tomlPath, ok := localTOMLPath(definition, id, s.options.OS)
			if !ok {
				continue
			}
			patches = append(patches, TOMLPatch{
				Path:    tomlPath,
				Value:   value,
				Comment: localTOMLComment(definition, id),
			})
		}
		if len(patches) == 0 {
			continue
		}
		path := localFilePath(s.options.Home, file)
		data, err := readTargetFile(path, false)
		if err != nil {
			return err
		}
		if data == nil {
			data = []byte{}
		}
		updated, err := PatchTOML(data, patches)
		if err != nil {
			return err
		}
		if err := AtomicWriteFile(path, updated, 0o600); err != nil {
			return err
		}
		progress.confirmed("target.local_write:" + string(definition.ID))
	}
	return nil
}

func (s *Service) decryptSecretUnits(
	plan targetPlan,
	values map[UnitID]UnitValue,
) (map[UnitID]UnitValue, error) {
	key, err := s.ageKey()
	if err != nil {
		return nil, err
	}
	if len(plan.ciphertext) == 0 {
		return nil, problem(codeInvalidInput, phaseValidate, nil)
	}
	plaintext, err := key.Decrypt(plan.ciphertext, plan.recipient)
	if err != nil {
		return nil, problem(codeStateInvalid, phaseStateRead, err)
	}
	if len(plan.target.Files) != 1 {
		return nil, invalidInput()
	}
	if err := validateTargetData(plan.target.Files[0], plaintext); err != nil {
		return nil, err
	}
	result := make(map[UnitID]UnitValue, len(values))
	for id, value := range values {
		if digestBytes(plaintext) != value.Digest {
			return nil, problem(codeStateInvalid, phaseStateRead, nil)
		}
		value.Data = plaintext
		result[id] = value
	}
	return result, nil
}

func localTOMLPath(definition Definition, id UnitID, goos string) ([]string, bool) {
	parts, ok := fieldUnitPath(id)
	if !ok {
		return nil, false
	}
	switch definition.ID {
	case CodexSettings:
		if len(parts) == 2 && parts[0] == codexFilesystemTable {
			local, ok := codexFilesystemUnits(goos)[parts[1]]
			if !ok {
				return nil, false
			}
			return []string{codexPermissionsTable, codexDevelopmentPermission, codexFilesystemTable, local}, true
		}
		switch string(id) {
		case "field:default_permissions", "field:permissions.development.extends",
			"field:permissions.development.network.enabled", "field:features.network_proxy":
			return parts, true
		}
	case Mise:
		if len(parts) == 2 && parts[0] == miseToolsTable {
			return parts, true
		}
		if len(parts) == 2 && parts[0] == miseSettingsTable &&
			(parts[1] == "http_timeout" || parts[1] == "url_replacements") {
			return parts, true
		}
	}
	return nil, false
}

// applicableUnits 仅比较本平台可应用的单位，其他平台声明仍由 allRemote 原样保留。
func applicableUnits(definition Definition, units map[UnitID]UnitValue, goos string) map[UnitID]UnitValue {
	if definition.Kind != UnitField {
		return units
	}
	result := make(map[UnitID]UnitValue, len(units))
	for id, value := range units {
		if _, ok := localTOMLPath(definition, id, goos); ok {
			result[id] = value
		}
	}
	return result
}

//nolint:goconst // 工具组关键词同时参与跨平台路径命名，集中为列表更易审查。
func localTOMLComment(definition Definition, id UnitID) string {
	if definition.ID != CodexSettings {
		return ""
	}
	switch string(id) {
	case "field:default_permissions":
		return "默认使用 development 权限配置。"
	case "field:permissions.development.extends":
		return "继承 Codex 内置工作区权限。"
	case "field:permissions.development.network.enabled":
		return "允许开发命令联网下载依赖和访问开发服务。"
	case "field:features.network_proxy":
		return "不启用 Codex 自带的网络过滤代理。"
	}
	body := strings.TrimPrefix(string(id), fieldUnitPrefix)
	if !strings.HasPrefix(body, codexFilesystemTable+".") {
		return ""
	}
	logical := strings.TrimPrefix(body, codexFilesystemTable+".")
	group := "开发工具"
	switch {
	case containsAny(logical, "gradle", "m2", "ivy2", "sbt", "coursier", "jbang", "konan", "java"):
		group = "JVM/Java"
	case containsAny(logical, "go"):
		group = "Go"
	case containsAny(logical, "npm", "pnpm", "yarn", "node"):
		group = "Node.js"
	case containsAny(logical, "uv", "pip", "poetry", "pdm", "pipx"):
		group = "Python"
	case containsAny(logical, "mise"):
		group = "mise"
	case containsAny(logical, "serena", "gitnexus"):
		group = "Agent"
	case containsAny(logical, "rtk"):
		group = "RTK"
	case containsAny(logical, "ccache", "sccache", "conan", "vcpkg", "cmake", "meson"):
		group = "原生构建工具"
	case containsAny(logical, "dotnet", "nuget"):
		group = ".NET"
	}
	return "允许 Codex 写入 " + group + " 开发工具目录。"
}

func containsAny(value string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(value, part) {
			return true
		}
	}
	return false
}

func (s *Service) captureScope(
	definition Definition,
	explicit []string,
) (MachineScope, error) {
	if len(explicit) == 0 {
		return definition.DefaultScope(s.options.MachineID), nil
	}
	scope := MachineScope{IDs: append([]string(nil), explicit...)}
	if err := scope.Validate(s.options.KnownMachines); err != nil {
		return MachineScope{}, err
	}
	return scope, nil
}

func (s *Service) validate() error {
	if s.options.Repository == nil || s.options.State == nil ||
		s.options.Home == "" || s.options.MachineID == "" {
		return invalidInput()
	}
	if !filepath.IsAbs(s.options.Home) {
		return invalidInput()
	}
	definitions := s.definitions()
	for _, definition := range s.options.Definitions {
		if err := definition.Validate(); err != nil {
			return err
		}
		if _, duplicate := definitions[definition.ID]; !duplicate {
			definitions[definition.ID] = definition
		}
	}
	return nil
}

func (s *Service) definitions() map[ID]Definition {
	result := make(map[ID]Definition, len(s.options.Definitions))
	for _, definition := range s.options.Definitions {
		result[definition.ID] = definition
	}
	return result
}

func existingUnits(values map[UnitID]UnitValue) map[UnitID]UnitValue {
	result := map[UnitID]UnitValue{}
	for id, value := range values {
		if value.Exists {
			result[id] = value
		}
	}
	return result
}

func baselineFromUnits(target ID, units map[UnitID]UnitValue) Baseline {
	secret := target == CodexAuth || target == ShellEnvSecret
	result := Baseline{Target: target, Units: make(map[UnitID]BaselineValue, len(units))}
	for id, value := range units {
		result.Units[id] = baselineValueFromUnit(value, secret)
	}
	return result
}

func baselineValueFromUnit(value UnitValue, secret bool) BaselineValue {
	result := BaselineValue{
		Exists: value.Exists,
		Secret: secret,
		Digest: value.Digest,
	}
	if !secret {
		result.Data = append([]byte(nil), value.Data...)
	}
	return result
}

func unitIDSet(sources ...any) []UnitID {
	set := map[UnitID]struct{}{}
	for _, raw := range sources {
		switch values := raw.(type) {
		case map[UnitID]UnitValue:
			for id := range values {
				set[id] = struct{}{}
			}
		case map[UnitID]BaselineValue:
			for id := range values {
				set[id] = struct{}{}
			}
		}
	}
	result := make([]UnitID, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	slices.Sort(result)
	return result
}

func sortedUnitIDs[V any](values map[UnitID]V) []UnitID {
	result := make([]UnitID, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	slices.Sort(result)
	return result
}

func planConflicts(plans []targetPlan) []Conflict {
	result := []Conflict{}
	for _, plan := range plans {
		for _, id := range sortedUnitIDs(plan.comparisons) {
			comparison := plan.comparisons[id]
			if comparison.Action == ActionConflict {
				result = append(result, Conflict{Target: plan.target.ID, Unit: id, Code: comparison.Code})
			}
		}
	}
	return result
}

func targetHasRemoteAction(plan targetPlan) bool {
	for _, comparison := range plan.comparisons {
		if comparison.Action == ActionUploadLocal {
			return true
		}
	}
	return false
}

func valuesForAction(
	comparisons map[UnitID]Comparison,
	source map[UnitID]UnitValue,
	action Action,
) map[UnitID]UnitValue {
	result := map[UnitID]UnitValue{}
	for id, comparison := range comparisons {
		if comparison.Action == action {
			result[id] = source[id]
		}
	}
	return result
}
