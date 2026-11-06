package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const windowsOS = "windows"

func envValue(env []string, key string) string {
	for _, e := range slices.Backward(env) {
		name, value, ok := strings.Cut(e, "=")
		if ok && (name == key || runtime.GOOS == windowsOS && strings.EqualFold(name, key)) {
			return value
		}
	}
	return ""
}

func resolve(program, dir string, env []string) (string, error) {
	if filepath.IsAbs(program) || strings.ContainsRune(program, os.PathSeparator) ||
		runtime.GOOS == windowsOS && strings.ContainsAny(program, `/\:`) {
		path := program
		if !filepath.IsAbs(path) {
			var err error
			path, err = filepath.Abs(filepath.Join(dir, path))
			if err != nil {
				return "", err
			}
		}
		if runtime.GOOS == windowsOS {
			return resolveWindowsExecutable(path, env)
		}
		return path, nil
	}
	for _, entry := range filepath.SplitList(envValue(env, "PATH")) {
		if runtime.GOOS == windowsOS && entry == "" {
			continue
		}
		candidate, err := filepath.Abs(filepath.Join(entry, program))
		if err != nil {
			continue
		}
		if runtime.GOOS == windowsOS {
			candidate, err = resolveWindowsExecutable(candidate, env)
		} else {
			_, err = exec.LookPath(candidate)
		}
		if err != nil {
			continue
		}
		if !filepath.IsAbs(entry) {
			return "", &exec.Error{Name: program, Err: exec.ErrDot}
		}
		return candidate, nil
	}
	return "", &exec.Error{Name: program, Err: exec.ErrNotFound}
}

// 按调用环境展开显式路径及 PATH 候选，避免标准库读取父进程 PATHEXT。
func resolveWindowsExecutable(program string, env []string) (string, error) {
	exists := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	}
	if filepath.Ext(program) != "" && exists(program) {
		return program, nil
	}
	raw := envValue(env, "PATHEXT")
	if raw == "" {
		raw = ".com;.exe;.bat;.cmd"
	}
	for extension := range strings.SplitSeq(strings.ToLower(raw), ";") {
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		if candidate := program + extension; exists(candidate) {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: program, Err: exec.ErrNotFound}
}
