package privatefs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

var (
	// ErrUnsafe 表示路径、类型或权限不满足私有文件边界。
	ErrUnsafe = errors.New("unsafe private filesystem entry")
	// ErrLimit 表示文件超过调用方声明的大小上限。
	ErrLimit = errors.New("private file exceeds size limit")
)

// WriteOptions 声明新文件权限及是否保留既有普通文件的权限。
type WriteOptions struct {
	Mode         os.FileMode
	PreserveMode bool
}

// Directory 校验目录和相邻父目录；create 允许以 0700 创建缺失目录。
func Directory(path string, create bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrUnsafe
	}
	for _, entry := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(entry)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := os.MkdirAll(entry, 0o700); err != nil {
				return err
			}
			info, err = os.Lstat(entry)
		}
		if err != nil {
			return err
		}
		mask := os.FileMode(0o022)
		if entry == path {
			mask = 0o077
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
			runtime.GOOS != "windows" && info.Mode().Perm()&mask != 0 {
			return ErrUnsafe
		}
	}
	return nil
}

// File 拒绝非普通、符号链接或过宽权限文件；正 limit 同时限制文件大小。
func File(path string, limit int64) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, ErrUnsafe
	}
	if limit > 0 && info.Size() > limit {
		return nil, ErrLimit
	}
	return info, nil
}

// ReadFile 在类型、权限和大小检查后有界读取；调用方校验所属目录。
func ReadFile(path string, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, ErrLimit
	}
	info, err := File(path, limit)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path) // #nosec G304 -- 已拒绝符号链接与特殊文件，调用方另行校验所属目录。
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(info, opened) {
		return nil, errors.Join(ErrUnsafe, statErr, file.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if int64(len(data)) > limit {
		readErr = errors.Join(readErr, ErrLimit)
	}
	return data, errors.Join(readErr, closeErr)
}

// AtomicWrite 使用同目录临时文件替换普通文件，所有失败保留关闭和清理错误。
// 调用方在持有业务锁期间校验父目录和私有权限；普通应用文件可选择保留权限。
func AtomicWrite(path string, data []byte, options WriteOptions) (err error) {
	mode := options.Mode.Perm()
	if mode == 0 {
		mode = 0o600
	}
	info, statErr := os.Lstat(path)
	switch {
	case statErr == nil:
		if !info.Mode().IsRegular() {
			return ErrUnsafe
		}
		if options.PreserveMode {
			mode = info.Mode().Perm()
		}
	case !errors.Is(statErr, os.ErrNotExist):
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".my-write-*")
	if err != nil {
		return err
	}
	closed := false
	replaced := false
	defer func() {
		if !closed {
			err = errors.Join(err, temp.Close())
		}
		if !replaced {
			removeErr := os.Remove(temp.Name())
			if !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	err = temp.Close()
	closed = true
	if err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	replaced = true
	return nil
}
