package target

import (
	"errors"
	"os"

	"github.com/zhYou/my/internal/base/privatefs"
)

// AtomicWriteFile 原子替换普通配置文件，保留既有权限并报告清理失败。
func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := privatefs.AtomicWrite(path, data, privatefs.WriteOptions{Mode: mode, PreserveMode: true}); err != nil {
		if errors.Is(err, privatefs.ErrUnsafe) {
			return unsupportedFile(phaseStateWrite)
		}
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	return nil
}
