package scaffold

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/spf13/afero"
)

// Initializer 显式持有模板事务所用的文件系统、命令与锁。
type Initializer struct {
	FS         afero.Fs
	RunCommand func(context.Context, string, ...string) ([]byte, error)
	Lock       func(string) (func() error, error)
}

func realCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	//nolint:gosec // G204：开发任务显式执行固定工具及拆分参数，不经过 shell；参数来自仓库或调用者。
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	return cmd.CombinedOutput()
}

func osLock(path string) (func() error, error) {
	lock := flock.New(path)
	ok, err := lock.TryLock()
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	if !ok {
		return nil, errors.Join(errors.New("已有初始化进程正在运行"), lock.Close())
	}
	return lock.Close, nil
}

func (i *Initializer) defaults() Initializer {
	configured := *i
	if configured.FS == nil {
		configured.FS = afero.NewOsFs()
	}
	if configured.RunCommand == nil {
		configured.RunCommand = realCommand
	}
	if configured.Lock == nil {
		configured.Lock = osLock
	}
	return configured
}

func (i *Initializer) validateModule(ctx context.Context, module string) (resultErr error) {
	dir, err := os.MkdirTemp("", "cli-module-check-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(dir))
	}()
	output, err := i.RunCommand(ctx, "go", "-C", dir, "mod", "init", module)
	if err != nil {
		return fmt.Errorf("无效的 Go 模块路径：%w：%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// safePath 检查每一级路径，不允许符号链接、上层跳转或非普通目标。
func (i *Initializer) safePath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") ||
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative ||
		relative == ".." ||
		strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("不安全的相对路径 %q", relative)
	}
	path := root
	for index, part := range strings.Split(relative, "/") {
		path = filepath.Join(path, part)
		var info os.FileInfo
		var err error
		if fs, ok := i.FS.(afero.Lstater); ok {
			info, _, err = fs.LstatIfPossible(path)
		} else {
			info, err = i.FS.Stat(path)
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || index < len(strings.Split(relative, "/"))-1 && !info.IsDir() ||
			index == len(strings.Split(relative, "/"))-1 && !info.Mode().IsRegular() && !info.IsDir() {
			return "", fmt.Errorf("拒绝非常规或符号链接路径 %q", relative)
		}
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}

func (i *Initializer) atomicWrite(item change) error {
	file, err := afero.TempFile(i.FS, filepath.Dir(item.path), ".template-init-*")
	if err != nil {
		return err
	}
	return i.finishAtomicWrite(file, item)
}

// atomicWriteAt 仅使用已经由事务日志确认所有权的临时路径。
func (i *Initializer) atomicWriteAt(item change, temporary string) error {
	file, err := i.FS.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, item.mode)
	if err != nil {
		return err
	}
	return i.finishAtomicWrite(file, item)
}

func (i *Initializer) finishAtomicWrite(file afero.File, item change) (resultErr error) {
	tempName := file.Name()
	defer func() {
		if err := i.FS.Remove(tempName); !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := i.FS.Chmod(tempName, item.mode); err != nil {
		return errors.Join(err, file.Close())
	}
	n, writeErr := file.Write(item.data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if n != len(item.data) {
		return io.ErrShortWrite
	}
	if closeErr != nil {
		return closeErr
	}
	return i.FS.Rename(tempName, item.path)
}
