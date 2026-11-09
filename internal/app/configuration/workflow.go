package configuration

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/zhYou/my/internal/datarepo"
)

// Repository 是配置业务使用的一致快照和 CAS 提交边界。
type Repository interface {
	Read(context.Context) (datarepo.Snapshot, error)
	CommitChanges(context.Context, datarepo.Snapshot, datarepo.Changes) (datarepo.Metadata, error)
}

// Options 显式注入配置业务依赖。
type Options struct {
	Repository    Repository
	State         State
	Home          string
	OS            string
	MachineID     string
	KnownMachines []string
	Definitions   []TargetDefinition
}

// Service 执行目标收录、差异、同步、冲突解决和移除。
type Service struct {
	options Options
}

// CaptureOptions 声明首次收录目标和机器范围。
type CaptureOptions struct {
	Targets  []TargetID
	All      bool
	Machines []string
}

// DiffOptions 选择只读比较的目标。
type DiffOptions struct {
	Targets []TargetID
}

// SyncOptions 选择双向同步的目标。
type SyncOptions struct {
	Targets []TargetID
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
	Target TargetID
	Unit   UnitID
	From   ResolveSource
}

// RemoveOptions 指定需要取消纳管的目标。
type RemoveOptions struct {
	Targets []TargetID
}

// TargetResult 汇总一个目标的变化。
type TargetResult struct {
	Target    TargetID `json:"target"`
	Changed   bool     `json:"changed"`
	Changes   int      `json:"changes"`
	Conflicts int      `json:"conflicts"`
}

// Result 是写操作完成后返回的稳定结果。
type Result struct {
	Snapshot datarepo.Metadata `json:"snapshot"`
	Targets  []TargetResult    `json:"targets"`
	Changed  bool              `json:"changed"`
}

// DiffChange 描述一个单位的只读比较结果。
type DiffChange struct {
	Target TargetID `json:"target"`
	Unit   UnitID   `json:"unit"`
	Action Action   `json:"action"`
}

// Conflict 描述需要显式来源选择的单位。
type Conflict struct {
	Target TargetID `json:"target"`
	Unit   UnitID   `json:"unit"`
	Code   string   `json:"code"`
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
	return "configuration.conflicts"
}

type targetPlan struct {
	target       TargetDefinition
	scope        MachineScope
	local        map[UnitID]UnitValue
	remote       map[UnitID]UnitValue
	baseline     Baseline
	hasBaseline  bool
	missingLocal bool
	comparisons  map[UnitID]Comparison
}

// New 补齐运行平台和默认目标，资源仍在调用时使用。
func New(options Options) *Service {
	if options.OS == "" {
		options.OS = runtime.GOOS
	}
	if len(options.Definitions) == 0 {
		options.Definitions = DefaultTargets()
	}
	return &Service{options: options}
}

// Capture 首次把显式目标的本机值发布到数据仓。
func (s *Service) Capture(ctx context.Context, input CaptureOptions) (result Result, err error) {
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
	defer func() { err = errors.Join(err, unlock()) }()

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
	targetIDs := append([]TargetID(nil), input.Targets...)
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
	baselines := map[TargetID]Baseline{}
	for _, id := range targetIDs {
		definition, ok := definitions[id]
		if !ok || !definition.CaptureAll {
			return result, invalidInput()
		}
		if _, exists := remote[id]; exists {
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
		target := remoteTarget{
			id:     id,
			scope:  scope,
			units:  existingUnits(local.Units),
			target: definition,
		}
		targetFiles, encodeErr := remoteFilesForTarget(target)
		if encodeErr != nil {
			return result, encodeErr
		}
		maps.Copy(files, targetFiles)
		baselines[id] = baselineFromUnits(id, local.Units)
	}
	if len(baselines) == 0 {
		return result, invalidInput()
	}
	metadata, err := s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
		Message: "my: capture configuration",
		Files:   files,
	})
	if err != nil {
		return result, err
	}
	result = Result{Snapshot: metadata, Changed: true, Targets: make([]TargetResult, 0, len(baselines))}
	for _, id := range targetIDs {
		baseline, selected := baselines[id]
		if !selected {
			continue
		}
		if err := s.options.State.Write(metadata, baseline); err != nil {
			return result, err
		}
		result.Targets = append(result.Targets, TargetResult{Target: id, Changed: true, Changes: len(baseline.Units)})
	}
	return result, nil
}

// Diff 返回只读三方比较结果，不修改远端或本机状态。
func (s *Service) Diff(ctx context.Context, input DiffOptions) (DiffResult, error) {
	if err := s.validate(); err != nil {
		return DiffResult{}, err
	}
	snapshot, plans, err := s.plan(ctx, input.Targets)
	result := DiffResult{Snapshot: snapshot.Metadata, Changes: []DiffChange{}, Conflicts: []Conflict{}}
	if err != nil {
		return result, err
	}
	for _, plan := range plans {
		for _, id := range sortedUnitIDs(plan.comparisons) {
			comparison := plan.comparisons[id]
			if comparison.Action == ActionUnchanged {
				continue
			}
			result.Changes = append(
				result.Changes,
				DiffChange{Target: plan.target.ID, Unit: id, Action: comparison.Action},
			)
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

// Sync 自动应用无冲突变化；存在冲突时不写本机或远端。
func (s *Service) Sync(ctx context.Context, input SyncOptions) (result Result, err error) {
	if err = s.validate(); err != nil {
		return result, err
	}
	unlock, err := s.options.State.Lock(ctx)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, unlock()) }()

	snapshot, plans, err := s.plan(ctx, input.Targets)
	if err != nil {
		return result, err
	}
	conflicts := planConflicts(plans)
	if len(conflicts) != 0 {
		return result, &ConflictError{Conflicts: conflicts}
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
			units:  unitOverlay(plan.remote, changedUnits),
			target: plan.target,
		}
		files, err := remoteFilesForTarget(target)
		if err != nil {
			return result, err
		}
		maps.Copy(remoteChanges, files)
		plan.remote = target.units
	}
	metadata := snapshot.Metadata
	remoteCommitted := len(remoteChanges) != 0
	if remoteCommitted {
		metadata, err = s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
			Message: "my: sync configuration",
			Files:   remoteChanges,
		})
		if err != nil {
			return result, err
		}
	}
	result = Result{Snapshot: metadata, Changed: remoteCommitted, Targets: []TargetResult{}}
	for _, plan := range plans {
		targetChanged := remoteCommitted && targetHasRemoteAction(plan)
		for _, comparison := range plan.comparisons {
			if comparison.Action == ActionAdopt || comparison.Action == ActionConverged {
				targetChanged = true
			}
		}
		remoteOnly := valuesForAction(plan.comparisons, plan.remote, ActionApplyRemote)
		if len(remoteOnly) != 0 {
			if err := s.applyLocalUnits(plan.target, remoteOnly); err != nil {
				return result, err
			}
			targetChanged = true
		}
		finalUnits := unitOverlay(plan.local, plan.remote)
		if err := s.options.State.Write(metadata, baselineFromUnits(plan.target.ID, finalUnits)); err != nil {
			return result, err
		}
		result.Targets = append(result.Targets, TargetResult{
			Target:  plan.target.ID,
			Changed: targetChanged,
			Changes: len(plan.comparisons),
		})
		if targetChanged {
			result.Changed = true
		}
	}
	return result, nil
}

// Resolve 显式采用一个冲突单位的一侧来源。
func (s *Service) Resolve(ctx context.Context, input ResolveOptions) (result Result, err error) {
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
	defer func() { err = errors.Join(err, unlock()) }()
	snapshot, plans, err := s.plan(ctx, []TargetID{input.Target})
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
			units:  unitOverlay(plan.remote, map[UnitID]UnitValue{input.Unit: chosen}),
			target: plan.target,
		}
		files, encodeErr := remoteFilesForTarget(updated)
		if encodeErr != nil {
			return result, encodeErr
		}
		metadata, commitErr := s.options.Repository.CommitChanges(ctx, snapshot, datarepo.Changes{
			Message: "my: resolve configuration conflict",
			Files:   files,
		})
		if commitErr != nil {
			return result, commitErr
		}
		snapshot.Metadata = metadata
		plan.remote = updated.units
	case ResolveRemote:
		chosen = plan.remote[input.Unit]
		if err := s.applyLocalUnits(plan.target, map[UnitID]UnitValue{input.Unit: chosen}); err != nil {
			return result, err
		}
		plan.local[input.Unit] = chosen
	}
	baseline, exists, err := s.options.State.Read(snapshot.Metadata, input.Target)
	if err != nil {
		return result, err
	}
	if !exists {
		baseline = Baseline{Target: input.Target, Units: map[UnitID]BaselineValue{}}
	}
	baseline.Units[input.Unit] = baselineValueFromUnit(
		chosen,
		input.Target == TargetCodexAuth || input.Target == TargetShellEnvSecret,
	)
	if err := s.options.State.Write(snapshot.Metadata, baseline); err != nil {
		return result, err
	}
	return Result{
		Snapshot: snapshot.Metadata,
		Changed:  true,
		Targets:  []TargetResult{{Target: input.Target, Changed: true, Changes: 1}},
	}, nil
}

// Remove 取消目标纳管并删除远端声明，保留本机文件。
func (s *Service) Remove(ctx context.Context, input RemoveOptions) (result Result, err error) {
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
	defer func() { err = errors.Join(err, unlock()) }()
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
		Message:   "my: remove configuration target",
		Deletions: deletions,
	})
	if err != nil {
		return result, err
	}
	result = Result{Snapshot: metadata, Changed: true, Targets: []TargetResult{}}
	for _, target := range input.Targets {
		if err := s.options.State.Clear(metadata, target); err != nil {
			return result, err
		}
		result.Targets = append(result.Targets, TargetResult{Target: target, Changed: true})
	}
	return result, nil
}

// String 返回用于诊断的稳定服务摘要。
func (s *Service) String() string {
	return fmt.Sprintf("configuration service for %s", s.options.MachineID)
}

func (s *Service) plan(
	ctx context.Context,
	targets []TargetID,
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
		ids := unitIDSet(localUnits, remoteTarget.units, baseline.Units)
		comparisons := make(map[UnitID]Comparison, len(ids))
		for _, unit := range ids {
			var baselinePtr *BaselineValue
			if value, ok := baseline.Units[unit]; ok {
				baselineCopy := value
				baselinePtr = &baselineCopy
			}
			localValue := localUnits[unit]
			remoteValue := remoteTarget.units[unit]
			comparison := Compare(definition.Kind, baselinePtr, localValue, remoteValue)
			if missingLocal && hasBaseline {
				comparison = Comparison{Action: ActionConflict, Code: fileMissingCode}
			}
			comparisons[unit] = comparison
		}
		plans = append(plans, targetPlan{
			target:       definition,
			scope:        remoteTarget.scope,
			local:        localUnits,
			remote:       remoteTarget.units,
			baseline:     baseline,
			hasBaseline:  hasBaseline,
			missingLocal: missingLocal,
			comparisons:  comparisons,
		})
	}
	return snapshot, plans, nil
}

func (s *Service) applyLocalUnits(
	definition TargetDefinition,
	units map[UnitID]UnitValue,
) error {
	if definition.Kind == UnitFile {
		for id := range units {
			body := strings.TrimPrefix(string(id), fileUnitPrefix)
			file, ok := targetFileByName(definition, body)
			if !ok || !units[id].Exists {
				return invalidInput()
			}
			if err := AtomicWriteFile(localFilePath(s.options.Home, file), units[id].Data, 0o600); err != nil {
				return err
			}
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
			patches = append(patches, TOMLPatch{Path: tomlPath, Value: value})
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
	}
	return nil
}

func localTOMLPath(definition TargetDefinition, id UnitID, goos string) ([]string, bool) {
	body := strings.TrimPrefix(string(id), fieldUnitPrefix)
	if body == string(id) || body == "" {
		return nil, false
	}
	if definition.ID == TargetCodexSettings && strings.HasPrefix(body, codexFilesystemTable+".") {
		logical := strings.TrimPrefix(body, codexFilesystemTable+".")
		local, ok := codexFilesystemUnits(goos)[logical]
		if !ok {
			return nil, false
		}
		return []string{codexPermissionsTable, codexDevelopmentPermission, codexFilesystemTable, local}, true
	}
	return strings.Split(body, "."), true
}

func (s *Service) captureScope(
	definition TargetDefinition,
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

func (s *Service) definitions() map[TargetID]TargetDefinition {
	result := make(map[TargetID]TargetDefinition, len(s.options.Definitions))
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

func baselineFromUnits(target TargetID, units map[UnitID]UnitValue) Baseline {
	secret := target == TargetCodexAuth || target == TargetShellEnvSecret
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

func sortedUnitIDs(values map[UnitID]Comparison) []UnitID {
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
