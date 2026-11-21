package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// isolateMCPLogs 把测试创建的临时日志目录归属 t.TempDir；
// 即使断言提前失败或调用 Fatal，testing 仍会清理该目录。
func isolateMCPLogs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	return root
}

// requireMCPLogsOwned 断言保留的日志目录位于测试自有临时目录内，无需测试手写清理。
func requireMCPLogsOwned(t *testing.T, root string, checkErr *mcpCheckError) {
	t.Helper()
	require.Equal(t, root, filepath.Dir(checkErr.logDir), "失败日志目录必须在测试自有临时目录内")
}

// requireNoMCPLogs 断言测试自有临时目录内没有遗留的验收日志目录。
func requireNoMCPLogs(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, entry := range entries {
		matched, err := filepath.Match(mcpLogDirectoryPattern, entry.Name())
		require.NoError(t, err)
		require.False(t, matched, "成功验收不应遗留日志目录 %s", entry.Name())
	}
}
