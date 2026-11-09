package shellenv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetRejectsDangerousVariableNames(t *testing.T) {
	service, repository := newEnvironmentService(t, "workstation")
	for _, name := range []string{"PATH", "BASH_ENV", "LD_PRELOAD", "NODE_OPTIONS", "PYTHONSTARTUP"} {
		_, err := service.Set(t.Context(), SetOptions{Name: name, Value: []byte("fixture")})
		require.Error(t, err)
	}
	require.Zero(t, repository.commits)
}

func TestSetRejectsInvalidValues(t *testing.T) {
	service, _ := newEnvironmentService(t, "workstation")
	for _, value := range [][]byte{nil, []byte("line\nfeed"), {0}, make([]byte, maxValueSize+1)} {
		_, err := service.Set(t.Context(), SetOptions{Name: "EDITOR", Value: value})
		require.Error(t, err)
	}
}
