package logging

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Logger 是项目日志入口，后端及编码细节不向其他包暴露。
type Logger struct {
	sugar       *zap.SugaredLogger
	contextBase *zap.SugaredLogger
}

// Event 描述由日志包统一编码的事件；其他包可实现 Emit 扩展事件。
type Event interface {
	Emit(*Logger)
}

// NewNop 提供不写入文件的空日志，适用于显式禁用和测试注入。
func NewNop() *Logger {
	return &Logger{sugar: zap.NewNop().Sugar()}
}

// With 创建共享输出且字段独立的子日志，不修改父日志。
func (l *Logger) With(fields ...any) *Logger {
	child := &Logger{sugar: l.sugar.With(fields...)}
	if l.contextBase != nil {
		child.contextBase = l.contextBase.With(fields...)
	}
	return child
}

// Infow 以 Sugar 风格记录临时诊断。
func (l *Logger) Infow(message string, fields ...any) {
	l.sugar.Infow(message, fields...)
}

// Errorw 记录错误级别的临时诊断。
func (l *Logger) Errorw(message string, fields ...any) {
	l.sugar.Errorw(message, fields...)
}

// Record 使用日志实例记录事件；其他包可实现 Emit 扩展事件，无需逐次向事件传入实例。
func (l *Logger) Record(event Event) {
	if event != nil {
		event.Emit(l)
	}
}

// Debugw 记录调试级别的临时诊断。
func (l *Logger) Debugw(message string, fields ...any) {
	l.sugar.Debugw(message, fields...)
}

// Warnw 记录警告级别的临时诊断。
func (l *Logger) Warnw(message string, fields ...any) {
	l.sugar.Warnw(message, fields...)
}

// WithContext 只绑定已有 span 的关联字段，不通过 Context 查找 logger。
func (l *Logger) WithContext(ctx context.Context) *Logger {
	base := l.sugar
	if l.contextBase != nil {
		base = l.contextBase
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return &Logger{sugar: base}
	}
	return &Logger{
		sugar:       base.With("trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String()),
		contextBase: base,
	}
}
