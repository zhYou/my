package datarepo

import (
	"context"
	"time"

	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func (c *Client) observe(
	ctx context.Context,
	phase string,
	baseCommit string,
) (context.Context, func(error, Metadata)) {
	started := time.Now()
	ctx, span := c.options.Tracer.Start(ctx, phase)
	return ctx, func(err error, metadata Metadata) {
		outcome := "success"
		commit := metadata.Commit
		if phase == phaseCommit && err != nil {
			commit = ""
		}
		code := ""
		if err != nil {
			outcome = "failed"
			d, _ := fault.DiagnosticOf(err)
			if d.CompletedEffects == nil && phase == phaseCommit {
				outcome = "unknown"
			}
			details := fault.Details(err)
			if len(details) > 0 {
				code = string(details[0].Code)
			}
			telemetry.RecordError(span, err)
			span.SetStatus(codes.Error, code)
		}
		span.SetAttributes(
			attribute.String("outcome", outcome),
			attribute.String(phaseCommit, commit),
			attribute.String("data.base_commit", baseCommit),
		)
		span.End()
		message := phase
		if c.options.Localizer != nil {
			message = c.options.Localizer.Text("data.operation_event", map[string]any{"Operation": phase})
		}
		c.options.Logger.WithContext(ctx).
			Record(finishedEvent{message: message, phase: phase, outcome: outcome, commit: commit, baseCommit: baseCommit, code: code, duration: time.Since(started)})
	}
}

type finishedEvent struct {
	message    string
	phase      string
	outcome    string
	commit     string
	baseCommit string
	code       string
	duration   time.Duration
}

func (e finishedEvent) Emit(logger *logging.Logger) {
	fields := []any{
		"event",
		"data_phase_finished",
		"phase",
		e.phase,
		"outcome",
		e.outcome,
		"duration",
		e.duration,
		"data_commit",
		e.commit,
		"base_commit", e.baseCommit,
		"error_code",
		e.code,
	}
	if e.code != "" {
		logger.Errorw(e.message, fields...)
		return
	}
	logger.Infow(e.message, fields...)
}
