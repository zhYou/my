package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func referenceProject(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"cmd/derived/main.go":          "package main\nfunc main() {}\n",
		moduleFile:                     "module example.com/derived\n\ngo 1.27.3\n",
		"internal/base/fault/error.go": "package fault\ntype Code string\ntype Definition struct { Code Code; Kind string }\ntype Recovery struct { Code string; Params map[string]any }\n",
		"internal/feature/feature.go":  "package feature\nimport f \"example.com/derived/internal/base/fault\"\n" + body + "\n",
	}
	for name, data := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	}
	return root
}
