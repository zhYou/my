package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/tools/internal/scaffold"
	"github.com/zhYou/my/tools/internal/toolrun"
)

type dependencies struct {
	Environ     func() []string
	MakeTempDir func(string) (string, error)
	RemoveAll   func(string) error
	Run         func(context.Context, string, []string, string, ...string) error
}

type verificationStep struct {
	name string
	args []string
}

var errUsage = errors.New(usageMessage)

func run(ctx context.Context, args []string, root string, deps dependencies) error {
	if len(args) != 0 {
		return errUsage
	}
	return verifyProject(ctx, root, deps)
}

func defaultDependencies() dependencies {
	return dependencies{
		Environ: os.Environ,
		MakeTempDir: func(pattern string) (string, error) {
			return os.MkdirTemp("", pattern)
		},
		RemoveAll: removeIsolatedTree,
		Run:       toolrun.RunContext,
	}
}

func verifyProject(ctx context.Context, root string, deps dependencies) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("解析仓库路径：%w", err)
	}
	project, err := scaffold.Inspect(root)
	if err != nil {
		return err
	}
	tempRoot, err := deps.MakeTempDir(project.Name + "-verify-*")
	if err != nil {
		return fmt.Errorf("创建隔离验证目录：%w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, deps.RemoveAll(tempRoot))
	}()
	environment, err := isolatedEnvironment(root, tempRoot, paths.EnvPrefix(project.Name)+"_HOME", deps.Environ())
	if err != nil {
		return err
	}
	steps := []verificationStep{
		{name: checkTask, args: []string{runCommand, checkTask}},
		{name: "go test -race", args: []string{execCommand, "--", goProgram, testCommand, raceFlag, allPackages}},
		{name: mcpCheckTask, args: []string{runCommand, mcpCheckTask}},
	}
	for _, step := range steps {
		if err := deps.Run(ctx, root, environment, miseProgram, step.args...); err != nil {
			return fmt.Errorf("隔离验证 %s 失败：%w", step.name, err)
		}
	}
	return nil
}

func isolatedEnvironment(root, tempRoot, appHomeKey string, base []string) ([]string, error) {
	trusted, err := trustedConfigPaths(root, environmentValue(base, "MISE_TRUSTED_CONFIG_PATHS"))
	if err != nil {
		return nil, err
	}
	overrides := []environmentEntry{
		{key: appHomeKey, value: filepath.Join(tempRoot, appHomeDirName)},
		{key: envMiseState, value: filepath.Join(tempRoot, miseDirName, "state")},
		{key: envMiseCache, value: filepath.Join(tempRoot, miseDirName, cacheDirName)},
		{key: envGOCACHE, value: filepath.Join(tempRoot, goDirName, cacheDirName)},
		{key: envGolangci, value: filepath.Join(tempRoot, "golangci-lint")},
		{key: envXDGCache, value: filepath.Join(tempRoot, "xdg-cache")},
		{key: envUVCache, value: filepath.Join(tempRoot, uvDirName, cacheDirName)},
		{key: envUVTools, value: filepath.Join(tempRoot, uvDirName, "tools")},
		{key: envUVInstall, value: filepath.Join(tempRoot, uvDirName, "python")},
		{key: envNPMLower, value: filepath.Join(tempRoot, "npm")},
		{key: envGOTOOLCHAIN, value: "local"},
		{key: envMiseTrusted, value: strings.Join(trusted, string(os.PathListSeparator))},
	}
	entries := make([]string, 0, len(overrides))
	for _, entry := range overrides {
		entries = append(entries, entry.key+"="+entry.value)
	}
	return toolrun.WithEnvironment(base, entries), nil
}

type environmentEntry struct {
	key   string
	value string
}

func environmentValue(environment []string, wanted string) string {
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, wanted) {
			return value
		}
	}
	return ""
}

func trustedConfigPaths(root, existing string) ([]string, error) {
	var candidates []string
	candidates = append(candidates, filepath.SplitList(existing)...)
	for dir := filepath.Clean(root); ; dir = filepath.Dir(dir) {
		info, err := os.Stat(filepath.Join(dir, "mise.toml"))
		switch {
		case err == nil && info.Mode().IsRegular():
			candidates = append(candidates, dir)
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("检查 mise 配置 %s：%w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	seen := make(map[string]struct{}, len(candidates))
	trustedPaths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		key := candidate
		if filepath.Separator == '\\' {
			key = strings.ToLower(candidate)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		trustedPaths = append(trustedPaths, candidate)
	}
	return trustedPaths, nil
}

func removeIsolatedTree(root string) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() && info.Mode().Perm()&0o700 != 0o700 {
			//nolint:gosec // G122：路径来自本次唯一的临时根目录，且已跳过符号链接。
			return os.Chmod(path, info.Mode().Perm()|0o700)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(root)
}
