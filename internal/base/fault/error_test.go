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
