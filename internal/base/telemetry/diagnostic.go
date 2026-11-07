package telemetry

import (
	"github.com/zhYou/my/internal/base/fault"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RecordError 逐分支记录安全错误文本与稳定诊断字段，不收集原始参数或敏感原因。
func RecordError(span trace.Span, err error) {
	for _, detail := range fault.Details(err) {
		d, _ := fault.DiagnosticOf(detail)
		attributes := []attribute.KeyValue{
			attribute.String("error.code", string(detail.Code)),
			attribute.String("error.kind", string(detail.Kind)),
		}
		if d.Phase != "" {
			attributes = append(attributes, attribute.String("error.phase", d.Phase))
		}
		if d.CompletedEffects != nil {
			attributes = append(attributes, attribute.StringSlice("error.completed_effects", d.CompletedEffects))
		}
		if d.Recovery != nil {
			attributes = append(attributes, attribute.String("error.recovery", d.Recovery.Code))
		}
		span.RecordError(detail, trace.WithAttributes(attributes...))
	}
}
