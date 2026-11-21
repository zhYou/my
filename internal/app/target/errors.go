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

func notManaged(target ID, phase string) error {
	params := fault.Params{"Target": string(target)}
	return (fault.Definition{Code: codeNotManaged, Kind: fault.Runtime}).New(
		fault.WithParams(params),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phase,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recoverNotManaged, Params: params},
			},
		),
	)
}

func outOfScope(target ID, machineID, phase string) error {
	params := fault.Params{"Target": string(target), "Machine": machineID}
	return (fault.Definition{Code: codeOutOfScope, Kind: fault.Runtime}).New(
		fault.WithParams(params),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phase,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recoverOutOfScope, Params: params},
			},
		),
	)
}

func unsupportedFile(phase string) error {
	return (fault.Definition{Code: codeFileUnsupported, Kind: fault.Runtime}).New(
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phase,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recoverFileUnsupported},
			},
		),
	)
}
