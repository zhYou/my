package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseSnapshotRejectsMissingOrMismatchedSBOM(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "hash", "duplicate", "escape", "wrong-document", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			dir := releaseFixture(t)
			sumPath := filepath.Join(dir, "checksums.txt")
			//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
			data, err := os.ReadFile(sumPath)
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			var sbom string
			for _, line := range lines {
				if strings.HasSuffix(line, ".spdx.json") {
					sbom = strings.SplitN(line, "  ", 2)[1]
					break
				}
			}
			switch mode {
			case "missing":
				//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
				require.NoError(t, os.Remove(filepath.Join(dir, sbom)))
			case "hash":
				//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
				require.NoError(t, os.WriteFile(filepath.Join(dir, sbom), []byte("{}"), 0o600))
			case "duplicate":
				data = append(data, []byte(lines[0]+"\n")...)
			case "escape":
				data = append(data, []byte(strings.Repeat("0", 64)+"  ../outside\n")...)
			case "wrong-document":
				raw, err := json.Marshal(map[string]any{"spdxVersion": "SPDX-2.3", "name": "wrong"})
				require.NoError(t, err)
				//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
				require.NoError(t, os.WriteFile(filepath.Join(dir, sbom), raw, 0o600))
				sum := sha256.Sum256(raw)
				for i, line := range lines {
					if strings.HasSuffix(line, "  "+sbom) {
						lines[i] = hex.EncodeToString(sum[:]) + "  " + sbom
					}
				}
				data = []byte(strings.Join(lines, "\n") + "\n")
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside")
				//nolint:gosec // G304：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
				raw, err := os.ReadFile(filepath.Join(dir, sbom))
				require.NoError(t, err)
				//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
				require.NoError(t, os.WriteFile(outside, raw, 0o600))
				//nolint:gosec // G703：测试路径和权限只来自当前临时 fixture，不读取用户文件。
				require.NoError(t, os.Remove(filepath.Join(dir, sbom)))
				if err := os.Symlink(outside, filepath.Join(dir, sbom)); err != nil {
					t.Skipf("符号链接不可用：%v", err)
				}
			}
			//nolint:gosec // G703：路径只来自当前测试的临时目录或测试子进程的显式协议参数。
			require.NoError(t, os.WriteFile(sumPath, data, 0o600))
			err = checkReleaseSnapshot(dir)
			if mode == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
