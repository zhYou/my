package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"update", "--against", "HEAD"}, {"check", "--bad", "HEAD"}} {
		require.ErrorContains(t, run(args), "用法")
	}
}
