package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseArchiveContents(t *testing.T) {
	for _, target := range []string{"linux_amd64", "windows_amd64"} {
		for _, mode := range []string{"valid", "binary", "readme", "config", "empty", "parent", "duplicate", "corrupt", "symlink", "dot-alias", "parent-alias", "windows-alias"} {
			t.Run(target+"/"+mode, func(t *testing.T) {
				dir := releaseFixtureMode(t, target, mode)
				err := checkReleaseSnapshot(dir)
				if mode == "valid" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}
