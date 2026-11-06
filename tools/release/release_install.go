package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zhYou/my/tools/internal/acceptance"

	"github.com/zhYou/my/internal/base/paths"
)

// verifyRelease 在六目标归档检查后，以宿主真实安装完成发布验收。
func verifyRelease(dist string) error {
	snapshot, err := validateReleaseSnapshot(dist)
	if err != nil {
		return err
	}
	return verifyArchiveInstall(dist, snapshot)
}

func verifyArchiveInstall(dist string, snapshot *releaseSnapshot) (resultErr error) {
	metadataPath, err := releaseFile(dist, "metadata.json")
	if err != nil {
		return err
	}
	//nolint:gosec // G304：路径已由 releaseFile 限制为本地发布目录中的常规文件。
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return err
	}
	var metadata struct {
		Name    string `json:"project_name"`
		Version string
		Commit  string
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return err
	}
	if metadata.Name != appName || metadata.Version == "" || metadata.Commit == "" {
		return fmt.Errorf("发布元数据缺少身份、版本或提交")
	}
	if metadata.Version != snapshot.version {
		return fmt.Errorf("发布元数据版本与校验清单不一致：%s != %s", metadata.Version, snapshot.version)
	}
	head, err := exec.Command(gitProgram, gitRevParse, "HEAD").Output()
	if err != nil {
		return err
	}
	if metadata.Commit != strings.TrimSpace(string(head)) {
		return fmt.Errorf("发布归档提交与当前 checkout 不一致，请重新生成 snapshot")
	}
	extension := ".tar.gz"
	binary := appName
	if runtime.GOOS == windowsPlatform {
		extension = ".zip"
		binary += executableSuffix
	}
	archiveName := fmt.Sprintf("%s_%s_%s_%s%s", appName, metadata.Version, runtime.GOOS, runtime.GOARCH, extension)
	checksum, listed := snapshot.checksums[archiveName]
	if !listed {
		return fmt.Errorf("宿主归档未列入已验证的最终校验清单：%s", archiveName)
	}
	archive, err := releaseFile(dist, archiveName)
	if err != nil {
		return err
	}
	archive, err = filepath.Abs(archive)
	if err != nil {
		return err
	}
	//nolint:gosec // G304：只重新核对清单内已验证归档，安装仍使用清单摘要而非本次读取的摘要。
	data, err = os.ReadFile(archive)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != checksum {
		return fmt.Errorf("宿主归档与已验证的最终清单摘要不一致：%s", archiveName)
	}
	mise, err := exec.LookPath("mise")
	if err != nil {
		return err
	}
	mise, err = filepath.Abs(mise)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "cli-archive-install-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(root))
	}()
	archiveURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(archive)}).String()
	// 错误摘要必须先在全新 data/cache 中失败；正确安装使用另一套目录，避免缓存掩盖校验。
	for _, valid := range []bool{false, true} {
		phase, expected := "wrong-sha", strings.Repeat("0", 64)
		if valid {
			phase, expected = "verified", checksum
		}
		dir := filepath.Join(root, phase)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		config := fmt.Sprintf(
			"[settings]\nauto_install = false\nexec_auto_install = false\n[tools.\"http:%s\"]\nversion = %q\nurl = %q\nchecksum = %q\n",
			appName,
			metadata.Version,
			archiveURL,
			"sha256:"+expected,
		)
		if err := os.WriteFile(filepath.Join(dir, "mise.toml"), []byte(config), 0o600); err != nil {
			return err
		}
		env := isolatedMiseEnv(dir)
		stdout, stderr, installErr := acceptance.Output(dir, env, mise, "install", "http:"+appName+"@"+metadata.Version)
		if !valid {
			reason := strings.ToLower(string(append(stdout, stderr...)))
			if installErr == nil || !strings.Contains(reason, "checksum") || !strings.Contains(reason, "mismatch") {
				return errors.Join(fmt.Errorf("错误 SHA 未被明确拒绝：%s", reason), installErr)
			}
			fmt.Printf("归档错误 SHA 已拒绝：%s；隔离目录 %s\n", archiveName, dir)
			continue
		}
		if installErr != nil {
			return fmt.Errorf("mise 归档安装失败：%w：%s%s", installErr, stdout, stderr)
		}
		stdout, stderr, err = acceptance.Output(dir, env, mise, "which", binary)
		if err != nil {
			return fmt.Errorf("解析 mise 安装路径失败：%w：%s", err, stderr)
		}
		executable := strings.TrimSpace(string(stdout))
		if !filepath.IsAbs(executable) {
			return fmt.Errorf("安装路径不是绝对路径：%s", executable)
		}
		resolved, err := filepath.EvalSymlinks(executable)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(filepath.Join(dir, "data"), resolved)
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("安装二进制不在隔离 data 目录：%s", resolved)
		}
		example := filepath.Join(filepath.Dir(resolved), "config.example.toml")
		if err := acceptance.Verify(resolved, dir, appName, example, metadata.Version, metadata.Commit); err != nil {
			return err
		}
		fmt.Printf("归档安装验收通过：archive=%s sha256=%s executable=%s\n", archiveName, checksum, resolved)
	}
	return nil
}

// isolatedMiseEnv 不继承调用者的 mise 设置、自动激活状态或应用覆盖；所有安装状态只写临时目录。
func isolatedMiseEnv(dir string) []string {
	env := []string{}
	for _, value := range acceptance.Env(appName, filepath.Join(dir, "app-home")) {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "MISE_") || strings.HasPrefix(key, "__MISE_") || key == "PWD" {
			continue
		}
		env = append(env, value)
	}
	for key, suffix := range map[string]string{
		"MISE_CONFIG_DIR":         "config",
		"MISE_DATA_DIR":           "data",
		"MISE_CACHE_DIR":          "cache",
		"MISE_STATE_DIR":          "state",
		"MISE_TMP_DIR":            "tmp",
		"MISE_SYSTEM_CONFIG_DIR":  "system-config",
		"MISE_SYSTEM_DATA_DIR":    "system-data",
		"MISE_GLOBAL_CONFIG_FILE": "empty-global.toml",
		"MISE_SYSTEM_CONFIG_FILE": "empty-system.toml",
	} {
		env = append(env, key+"="+filepath.Join(dir, suffix))
	}
	return append(
		env,
		"PWD="+dir,
		"MISE_CEILING_PATHS="+filepath.Dir(dir),
		"MISE_TRUSTED_CONFIG_PATHS="+dir,
		"MISE_YES=1",
		"MISE_COLOR=0",
		"MISE_AUTO_INSTALL=0",
		"MISE_EXEC_AUTO_INSTALL=0",
		"MISE_NOT_FOUND_AUTO_INSTALL=0",
		"MISE_SELF_UPDATE_AVAILABLE=0",
		paths.EnvPrefix(appName)+"_HOME="+filepath.Join(dir, "app-home"),
	)
}
