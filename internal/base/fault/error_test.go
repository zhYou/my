package fault_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
)

func TestKindsAndCause(t *testing.T) {
	cause := errors.New("磁盘不可用")
	err := fault.Wrap(fault.Configuration, "读取配置失败", cause)
	require.ErrorIs(t, err, cause)
	var typed *fault.Error
	require.ErrorAs(t, err, &typed)
	require.Equal(t, fault.Configuration, fault.KindOf(fmt.Errorf("外层：%w", err)))
	require.Contains(t, err.Error(), "读取配置失败")
	require.Contains(t, err.Error(), "磁盘不可用")
	require.Equal(t, fault.Runtime, fault.KindOf(cause))
	require.Equal(t, fault.Usage, fault.KindOf(fault.New(fault.Usage, "参数无效")))
}

func TestDefinitionSnapshotsAndJoinedKinds(t *testing.T) {
	d := fault.Definition{Code: "sample.invalid", Kind: fault.Usage}
	params := fault.Params{"Field": "x"}
	option := fault.WithParams(params)
	params["Field"] = "changed"
	a, b := d.New(option), d.New(option)
	a.Params["Field"] = "a"
	require.Equal(t, "x", b.Params["Field"])
	cause := errors.New("cause")
	wrapped := d.Wrap(cause, option)
	require.ErrorIs(t, wrapped, cause)
	require.Equal(t, fault.Code("sample.invalid"), fault.Details(wrapped)[0].Code)
	require.Len(t, fault.Details(errors.Join(a, b)), 2)
	require.Equal(t, fault.Runtime, fault.KindOf(errors.Join(a, cause)))
}

func TestDiagnosticSnapshotsAndSensitiveCause(t *testing.T) {
	cause := errors.New("private-fixture-cause")
	completed := []string{"fixture.saved"}
	params := fault.Params{"Path": "safe-file"}
	option := fault.WithDiagnostic(
		fault.Diagnostic{
			Phase:            "fixture.publish",
			CompletedEffects: completed,
			Recovery:         &fault.Recovery{Code: "fixture.recover", Params: params},
		},
	)
	completed[0] = "changed"
	params["Path"] = "changed"
	first := (fault.Definition{Code: "fixture.failed", Kind: fault.Runtime}).Wrap(
		cause,
		option,
		fault.WithSensitiveCause(),
	)
	require.ErrorIs(t, first, cause)
	require.NotContains(t, first.Error(), "private-fixture")
	outer := (fault.Definition{Code: "outer.failed", Kind: fault.Runtime}).Wrap(first)
	got, ok := fault.DiagnosticOf(outer)
	require.True(t, ok)
	require.Equal(t, "fixture.publish", got.Phase)
	require.Equal(t, []string{"fixture.saved"}, got.CompletedEffects)
	require.Equal(t, "safe-file", got.Recovery.Params["Path"])
	got.CompletedEffects[0] = "changed"
	got.Recovery.Params["Path"] = "changed"
	again, _ := fault.DiagnosticOf(first)
	require.Equal(t, []string{"fixture.saved"}, again.CompletedEffects)
	require.Equal(t, "safe-file", again.Recovery.Params["Path"])
	for _, effects := range [][]string{nil, {}} {
		problem := (fault.Definition{Code: "fixture", Kind: fault.Runtime}).New(
			fault.WithDiagnostic(fault.Diagnostic{Phase: "fixture", CompletedEffects: effects}),
		)
		diagnostic, ok := fault.DiagnosticOf(problem)
		require.True(t, ok)
		require.Equal(t, effects == nil, diagnostic.CompletedEffects == nil)
	}
}

func TestWrappedJoinPreservesEachDiagnosticBranch(t *testing.T) {
	first := (fault.Definition{Code: "fixture.first", Kind: fault.Runtime}).New(
		fault.WithDiagnostic(fault.Diagnostic{Phase: "fixture.first", CompletedEffects: []string{}}),
	)
	second := (fault.Definition{Code: "fixture.second", Kind: fault.Runtime}).New(
		fault.WithDiagnostic(fault.Diagnostic{Phase: "fixture.second"}),
	)
	outer := (fault.Definition{Code: "fixture.outer", Kind: fault.Runtime}).Wrap(errors.Join(first, second))
	branches := fault.Details(outer)
	require.Len(t, branches, 2)
	for i, phase := range []string{"fixture.first", "fixture.second"} {
		d, ok := fault.DiagnosticOf(branches[i])
		require.True(t, ok)
		require.Equal(t, phase, d.Phase)
		require.Equal(t, fault.Code("fixture.outer"), branches[i].Code)
	}
	hidden := (fault.Definition{Code: "fixture.hidden", Kind: fault.Runtime}).Wrap(
		errors.Join(first, second),
		fault.WithSensitiveCause(),
	)
	require.Len(t, fault.Details(hidden), 1)
	require.NotContains(t, hidden.Error(), "fixture.first")
}
