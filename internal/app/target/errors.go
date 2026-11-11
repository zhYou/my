package target

import (
	"github.com/zhYou/my/internal/base/fault"
)

func invalidInput() error {
	return (fault.Definition{Code: codeInvalidInput, Kind: fault.Usage}).New(
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phaseValidate,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recoverUsage},
			},
		),
	)
}

func problem(code fault.Code, phase string, cause error) error {
	options := []fault.Option{
		fault.WithSensitiveCause(),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phase,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recoverConfiguration, Params: fault.Params{"Commit": ""}},
			},
		),
	}
	if cause == nil {
		return (fault.Definition{Code: code, Kind: fault.Runtime}).New(options...)
	}
	return (fault.Definition{Code: code, Kind: fault.Runtime}).Wrap(cause, options...)
}
