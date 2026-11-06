package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zhYou/my/tools/internal/fixture"
	"github.com/zhYou/my/tools/internal/toolrun"

	"github.com/stretchr/testify/require"
)

func TestReleaseVerificationRequiresInstalledArtifact(t *testing.T) {
	require.Error(t, verifyRelease(releaseFixture(t)), "仅归档结构通过不能替代真实安装验收")
}

func TestReleaseRejectsUnlistedInstallArchive(t *testing.T) {
	marker := installBoundaryObserver(t)
	dir := releaseFixture(t)
	releaseMetadataFixture(t, dir, "9.9.9")
	extension := ".tar.gz"
	if runtime.GOOS == windowsPlatform {
		extension = ".zip"
	}
	unlisted := appName + "_9.9.9_" + runtime.GOOS + "_" + runtime.GOARCH + extension
	require.NoError(t, os.WriteFile(filepath.Join(dir, unlisted), []byte("未列入最终校验清单的宿主归档"), 0o600))
	require.NoError(t, checkReleaseSnapshot(dir), "原有六目标清单依然有效")
	err := verifyRelease(dir)
	require.NoFileExists(t, marker, "未列入清单的归档不得到达任何 mise 安装调用")
	require.ErrorContains(t, err, "发布元数据版本与校验清单不一致")
}

func TestArchiveInstallRejectsChangedValidatedSnapshot(t *testing.T) {
	marker := installBoundaryObserver(t)
	for _, tc := range []struct {
		mode string
		want string
	}{
		{"missing", "宿主归档未列入已验证的最终校验清单"},
		{"changed-bytes", "宿主归档与已验证的最终清单摘要不一致"},
		{"changed-digest", "宿主归档与已验证的最终清单摘要不一致"},
		{"stale-head", "发布归档提交与当前 checkout 不一致"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := releaseFixture(t)
			releaseMetadataFixture(t, dir, "0.1.0")
			snapshot, err := validateReleaseSnapshot(dir)
			require.NoError(t, err)
			host := fixtureHostArchive()
			switch tc.mode {
			case "missing":
				delete(snapshot.checksums, host)
			case "changed-bytes":
				require.NoError(t, os.WriteFile(filepath.Join(dir, host), []byte("结构验证后被替换的归档"), 0o600))
			case "changed-digest":
				snapshot.checksums[host] = strings.Repeat("0", 64)
			case "stale-head":
				data, err := json.Marshal(
					map[string]string{"project_name": appName, "version": "0.1.0", "commit": strings.Repeat("0", 40)},
				)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o600))
			}
			err = verifyArchiveInstall(dir, snapshot)
			require.NoFileExists(t, marker, "版本、成员或摘要拒绝必须先于安装")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestReleaseRejectsMixedManifestVersionsBeforeInstall(t *testing.T) {
	marker := installBoundaryObserver(t)
	dir := releaseFixture(t)
	releaseMetadataFixture(t, dir, "0.1.0")
	host := fixtureHostArchive()
	changed := strings.Replace(host, "_0.1.0_", "_9.9.9_", 1)
	require.NoError(t, os.Rename(filepath.Join(dir, host), filepath.Join(dir, changed)))
	// 同步归档与 SPDX 的名称及摘要，保留六个目标，只制造版本混合。
	//nolint:gosec // G304：只读取当前测试生成的归档 SPDX fixture。
	sbom, err := os.ReadFile(filepath.Join(dir, host+".spdx.json"))
	require.NoError(t, err)
	sbom = []byte(strings.ReplaceAll(string(sbom), host, changed))
	require.NoError(t, os.Remove(filepath.Join(dir, host+".spdx.json")))
	//nolint:gosec // G703：changed 仅由固定测试身份与版本组成，目标位于当前临时目录。
	require.NoError(t, os.WriteFile(filepath.Join(dir, changed+".spdx.json"), sbom, 0o600))
	//nolint:gosec // G304：只读取当前测试生成的校验清单 fixture。
	sums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(sums)), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if fields[1] == host {
			lines[i] = fields[0] + "  " + changed
		}
		if fields[1] == host+".spdx.json" {
			digest := sha256.Sum256(sbom)
			lines[i] = hex.EncodeToString(digest[:]) + "  " + changed + ".spdx.json"
		}
	}
	//nolint:gosec // G703：仅更新当前临时 fixture 内的固定校验清单。
	require.NoError(t, os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	err = verifyRelease(dir)
	require.NoFileExists(t, marker, "混合版本必须先于安装被拒绝")
	require.ErrorContains(t, err, "发布归档版本不一致")
}

// installBoundaryObserver 只记录 mise 是否被调用，不模拟或证明实际安装。
func installBoundaryObserver(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(source, []byte(`package main
import ("os"; "fmt")
func main() { _ = os.WriteFile(os.Getenv("INSTALL_BOUNDARY_MARKER"), []byte("called"), 0600); fmt.Fprintln(os.Stderr, "checksum mismatch"); os.Exit(1) }
`), 0o600))
	binary := filepath.Join(dir, "mise")
	if runtime.GOOS == windowsPlatform {
		binary += executableSuffix
	}
	//nolint:gosec // G204：仅构建固定内容的调用边界观察器到临时目录。
	cmd := exec.Command("go", "build", "-o", binary, source)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	marker := filepath.Join(dir, "invoked")
	t.Setenv("INSTALL_BOUNDARY_MARKER", marker)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func releaseMetadataFixture(t *testing.T, dir, version string) {
	t.Helper()
	// 使用独立且有提交的临时 Git fixture，不依赖调用仓库已有 HEAD。
	// template:check 的生成仓库没有提交，不能借用它作为发布提交身份。
	require.NoError(t, fixture.PrepareGit(dir))
	require.NoError(
		t,
		toolrun.Run(
			dir,
			nil,
			gitProgram,
			"-c",
			"user.name=fixture",
			"-c",
			"user.email=fixture@example.com",
			"commit",
			"--quiet",
			"--allow-empty",
			"-m",
			"fixture",
		),
	)
	t.Chdir(dir)
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	data, err := json.Marshal(
		map[string]string{"project_name": appName, "version": version, "commit": strings.TrimSpace(string(head))},
	)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o600))
}

func fixtureHostArchive() string {
	extension := ".tar.gz"
	if runtime.GOOS == windowsPlatform {
		extension = ".zip"
	}
	return appName + "_0.1.0_" + runtime.GOOS + "_" + runtime.GOARCH + extension
}
