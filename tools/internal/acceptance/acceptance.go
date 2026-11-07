package acceptance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/tools/internal/contract"
)

// Env 隔离应用配置和状态，同时清除调用者的应用覆盖，避免验收读取真实用户目录。
func Env(name, home string) []string {
	prefix := paths.EnvPrefix(name) + "_"
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, prefix) {
			env = append(env, value)
		}
	}
	return append(env, prefix+"HOME="+home)
}

// Output 执行显式二进制并分别捕获 stdout 和 stderr。
func Output(dir string, env []string, executable string, args ...string) ([]byte, []byte, error) {
	//nolint:gosec // G204：仅执行验收方指定的绝对路径二进制，不经过 shell。
	cmd := exec.Command(executable, args...)
	cmd.Dir, cmd.Env = dir, env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	return stdout, stderr.Bytes(), err
}

// Verify 从实际二进制验收身份、双语契约、嵌入资源和配置优先级。
func Verify(executable, dir, name, example, version, commit string) (resultErr error) {
	home, err := os.MkdirTemp(dir, "acceptance-home-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(home))
	}()
	env := Env(name, home)
	run := func(extra []string, args ...string) ([]byte, []byte, error) {
		return Output(dir, append(append([]string{}, env...), extra...), executable, args...)
	}
	stdout, stderr, err := run(nil, versionCommand, jsonFlag)
	if err != nil {
		return fmt.Errorf("执行安装版本失败：%w：%s", err, stderr)
	}
	var actual struct {
		Name    string
		Version string
		Commit  string
		Date    string
	}
	if err := json.Unmarshal(stdout, &actual); err != nil {
		return err
	}
	if actual.Name != name || actual.Version != version || actual.Commit != commit || actual.Date == "" {
		return fmt.Errorf("安装元数据不匹配：实际 %+v，预期 %s %s %s", actual, name, version, commit)
	}
	var baseline []byte
	for _, language := range []string{"zh-CN", "en"} {
		stdout, stderr, err = run(nil, languageFlag, language, "--help")
		if err != nil || len(stderr) != 0 || !bytes.Contains(stdout, []byte(name)) ||
			!bytes.Contains(stdout, []byte(languageFlag)) {
			return errors.Join(fmt.Errorf("安装帮助验收失败（%s）：%s", language, stderr), err)
		}
		stdout, stderr, err = run(nil, languageFlag, language, schemaCommand, greetCommand)
		if err != nil || len(stderr) != 0 {
			return errors.Join(fmt.Errorf("安装 schema 验收失败：%s", stderr), err)
		}
		var schema struct {
			Version  int `json:"schema_version"`
			Commands []struct {
				Path string `json:"path"`
			} `json:"commands"`
		}
		if err := json.Unmarshal(stdout, &schema); err != nil {
			return err
		}
		if schema.Version != 1 || len(schema.Commands) != 1 || schema.Commands[0].Path != name+" greet" {
			return fmt.Errorf("安装 schema 版本或命令身份错误")
		}
		normalized, err := contract.Normalize(stdout)
		if err != nil {
			return err
		}
		if baseline != nil && !bytes.Equal(baseline, normalized) {
			return fmt.Errorf("双语 schema 机器字段不一致")
		}
		baseline = normalized
	}
	// 未配置数据仓时 doctor 完成负向报告；帮助与补全仍可发现。
	stdout, stderr, err = run(nil, doctorCommand, jsonFlag)
	var report struct {
		RequiredAvailable bool  `json:"required_available"`
		Checks            []any `json:"checks"`
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(stderr) != 0 {
		return errors.Join(fmt.Errorf("doctor 负向报告验收失败：%s", stderr), err)
	}
	if err := json.Unmarshal(stdout, &report); err != nil {
		return err
	}
	if report.RequiredAvailable || report.Checks == nil || len(report.Checks) == 0 {
		return fmt.Errorf("doctor 负向报告不符合契约：%s", stdout)
	}
	stdout, stderr, err = run(nil, "help", "completion", "bash")
	if err != nil || len(stderr) != 0 || !bytes.Contains(stdout, []byte(name+" completion bash")) {
		return errors.Join(fmt.Errorf("补全专属帮助验收失败：%s", stderr), err)
	}
	stdout, stderr, err = run(nil, "__complete", "--lang", "")
	if err != nil || len(stderr) != 0 || !bytes.Contains(stdout, []byte("en\n")) ||
		!bytes.Contains(stdout, []byte(":4\n")) {
		return errors.Join(fmt.Errorf("语言补全验收失败：%s", stderr), err)
	}
	// 发现命令不得创建诊断目录。
	entries, err := os.ReadDir(home)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("发现命令写入应用目录")
	}
	// 使用发布包或模板副本自带的 TOML，先核验它能直接加载。
	stdout, stderr, err = run(nil, "--config", example, greetCommand, nameFlag, acceptanceName, jsonFlag)
	if err := acceptGreeting(stdout, stderr, err, englishGreeting); err != nil {
		return err
	}
	// 默认配置路径同样通过应用 HOME 隔离；用 stderr 日志观察真实覆盖结果。
	config := filepath.Join(home, "config.toml")
	if err := os.WriteFile(config, []byte("[logging]\nlevel = \"debug\"\noutput = \"stderr\"\n"), 0o600); err != nil {
		return err
	}
	prefix := paths.EnvPrefix(name)
	stdout, stderr, err = run(nil, greetCommand, nameFlag, acceptanceName, jsonFlag)
	if err != nil || !bytes.Contains(stderr, []byte("greeting_finished")) {
		return errors.Join(fmt.Errorf("默认 TOML 配置未生效：%s", stderr), err)
	}
	if err := acceptGreeting(stdout, nil, nil, englishGreeting); err != nil {
		return err
	}
	override := []string{prefix + "_LOGGING_LEVEL=error", prefix + "_LANG=zh-CN"}
	stdout, stderr, err = run(override, greetCommand, nameFlag, acceptanceName, jsonFlag)
	if err := acceptGreeting(stdout, stderr, err, chineseGreeting); err != nil {
		return err
	}
	stdout, stderr, err = run(
		override,
		languageFlag,
		"en",
		"--log-level",
		"debug",
		greetCommand,
		nameFlag,
		acceptanceName,
		jsonFlag,
	)
	if err != nil || !bytes.Contains(stderr, []byte("greeting_finished")) {
		return errors.Join(fmt.Errorf("显式日志选项未覆盖环境变量：%s", stderr), err)
	}
	if err := acceptGreeting(stdout, nil, nil, englishGreeting); err != nil {
		return err
	}
	stdout, stderr, err = run([]string{prefix + "_LOG_LEVEL=invalid"}, greetCommand, nameFlag, acceptanceName, jsonFlag)
	if err == nil || len(stdout) != 0 || len(stderr) == 0 {
		return fmt.Errorf("无效环境配置未失败或污染 stdout")
	}
	fmt.Printf(
		"二进制验收通过：%s，name=%s version=%s commit=%s；双语帮助/schema、TOML、环境变量与显式覆盖。\n",
		executable,
		name,
		version,
		commit,
	)
	return nil
}

func acceptGreeting(stdout, stderr []byte, commandErr error, message string) error {
	if commandErr != nil || len(stderr) != 0 {
		return errors.Join(fmt.Errorf("配置与问候验收失败：%s", stderr), commandErr)
	}
	var result struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(stdout, &result); err != nil {
		return err
	}
	if result.Message != message {
		return fmt.Errorf("问候语言不匹配：%q", result.Message)
	}
	return nil
}
