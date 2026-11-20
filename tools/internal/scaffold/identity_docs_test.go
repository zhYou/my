package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootIdentityDocsAreManaged(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		root, err := filepath.Abs(filepath.Join("..", "..", ".."))
		require.NoError(t, err)
		unmanaged, err := unmanagedIdentityDocs(root)
		require.NoError(t, err)
		require.Empty(t, unmanaged)
	})

	t.Run("detects unmanaged document", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte(TemplateName), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "UNMANAGED.md"), []byte(TemplateName), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "OTHER.md"), []byte("其他内容"), 0o600))
		unmanaged, err := unmanagedIdentityDocs(root)
		require.NoError(t, err)
		require.Equal(t, []string{"UNMANAGED.md"}, unmanaged)
	})
}

func unmanagedIdentityDocs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取仓库根目录: %w", err)
	}
	var unmanaged []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		//nolint:gosec // G304：仅读取调用方指定仓库根目录中的 Markdown 文件。
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", entry.Name(), err)
		}
		if strings.Contains(string(data), TemplateName) && !slices.Contains(managedFiles, entry.Name()) {
			unmanaged = append(unmanaged, entry.Name())
		}
	}
	return unmanaged, nil
}
