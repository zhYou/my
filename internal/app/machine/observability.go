package machine

import (
	"context"
	"errors"
	"time"

	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/telemetry"
	"github.com/zhYou/my/internal/datarepo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func (s *Service) observe(
	ctx context.Context,
	operation string,
) (context.Context, func(error, bool, datarepo.Metadata)) {
	started := time.Now()
	ctx, span := s.options.Tracer.Start(ctx, operation)
	return ctx, func(err error, changed bool, metadata datarepo.Metadata) {
		outcome := "success"
		if !changed {
			outcome = "unchanged"
		}
		phase := operation
		code := ""
		if err != nil {
			outcome = outcomeFailed
			details := fault.Details(err)
			if len(details) > 0 {
				code = string(details[0].Code)
			}
			if d, ok := fault.DiagnosticOf(err); ok {
				phase = d.Phase
				if metadata.Commit == "" {
					for _, detail := range fault.Details(err) {
						if commit, ok := detail.Params["Commit"].(string); ok {
							metadata.Commit = commit
						}
					}
				}
				if d.Phase == effectCommit && len(d.CompletedEffects) == 0 {
					metadata.Commit = ""
				}
				if d.CompletedEffects == nil {
					outcome = "unknown"
				} else if len(d.CompletedEffects) > 0 {
					outcome = "partial"
				}
			}
			if errors.Is(err, context.Canceled) && outcome == outcomeFailed {
				outcome = "cancelled"
			}
		}
		s.endPhase(span, err)
		message := operation
		if s.options.Localizer != nil {
			message = s.options.Localizer.Text("machine.operation_event", map[string]any{"Operation": operation})
		}
		s.options.Logger.WithContext(ctx).
			Record(operationEvent{message: message, operation: operation, phase: phase, outcome: outcome, commit: metadata.Commit, duration: time.Since(started), failed: err != nil, code: code})
	}
}

func (s *Service) endPhase(span trace.Span, err error) {
	if err != nil {
		telemetry.RecordError(span, err)
		span.SetStatus(codes.Error, outcomeFailed)
	}
	span.SetAttributes(attribute.Bool(outcomeFailed, err != nil))
	span.End()
}

type operationEvent struct {
	message   string
	operation string
	phase     string
	outcome   string
	commit    string
	duration  time.Duration
	failed    bool
	code      string
}

func (e operationEvent) Emit(logger *logging.Logger) {
	fields := []any{
		"event",
		"business_operation_finished",
		"operation",
		e.operation,
		"phase",
		e.phase,
		"outcome",
		e.outcome,
		"data_commit",
		e.commit,
		"duration",
		e.duration,
		"error_code", e.code,
	}
	if e.failed {
		logger.Errorw(e.message, fields...)
		return
	}
	logger.Infow(e.message, fields...)
}
