package target

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnifiedTextDiffMarksBothSides(t *testing.T) {
	diff := UnifiedTextDiff("go.rules", []byte("old\n"), []byte("new\n"))
	require.Contains(t, diff, "--- go.rules")
	require.Contains(t, diff, "+++ go.rules")
	require.Contains(t, diff, "-old")
	require.Contains(t, diff, "+new")
}
