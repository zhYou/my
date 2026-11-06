package acceptance

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/tools/internal/scaffold"
)

func TestExecutableAcceptanceVerifiesIdentityAndMetadata(t *testing.T) {
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	project, err := scaffold.Inspect(root)
	require.NoError(t, err)
	name := project.Name
	dir := t.TempDir()
	executable := filepath.Join(dir, name)
	if runtime.GOOS == windowsPlatform {
		executable += executableSuffix
	}
	//nolint:gosec // G204：仅构建当前仓库 CLI 到临时目录。
	build := exec.Command(
		"go",
		"build",
		"-ldflags",
		"-X main.version=0.2.0 -X main.commit=acceptance",
		"-o",
		executable,
		"./"+project.CommandPath,
	)
	build.Dir = root
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	example := filepath.Join(root, "configs/config.example.toml")
	require.NoError(t, Verify(executable, dir, name, example, "0.2.0", "acceptance"))
	require.Error(t, Verify(executable, dir, "wrong-name", example, "0.2.0", "acceptance"))
	require.Error(t, Verify(executable, dir, name, example, "wrong-version", "acceptance"))
	require.Error(t, Verify(executable, dir, name, example, "0.2.0", "wrong-commit"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("unknown = true"), 0o600))
	require.Error(t, Verify(executable, dir, name, filepath.Join(dir, "bad.toml"), "0.2.0", "acceptance"))
}
