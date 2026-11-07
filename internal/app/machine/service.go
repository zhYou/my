package machine

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/datarepo"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Repository 是应用使用的一致快照与 CAS 提交边界。
type Repository interface {
	Read(context.Context) (datarepo.Snapshot, error)
	Commit(context.Context, datarepo.Snapshot, map[string][]byte) (datarepo.Metadata, error)
}

// Options 显式组合业务依赖，不读取进程全局状态或凭据。
type Options struct {
	Config     datarepo.Config
	Repository Repository
	State      State
	OS         string
	Arch       string
	Now        func() time.Time
	Logger     *logging.Logger
	Tracer     trace.Tracer
	Localizer  *messages.Localizer
}

// Service 执行机器和数据仓业务，每次 CLI 调用独立创建。
type Service struct{ options Options }

// New 补齐平台、时钟和空观测依赖，资源仍在实际调用时取得。
func New(options Options) *Service {
	if options.OS == "" {
		options.OS = runtime.GOOS
	}
	if options.Arch == "" {
		options.Arch = runtime.GOARCH
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = logging.NewNop()
	}
	if options.Tracer == nil {
		options.Tracer = noop.NewTracerProvider().Tracer("my.machine")
	}
	return &Service{options: options}
}

// InitResult 描述格式初始化是否产生新提交。
type InitResult struct {
	Changed  bool              `json:"changed"`
	Snapshot datarepo.Metadata `json:"snapshot"`
}

// ChangeResult 仅在远端及本机操作均完成时作为成功输出。
type ChangeResult struct {
	Machine  Record            `json:"machine"`
	Snapshot datarepo.Metadata `json:"snapshot"`
	Changed  bool              `json:"changed"`
	Bound    bool              `json:"bound"`
}

// ListResult 始终编码机器数组，包括空清单。
type ListResult struct {
	Machines []Record          `json:"machines"`
	Snapshot datarepo.Metadata `json:"snapshot"`
}

// ShowResult 包含指定机器及读取版本。
type ShowResult struct {
	Machine  Record            `json:"machine"`
	Snapshot datarepo.Metadata `json:"snapshot"`
}

// Initialize 显式初始化格式；缺少标记但已有 machines 时拒绝接管。
func (s *Service) Initialize(ctx context.Context) (result InitResult, err error) {
	ctx, finish := s.observe(ctx, "data.init")
	defer func() { finish(err, result.Changed, result.Snapshot) }()
	if err = s.ready(ctx); err != nil {
		return result, err
	}
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return result, err
	}
	result.Snapshot = snapshot.Metadata
	if snapshot.Initialized {
		_, err = Records(snapshot)
		return result, err
	}
	if snapshot.HasMachines {
		return result, problem(codeRecordInvalid, "data.validate", nil, []string{}, recoverRecord)
	}
	metadata, err := s.options.Repository.Commit(
		ctx,
		snapshot,
		map[string][]byte{"my.toml": []byte("schema_version = 1\n")},
	)
	if err != nil {
		return result, err
	}
	result.Snapshot = metadata
	result.Changed = true
	return result, nil
}

// List 在线读取完整机器清单，按机器 ID 稳定排序。
func (s *Service) List(ctx context.Context) (ListResult, error) {
	snapshot, records, err := s.read(ctx)
	result := ListResult{Snapshot: snapshot.Metadata, Machines: []Record{}}
	if err != nil {
		return result, err
	}
	for _, record := range records {
		result.Machines = append(result.Machines, record)
	}
	sort.Slice(
		result.Machines,
		func(i, j int) bool { return result.Machines[i].MachineID < result.Machines[j].MachineID },
	)
	return result, nil
}

// Show 在线显示一条记录，包括退役状态。
func (s *Service) Show(ctx context.Context, id string) (ShowResult, error) {
	if err := ValidateID(id); err != nil {
		return ShowResult{}, err
	}
	snapshot, records, err := s.read(ctx)
	if err != nil {
		return ShowResult{}, err
	}
	record, ok := records[id]
	if !ok {
		return ShowResult{}, problem(codeNotFound, phaseRead, nil, []string{}, recoverRecord)
	}
	return ShowResult{Machine: record, Snapshot: snapshot.Metadata}, nil
}

// Current 只读取当前本机绑定，不创建状态、不访问仓库。
func (s *Service) Current(ctx context.Context) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	if s.options.State == nil {
		return Binding{}, problem(
			codeStateInvalid,
			phaseBindingRead,
			nil,
			[]string{},
			recoverLocal,
		)
	}
	binding, ok, err := s.options.State.Read()
	if err != nil {
		return Binding{}, problem(
			codeStateInvalid,
			phaseBindingRead,
			err,
			[]string{},
			recoverLocal,
		)
	}
	if !ok {
		return Binding{}, problem("machine.not_bound", phaseBindingRead, nil, []string{}, "machine.recover_bind")
	}
	return binding, nil
}

// Register 创建 active 登记；相同资料可在重跑时补齐本机绑定。
func (s *Service) Register(ctx context.Context, input RegisterOptions) (result ChangeResult, err error) {
	if err = input.Validate(); err != nil {
		return result, err
	}
	ctx, finish := s.observe(ctx, "machine.register")
	defer func() { finish(err, result.Changed, result.Snapshot) }()
	return s.locked(ctx, input.ID, true, func(ctx context.Context) (ChangeResult, error) {
		snapshot, records, err := s.read(ctx)
		if err != nil {
			return ChangeResult{}, err
		}
		if err := s.checkIdentity(snapshot.Metadata, input.ID); err != nil {
			return ChangeResult{}, err
		}
		proposed := Record{
			SchemaVersion: 1,
			MachineID:     input.ID,
			Kind:          input.Kind,
			OS:            s.options.OS,
			Arch:          s.options.Arch,
			Note:          input.Note,
			RegisteredAt:  s.options.Now().UTC(),
			Status:        Active,
		}
		current, exists := records[input.ID]
		changed := !exists
		if exists {
			if current.Status != Active || current.Kind != proposed.Kind || current.OS != proposed.OS ||
				current.Arch != proposed.Arch ||
				current.Note != proposed.Note {
				return ChangeResult{}, problem(
					"machine.record_conflict",
					phaseValidate,
					nil,
					[]string{},
					recoverRecord,
				)
			}
			proposed = current
		}
		return s.saveAndBind(ctx, snapshot, proposed, changed)
	})
}

// Bind 恢复现有记录；重新激活和平台变化必须由调用者显式允许。
func (s *Service) Bind(ctx context.Context, input BindOptions) (result ChangeResult, err error) {
	if err = input.Validate(); err != nil {
		return result, err
	}
	ctx, finish := s.observe(ctx, "machine.bind")
	defer func() { finish(err, result.Changed, result.Snapshot) }()
	return s.locked(ctx, input.ID, true, func(ctx context.Context) (ChangeResult, error) {
		snapshot, records, err := s.read(ctx)
		if err != nil {
			return ChangeResult{}, err
		}
		if err := s.checkIdentity(snapshot.Metadata, input.ID); err != nil {
			return ChangeResult{}, err
		}
		record, ok := records[input.ID]
		if !ok {
			return ChangeResult{}, problem(
				codeNotFound,
				phaseRead,
				nil,
				[]string{},
				recoverRecord,
			)
		}
		changed := false
		if record.Status == Retired {
			if !input.Reactivate {
				return ChangeResult{}, problem(
					"machine.reactivate_required",
					phaseValidate,
					nil,
					[]string{},
					"machine.recover_reactivate",
				)
			}
			record.Status = Active
			record.RetiredAt = nil
			changed = true
		}
		if record.OS != s.options.OS || record.Arch != s.options.Arch {
			if !input.UpdatePlatform {
				return ChangeResult{}, problem(
					"machine.platform_required",
					phaseValidate,
					nil,
					[]string{},
					"machine.recover_platform",
				)
			}
			record.OS = s.options.OS
			record.Arch = s.options.Arch
			changed = true
		}
		return s.saveAndBind(ctx, snapshot, record, changed)
	})
}

// Retire 保留记录及登记时间，仅清除精确匹配的当前绑定。
func (s *Service) Retire(ctx context.Context, id string) (result ChangeResult, err error) {
	if err = ValidateID(id); err != nil {
		return result, err
	}
	ctx, finish := s.observe(ctx, "machine.retire")
	defer func() { finish(err, result.Changed, result.Snapshot) }()
	return s.locked(ctx, id, false, func(ctx context.Context) (ChangeResult, error) {
		snapshot, records, err := s.read(ctx)
		if err != nil {
			return ChangeResult{}, err
		}
		record, ok := records[id]
		if !ok {
			return ChangeResult{}, problem(
				codeNotFound,
				phaseRead,
				nil,
				[]string{},
				recoverRecord,
			)
		}
		changed := record.Status != Retired
		effects := []string{}
		if changed {
			now := s.options.Now().UTC()
			record.Status = Retired
			record.RetiredAt = &now
			snapshot.Metadata, err = s.commitRecord(ctx, snapshot, record)
			if err != nil {
				return ChangeResult{}, err
			}
			effects = append(effects, effectCommit)
		}
		expected := bindingFor(snapshot.Metadata, id)
		local, bound, readErr := s.options.State.Read()
		if readErr != nil {
			return ChangeResult{}, bindingFailure(phaseBindingClear, readErr, effects, snapshot.Metadata)
		}
		if bound && local == expected {
			ctx, span := s.options.Tracer.Start(ctx, phaseBindingClear)
			err = s.options.State.Clear(expected)
			if err != nil {
				err = bindingFailure(phaseBindingClear, err, effects, snapshot.Metadata)
			}
			s.endPhase(span, err)
			if err != nil {
				return ChangeResult{}, err
			}
			_ = ctx
			bound = false
		}
		return ChangeResult{
			Machine:  record,
			Snapshot: snapshot.Metadata,
			Changed:  changed,
			Bound:    bound && local == expected,
		}, nil
	})
}

func (s *Service) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.options.Config.Validate(); err != nil {
		return err
	}
	if s.options.Repository == nil {
		return problem(codeRecordInvalid, phaseRead, nil, []string{}, recoverRecord)
	}
	return nil
}

func (s *Service) read(ctx context.Context) (datarepo.Snapshot, map[string]Record, error) {
	if err := s.ready(ctx); err != nil {
		return datarepo.Snapshot{}, nil, err
	}
	snapshot, err := s.options.Repository.Read(ctx)
	if err != nil {
		return snapshot, nil, err
	}
	records, err := Records(snapshot)
	return snapshot, records, err
}

func (s *Service) locked(
	ctx context.Context,
	id string,
	check bool,
	run func(context.Context) (ChangeResult, error),
) (result ChangeResult, err error) {
	if err = s.ready(ctx); err != nil {
		return result, err
	}
	if s.options.State == nil {
		return result, problem(codeStateInvalid, phaseLock, nil, []string{}, recoverLocal)
	}
	probe, cancel := context.WithTimeout(ctx, s.options.Config.RequestTimeout)
	unlock, err := s.options.State.Lock(probe)
	cancel()
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			err = &datarepo.RequestTimeoutError{Cause: err}
		}
		return result, problem(codeStateInvalid, phaseLock, err, []string{}, recoverLocal)
	}
	defer func() {
		if closeErr := unlock(); closeErr != nil {
			effects := []string(nil)
			if err == nil && result.Changed {
				effects = []string{effectCommit}
			}
			err = errors.Join(
				err,
				problem(codeStateInvalid, "machine.unlock", closeErr, effects, recoverLocal),
			)
		}
	}()
	if check {
		local, exists, readErr := s.options.State.Read()
		if readErr != nil {
			return result, problem(
				codeStateInvalid,
				phaseBindingRead,
				readErr,
				[]string{},
				recoverLocal,
			)
		}
		if exists &&
			(!strings.EqualFold(local.Repository, s.options.Config.Repository) || local.Branch != s.options.Config.Branch || local.MachineID != id) {
			return result, problem(
				codeBindingConflict,
				phaseValidate,
				nil,
				[]string{},
				recoverLocal,
			)
		}
	}
	return run(ctx)
}

func (s *Service) checkIdentity(metadata datarepo.Metadata, id string) error {
	current, exists, err := s.options.State.Read()
	if err != nil {
		return problem(codeStateInvalid, phaseBindingRead, err, []string{}, recoverLocal)
	}
	if exists && current != bindingFor(metadata, id) {
		return problem(codeBindingConflict, phaseValidate, nil, []string{}, recoverLocal)
	}
	return nil
}

func (s *Service) commitRecord(
	ctx context.Context,
	snapshot datarepo.Snapshot,
	record Record,
) (datarepo.Metadata, error) {
	data, err := encodeRecord(record)
	if err != nil {
		return snapshot.Metadata, problem(
			codeRecordInvalid,
			phaseValidate,
			err,
			[]string{},
			recoverRecord,
		)
	}
	return s.options.Repository.Commit(ctx, snapshot, map[string][]byte{"machines/" + record.MachineID + ".toml": data})
}

func (s *Service) saveAndBind(
	ctx context.Context,
	snapshot datarepo.Snapshot,
	record Record,
	changed bool,
) (ChangeResult, error) {
	effects := []string{}
	if changed {
		metadata, err := s.commitRecord(ctx, snapshot, record)
		if err != nil {
			return ChangeResult{}, err
		}
		snapshot.Metadata = metadata
		effects = append(effects, effectCommit)
	}
	if err := ctx.Err(); err != nil {
		return ChangeResult{}, bindingFailure(phaseBindingWrite, err, effects, snapshot.Metadata)
	}
	_, span := s.options.Tracer.Start(ctx, phaseBindingWrite)
	err := s.options.State.Write(bindingFor(snapshot.Metadata, record.MachineID))
	if err != nil {
		err = bindingFailure(phaseBindingWrite, err, effects, snapshot.Metadata)
	}
	s.endPhase(span, err)
	if err != nil {
		return ChangeResult{}, err
	}
	return ChangeResult{Machine: record, Snapshot: snapshot.Metadata, Changed: changed, Bound: true}, nil
}

func bindingFor(metadata datarepo.Metadata, id string) Binding {
	return Binding{
		SchemaVersion: 1,
		Repository:    metadata.Repository,
		RepositoryID:  metadata.RepositoryID,
		Branch:        metadata.Branch,
		MachineID:     id,
	}
}
