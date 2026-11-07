package fault

import (
	"errors"
	"maps"
	"slices"
)

// Diagnostic 描述失败阶段和已确认副作用；nil 列表表示未知，空列表表示确认没有副作用。
// 列表中的稳定标识只报告已确认事实，不承诺穷尽所有副作用。
type Diagnostic struct {
	Phase            string
	CompletedEffects []string
	Recovery         *Recovery
}

// Recovery 保存恢复说明的双语消息 ID 与安全参数，不执行恢复动作。
type Recovery struct {
	Code   string
	Params Params
}

// DisplayCause 返回可显示的原因；敏感原因仅通过 Unwrap 遍历。
func (e *Error) DisplayCause() error {
	if e.sensitiveCause {
		return nil
	}
	return e.Cause
}

// WithSensitiveCause 隐藏原因的诊断文本，保留错误链及已审查的领域参数。
func WithSensitiveCause() Option {
	return func(e *Error) { e.sensitiveCause = true }
}

// WithDiagnostic 快照诊断信息；参数的嵌套值须不可变。
func WithDiagnostic(diagnostic Diagnostic) Option {
	snapshot := cloneDiagnostic(diagnostic)
	return func(e *Error) { e.Diagnostic = cloneDiagnostic(snapshot) }
}

// DiagnosticOf 沿单条错误链取最近的完整诊断；聚合错误应先使用 Details 逐分支处理。
func DiagnosticOf(err error) (Diagnostic, bool) {
	for err != nil {
		//nolint:errorlint // 逐层查找最近的诊断，避免 errors.As 跳过外层及聚合边界。
		if problem, ok := err.(*Error); ok {
			d := problem.Diagnostic
			if d.Phase != "" || d.CompletedEffects != nil || d.Recovery != nil {
				return cloneDiagnostic(d), true
			}
		}
		err = errors.Unwrap(err)
	}
	return Diagnostic{}, false
}

// WithDetails 快照边界归一化后的分支列表，原始原因仍由 Unwrap 保留。
// 分支错误须保持不可变；空列表不改变默认呈现，敏感原因不会通过分支展开。
func WithDetails(details []*Error) Option {
	snapshot := slices.Clone(details)
	return func(e *Error) { e.details = slices.Clone(snapshot) }
}

func cloneDiagnostic(d Diagnostic) Diagnostic {
	d.CompletedEffects = slices.Clone(d.CompletedEffects)
	if d.Recovery != nil {
		recovery := *d.Recovery
		recovery.Params = maps.Clone(recovery.Params)
		d.Recovery = &recovery
	}
	return d
}
