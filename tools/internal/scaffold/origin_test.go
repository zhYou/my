package scaffold

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestOriginUsesCommandEntry(t *testing.T) {
	root := projectFixture(t, TemplateModule, TemplateName)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".template-version"), []byte("0.1.0\n"), 0o600))
	git := func(args ...string) string {
		t.Helper()
		//nolint:gosec // G204：测试只运行当前测试二进制或固定 Git/Go 命令，参数来自临时 fixture。
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return string(out)
	}
	git("-c", "init.templateDir=", "init", "--quiet")
	git("remote", "add", "origin", "https://"+TemplateModule+".git")
	git("add", ".")
	git("-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "--quiet", "-m", "fixture")
	origin, err := makeOrigin(root, "example.com/tool", "tool")
	require.NoError(t, err)
	require.NotEqual(t, "unknown", origin.SourceCommit)
	git("remote", "set-url", "origin", "https://example.com/copied.git")
	origin, err = makeOrigin(root, "example.com/tool", "tool")
	require.NoError(t, err)
	require.Equal(t, "unknown", origin.SourceCommit)
}

func TestOriginWithoutGitAndLegacyProject(t *testing.T) {
	root := fixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".template-version"), []byte("0.1.0\n"), 0o600))
	_, err := Initialize(root, "example.com/tool", "tool")
	require.NoError(t, err)
	_, err = makeOrigin(root, "example.com/tool", "tool")
	require.NoError(t, err)
	origin, err := ReadOrigin(afero.NewOsFs(), root)
	require.NoError(t, err)
	require.Equal(t, "unknown", origin.SourceCommit)
	require.Equal(t, TemplateModule, origin.Template)
	require.Equal(t, "0.1.0", origin.TemplateVersion)
	legacy := fixture(t)
	_, err = Initialize(legacy, "example.com/tool", "tool")
	require.NoError(t, err)
	_, err = ReadOrigin(afero.NewOsFs(), legacy)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = Initialize(legacy, "example.com/tool", "tool")
	require.NoError(t, err)
}

func TestOriginIsIdempotent(t *testing.T) {
	root := fixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".template-version"), []byte("0.1.0\n"), 0o600))
	_, err := Initialize(root, "example.com/tool", "tool")
	require.NoError(t, err)
	before := read(t, root, ".template-origin.json")
	changed, err := Initialize(root, "example.com/tool", "tool")
	require.NoError(t, err)
	require.Empty(t, changed)
	require.Equal(t, before, read(t, root, ".template-origin.json"))
}

func TestOriginFailureRollsBackRename(t *testing.T) {
	i, root := memoryInitializer(t)
	base := i.FS
	require.NoError(t, afero.WriteFile(base, filepath.Join(root, ".template-version"), []byte("0.1.0\n"), 0o600))
	before, err := afero.ReadFile(base, filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	i.FS = faultFS{
		Fs: base,
		rename: func(a, b string) error {
			if filepath.Base(b) == ".template-origin.json" {
				return errors.New("来源写入失败")
			}
			return base.Rename(a, b)
		},
	}
	_, err = i.Initialize(context.Background(), root, "example.com/tool", "tool")
	require.Error(t, err)
	after, err := afero.ReadFile(base, filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	ok, err := afero.Exists(base, filepath.Join(root, ".template-origin.json"))
	require.NoError(t, err)
	require.False(t, ok)
}
