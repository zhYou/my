package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"check", "extra"}} {
		var out, errOut bytes.Buffer
		require.Equal(t, 2, run(args, t.TempDir(), &out, &errOut))
		require.Empty(t, out.String())
		require.Contains(t, errOut.String(), "用法")
	}
}
