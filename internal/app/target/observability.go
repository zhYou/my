package target

import (
	"context"
	"slices"
	"time"

	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type operation struct {
	service   *Service
	logger    *logging.Logger
	span      trace.Span
	phaseSpan trace.Span
	phase     string
	started   time.Time
	effects   []string
	commit    string
}

func (s *Service) begin(ctx context.Context, name string) (context.Context, *operation) {
	ctx, span := s.options.Tracer.Start(ctx, name)
	p := &operation{service: s, logger: s.options.Logger.WithContext(ctx), span: span, effects: []string{}}
	p.step(ctx, "target.validate")
	return ctx, p
}

func (p *operation) step(ctx context.Context, phase string) {
	p.endPhase(nil)
	p.phase, p.started = phase, time.Now()
	phaseContext, phaseSpan := p.service.options.Tracer.Start(ctx, phase)
	p.phaseSpan = phaseSpan
	p.logger = p.service.options.Logger.WithContext(phaseContext)
}

func (p *operation) confirmed(effect string) {
	if !slices.Contains(p.effects, effect) {
		p.effects = append(p.effects, effect)
	}
}

func (p *operation) finish(result *error) {
	if err := *result; err != nil {
		details := fault.Details(err)
		diagnostic, known := fault.DiagnosticOf(err)
		if !known {
			for _, detail := range details {
				child, exists := fault.DiagnosticOf(detail)
				if !exists {
					continue
				}
				if !known {
					diagnostic, known = child, true
					continue
				}
				if child.CompletedEffects == nil {
					diagnostic = child
				} else if diagnostic.CompletedEffects != nil {
					diagnostic.CompletedEffects = append(diagnostic.CompletedEffects, child.CompletedEffects...)
				}
			}
		}
		if !known {
			diagnostic.CompletedEffects = []string{}
		}
		if diagnostic.CompletedEffects != nil {
			diagnostic.CompletedEffects = append(slices.Clone(p.effects), diagnostic.CompletedEffects...)
		}
		if diagnostic.Phase == "" || len(p.effects) > 0 {
			diagnostic.Phase = p.phase
		}
		if len(p.effects) > 0 && diagnostic.CompletedEffects != nil || diagnostic.Recovery == nil {
			diagnostic.Recovery = &fault.Recovery{Code: "target.recover", Params: fault.Params{"Commit": p.commit}}
		}
		code := fault.Code("target.operation_failed")
		if diagnostic.CompletedEffects == nil {
			p.commit = ""
		}
		params := fault.Params{}
		if len(details) == 1 && details[0].Code != "" {
			code, params = details[0].Code, details[0].Params
		}
		*result = (fault.Definition{Code: code, Kind: fault.KindOf(err)}).Wrap(err,
			fault.WithParams(params), fault.WithSensitiveCause(), fault.WithDiagnostic(diagnostic))
	}
	p.endPhase(*result)
	if *result != nil {
		telemetry.RecordError(p.span, *result)
		p.span.SetStatus(codes.Error, "failed")
	}
	p.span.SetAttributes(attribute.String(effectDataCommit, p.commit))
	p.span.End()
}

func (p *operation) endPhase(err error) {
	if p.phaseSpan == nil {
		return
	}
	outcome, code := "success", ""
	if err != nil {
		outcome = "failed"
		if d, ok := fault.DiagnosticOf(err); ok {
			if d.CompletedEffects == nil {
				outcome = "unknown"
			} else if len(d.CompletedEffects) > 0 {
				outcome = "partial"
			}
		}
		if details := fault.Details(err); len(details) > 0 {
			code = string(details[0].Code)
		}
		telemetry.RecordError(p.phaseSpan, err)
		p.phaseSpan.SetStatus(codes.Error, code)
	}
	p.phaseSpan.SetAttributes(attribute.String("outcome", outcome))
	p.phaseSpan.End()
	message := p.phase
	if p.service.options.Localizer != nil {
		message = p.service.options.Localizer.Text("target.operation_event", map[string]any{"Operation": p.phase})
	}
	fields := []any{
		"event",
		"business_phase_finished",
		"phase",
		p.phase,
		"outcome",
		outcome,
		"data_commit",
		p.commit,
		"duration",
		time.Since(p.started),
		"error_code",
		code,
	}
	logger := p.logger
	if err != nil {
		logger.Errorw(message, fields...)
	} else {
		logger.Infow(message, fields...)
	}
	p.phaseSpan = nil
}
