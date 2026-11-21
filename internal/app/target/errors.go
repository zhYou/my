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

// targetProblem 构造带稳定领域码、参数和恢复建议的目标错误。
func targetProblem(code, recovery, phase string, params fault.Params) error {
	return (fault.Definition{Code: fault.Code(code), Kind: fault.Runtime}).New(
		fault.WithParams(params),
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            phase,
				CompletedEffects: []string{},
				Recovery:         &fault.Recovery{Code: recovery, Params: params},
			},
		),
	)
}

func notManaged(target ID, phase string) error {
	return targetProblem(
		codeNotManaged,
		recoverNotManaged,
		phase,
		fault.Params{paramTarget: string(target)},
	)
}

func outOfScope(target ID, machineID, phase string) error {
	return targetProblem(
		codeOutOfScope,
		recoverOutOfScope,
		phase,
		fault.Params{paramTarget: string(target), paramMachine: machineID},
	)
}

func remoteAuthoritative(target ID, phase string) error {
	return targetProblem(
		codeRemoteAuthoritative,
		recoverRemoteAuthoritative,
		phase,
		fault.Params{paramTarget: string(target)},
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
	return targetProblem(
		codeNoConflicts,
		recoverNoConflicts,
		phase,
		fault.Params{paramTarget: string(target)},
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
