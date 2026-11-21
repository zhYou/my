package target

import (
	"strings"

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
	params := fault.Params{paramTarget: string(target)}
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
	params := fault.Params{paramTarget: string(target), "Machine": machineID}
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

func noConflicts(target ID, phase string) error {
	params := fault.Params{paramTarget: string(target)}
	return (fault.Definition{Code: codeNoConflicts, Kind: fault.Runtime}).New(
		fault.WithParams(params),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phase,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recoverNoConflicts, Params: params},
			},
		),
	)
}

func partialResolve(
	target ID,
	from ResolveSource,
	resolved int,
	remaining []Conflict,
	cause error,
) error {
	units := make([]string, 0, len(remaining))
	for _, conflict := range remaining {
		units = append(units, string(conflict.Unit))
	}
	params := fault.Params{
		paramTarget:      string(target),
		"From":           string(from),
		"Resolved":       resolved,
		"Remaining":      len(remaining),
		"RemainingUnits": strings.Join(units, ", "),
	}
	return (fault.Definition{Code: codeResolvePartial, Kind: fault.Runtime}).Wrap(
		cause,
		fault.WithSensitiveCause(),
		fault.WithParams(params),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phaseStateWrite,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recoverResolvePartial, Params: params},
			},
		),
	)
}
