package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/zhYou/my/internal/app/cli"
	"github.com/zhYou/my/internal/app/doctor"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/tools/internal/contract"
)

const contractFile = "test/testdata/contracts/v1.json"

func captureContract(ctx context.Context) (_ []byte, resultErr error) {
	dir, err := os.MkdirTemp("", "cli-contract-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(dir))
	}()
	shapes := map[string]any{}
	results := map[string]any{"results": shapes}
	for key, args := range map[string][]string{
		schemaCommand:  {schemaCommand},
		greetCommand:   {greetCommand, nameFlag, "Codex", jsonFlag},
		versionCommand: {versionCommand, jsonFlag},
		doctorCommand:  {doctorCommand, jsonFlag},
	} {
		var out, errOut bytes.Buffer
		code := cli.Run(ctx, args, cli.Options{
			Name: "contract-cli",
			Dependencies: &cli.Dependencies{Doctor: func(context.Context, doctor.Options) (doctor.Result, error) {
				return doctor.Result{RequiredAvailable: true, Checks: []doctor.Check{}}, nil
			}},
			Version:     "1.0.0",
			Commit:      "abc",
			Date:        "2000-01-01",
			Out:         &out,
			ErrOut:      &errOut,
			ConfigDir:   dir,
			Directories: &paths.Directories{State: filepath.Join(dir, "state")},
			LookupEnv: func(string) (string, bool) {
				return "", false
			},
		})
		if code != 0 {
			return nil, fmt.Errorf("捕获 %s 失败：%s", key, &errOut)
		}
		var value any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			return nil, err
		}
		if key == schemaCommand {
			results[key] = value
		} else {
			shapes[key] = shape(value)
		}
	}
	diagnostics, err := captureDiagnostics(ctx, dir)
	if err != nil {
		return nil, err
	}
	shapes["diagnostics"] = diagnostics
	data, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	return contract.Normalize(data)
}

func shape(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			out[k] = shape(item)
		}
		return out
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return "null"
	}
}

func validateContract(base, snapshot, actual []byte, records []contract.Record, initial bool) error {
	changes, err := contract.Diff(snapshot, actual)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		return fmt.Errorf("当前快照与程序不符，请审查后运行 mise run contract:update：%v", changes)
	}
	changes, err = contract.Diff(base, actual)
	if err != nil {
		return err
	}
	return contract.ValidateRecords(changes, records, initial)
}

func readRecords(against string) ([]contract.Record, error) {
	recordPaths, err := filepath.Glob("docs/compatibility/changes/*.json")
	if err != nil {
		return nil, err
	}
	records := []contract.Record{}
	for _, path := range recordPaths {
		//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if against != "" {
			//nolint:gosec // G204：开发工具读取显式项目文件或执行固定 Git 子命令，不经过 shell，不是外部服务入口。
			old, err := exec.Command(gitProgram, "show", against+":"+filepath.ToSlash(path)).Output()
			if err == nil && bytes.Equal(old, data) {
				continue
			}
		}
		var record contract.Record
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("读取 %s 失败：%w", path, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func checkContract(ctx context.Context, against string) error {
	actual, err := captureContract(ctx)
	if err != nil {
		return err
	}
	snapshot, err := os.ReadFile(contractFile)
	if err != nil {
		return err
	}
	base := snapshot
	initial := false
	if against != "" {
		base, initial, err = readContractBase(ctx, against)
		if err != nil {
			return err
		}
	}

	records, err := readRecords(against)
	if err != nil {
		return err
	}
	return validateContract(base, snapshot, actual, records, initial)
}

func updateContract(ctx context.Context) error {
	data, err := captureContract(ctx)
	if err != nil {
		return err
	}
	//nolint:gosec // G301：这是公开构建产物或机器契约文件的既定权限，不包含凭据。
	if err := os.MkdirAll(filepath.Dir(contractFile), 0o755); err != nil {
		return err
	}
	//nolint:gosec // G306：这是公开构建产物或机器契约文件的既定权限，不包含凭据。
	return os.WriteFile(contractFile, append(data, '\n'), 0o644)
}

func captureDiagnostics(ctx context.Context, dir string) (map[string]any, error) {
	var out, stderr bytes.Buffer
	blocked := filepath.Join(dir, "blocked-state")
	opts := cli.Options{
		Name:        "contract-cli",
		ConfigDir:   dir,
		Directories: &paths.Directories{State: blocked},
		Out:         &out,
		ErrOut:      &stderr,
		LookupEnv:   func(string) (string, bool) { return "", false },
	}
	code := cli.Run(
		ctx,
		[]string{greetCommand, nameFlag, "fixture", jsonFlag, "--config", filepath.Join(dir, "missing.toml")},
		opts,
	)
	if code != 1 {
		return nil, fmt.Errorf("错误诊断捕获退出码=%d", code)
	}
	var failure any
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil {
		return nil, err
	}
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		return nil, err
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(ctx, []string{greetCommand, nameFlag, "fixture", jsonFlag}, opts)
	if code != 0 {
		return nil, fmt.Errorf("告警诊断捕获退出码=%d", code)
	}
	var warning any
	if err := json.NewDecoder(&stderr).Decode(&warning); err != nil {
		return nil, err
	}
	return map[string]any{"error": shape(failure), "warning": shape(warning)}, nil
}
