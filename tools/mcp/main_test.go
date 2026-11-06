package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"extra"}} {
		require.ErrorContains(t, run(args), "mise run mcp:check")
	}
}
