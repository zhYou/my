package diagnostics

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeMetadataPath(path string, value metadata) (resultErr error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
	}()
	return writeMetadata(root, value)
}

// startDiagnosticProcess 在成功启动后登记唯一 Wait 所有者，早退也终止并等待子进程。
func startDiagnosticProcess(t *testing.T, child *exec.Cmd) func() error {
	t.Helper()
	require.NoError(t, child.Start())
	var once sync.Once
	var waitErr error
	wait := func() error {
		once.Do(func() {
			waitErr = child.Wait()
		})
		return waitErr
	}
	t.Cleanup(func() {
		// Wait 已完成时 Windows 会释放句柄；此时不再重复终止进程。
		if child.ProcessState == nil {
			if err := child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
		}
		//nolint:errcheck // 主测试断言进程结果；早退收尾可能产生主动终止错误，仍必须等待进程退出。
		_ = wait()
	})
	return wait
}
