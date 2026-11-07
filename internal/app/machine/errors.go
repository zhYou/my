package machine

import (
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/datarepo"
)

func invalidInput() error {
	return (fault.Definition{Code: "machine.invalid_input", Kind: fault.Usage}).New(
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phaseValidate,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: "cli.recover_usage"},
			},
		),
	)
}

func problem(code, phase string, cause error, effects []string, recovery string) error {
	definition := fault.Definition{Code: fault.Code(code), Kind: fault.Runtime}
	options := []fault.Option{
		fault.WithSensitiveCause(),
		fault.WithDiagnostic(
			fault.Diagnostic{Phase: phase, CompletedEffects: effects, Recovery: &fault.Recovery{Code: recovery}},
		),
	}
	if cause == nil {
		return definition.New(options...)
	}
	return definition.Wrap(cause, options...)
}

func bindingFailure(phase string, cause error, effects []string, metadata datarepo.Metadata) error {
	return (fault.Definition{Code: "machine.binding_failed", Kind: fault.Runtime}).Wrap(
		cause,
		fault.WithSensitiveCause(),
		fault.WithParams(fault.Params{"Commit": metadata.Commit}),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phase,
				CompletedEffects: effects,
				Recovery:         &fault.Recovery{Code: recoverLocal},
			},
		),
	)
}
