package configuration

import (
	"errors"
	"os"
	"path/filepath"
)

// AtomicWriteFile 在目标目录写入临时普通文件并原子替换目标。
func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return problem(codeStateInvalid, phaseStateWrite, nil)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".my-config-*")
	if err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	tempPath := temp.Name()
	replaced := false
	defer func() {
		if !replaced {
			//nolint:errcheck // 临时文件清理失败不覆盖已有的写入错误。
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(mode.Perm()); err != nil {
		//nolint:errcheck // Chmod 已失败，Close 的清理错误不覆盖根因。
		_ = temp.Close()
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if _, err := temp.Write(data); err != nil {
		//nolint:errcheck // Write 已失败，Close 的清理错误不覆盖根因。
		_ = temp.Close()
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if err := temp.Sync(); err != nil {
		//nolint:errcheck // Sync 已失败，Close 的清理错误不覆盖根因。
		_ = temp.Close()
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if err := temp.Close(); err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	replaced = true
	return nil
}
