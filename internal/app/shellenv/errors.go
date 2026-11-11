package shellenv

import (
	"github.com/zhYou/my/internal/base/fault"
)

const (
	codeInvalid   = "shellenv.invalid_input"
	codeState     = "shellenv.invalid_state"
	phaseValidate = "shellenv.validate"
	phaseRead     = "shellenv.read"
	phaseWrite    = "shellenv.write"
)

func invalidInput() error {
	return (fault.Definition{Code: codeInvalid, Kind: fault.Usage}).New(
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phaseValidate,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: "cli.recover_usage"},
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
				Recovery:         &fault.Recovery{Code: "shellenv.recover", Params: fault.Params{"Commit": ""}},
			},
		),
	}
	if cause == nil {
		return (fault.Definition{Code: code, Kind: fault.Runtime}).New(options...)
	}
	return (fault.Definition{Code: code, Kind: fault.Runtime}).Wrap(cause, options...)
}
