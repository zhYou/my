package fixture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyFile 只复制普通文件并保留权限，不覆盖已存在的目标。
func CopyFile(source, target string) (resultErr error) {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("无法复制非常规模板文件 %s", source)
	}
	//nolint:gosec // G301：这是公开构建产物或机器契约文件的既定权限，不包含凭据。
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, in.Close())
	}()
	//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
