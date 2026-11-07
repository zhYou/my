package machine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gofrs/flock"
	"github.com/zhYou/my/internal/datarepo"
)

const bindingFile = "machine.json"

// Binding 将当前设备身份绑定到明确的仓库和分支。
type Binding struct {
	SchemaVersion int    `json:"schema_version"`
	Repository    string `json:"repository"`
	RepositoryID  int64  `json:"repository_id"`
	Branch        string `json:"branch"`
	MachineID     string `json:"machine_id"`
}

// State 的更新须在 Lock 临界区内完成，Read 不创建资源或取得锁。
type State interface {
	Lock(context.Context) (func() error, error)
	Read() (Binding, bool, error)
	Write(Binding) error
	Clear(Binding) error
}

// FileState 使用应用私有状态目录保存机器绑定。
type FileState struct{ directory string }

// NewFileState 不执行 IO；校验在读取或加锁时完成。
func NewFileState(directory string) *FileState {
	return &FileState{directory: directory}
}

// Lock 以系统文件锁串行化本机生命周期变更。
func (s *FileState) Lock(ctx context.Context) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.directorySafe(true); err != nil {
		return nil, err
	}
	path := filepath.Join(s.directory, phaseLock)
	if err := regularPrivate(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock := flock.New(path, flock.SetPermissions(0o600))
	ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil || !ok {
		return nil, problem(codeStateInvalid, phaseLock, err, []string{}, recoverLocal)
	}
	return lock.Unlock, nil
}

// Read 验证安全类型、大小和完整 JSON；不存在表示尚未绑定。
func (s *FileState) Read() (Binding, bool, error) {
	if err := s.directorySafe(false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Binding{}, false, nil
		}
		return Binding{}, false, err
	}
	path := filepath.Join(s.directory, bindingFile)
	if err := regularPrivate(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Binding{}, false, nil
		}
		return Binding{}, false, err
	}
	file, err := os.Open(path) // #nosec G304 -- 路径由私有目录和固定文件名组成，前置校验已拒绝符号链接。
	if err != nil {
		return Binding{}, false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 65537))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 65536 {
		return Binding{}, false, problem(
			codeStateInvalid,
			phaseBindingRead,
			errors.Join(readErr, closeErr),
			[]string{},
			recoverLocal,
		)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var binding Binding
	var extra any
	if err := decoder.Decode(&binding); err != nil {
		return Binding{}, false, problem(
			codeStateInvalid,
			phaseBindingRead,
			err,
			[]string{},
			recoverLocal,
		)
	}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) || binding.Validate() != nil {
		return Binding{}, false, problem(
			codeStateInvalid,
			phaseBindingRead,
			err,
			[]string{},
			recoverLocal,
		)
	}
	return binding, true, nil
}

// Write 以同目录临时文件和独占链接原子发布，既有不同绑定永不替换。
func (s *FileState) Write(binding Binding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	current, exists, err := s.Read()
	if err != nil {
		return err
	}
	if exists {
		if current == binding {
			return nil
		}
		return problem(codeBindingConflict, phaseBindingWrite, nil, []string{}, recoverLocal)
	}
	if err := s.directorySafe(true); err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.directory, ".binding-*")
	if err != nil {
		return err
	}
	defer func() {
		//nolint:errcheck // 尽力清理临时文件，发布结果由 Link 的返回值决定。
		_ = os.Remove(temp.Name())
	}()
	data, err := json.Marshal(binding)
	if err == nil {
		_, err = temp.Write(append(data, '\n'))
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return os.Link(temp.Name(), filepath.Join(s.directory, bindingFile))
}

// Clear 在临界区内重新核对精确绑定，防止清除其他身份。
func (s *FileState) Clear(expected Binding) error {
	current, exists, err := s.Read()
	if err != nil || !exists {
		return err
	}
	if current != expected {
		return problem(codeBindingConflict, phaseBindingClear, nil, []string{}, recoverLocal)
	}
	return os.Remove(filepath.Join(s.directory, bindingFile))
}

// Validate 拒绝缺少仓库身份或非法机器标识的本机状态。
func (b Binding) Validate() error {
	cfg := datarepo.Config{Repository: b.Repository, Branch: b.Branch, RequestTimeout: time.Second}
	if b.SchemaVersion != 1 || b.RepositoryID <= 0 || cfg.Validate() != nil || ValidateID(b.MachineID) != nil {
		return problem(codeStateInvalid, phaseBindingRead, nil, []string{}, recoverLocal)
	}
	return nil
}

func (s *FileState) directorySafe(create bool) error {
	if !filepath.IsAbs(s.directory) {
		return problem(codeStateInvalid, phaseBindingRead, nil, []string{}, recoverLocal)
	}
	for _, path := range []string{filepath.Dir(s.directory), s.directory} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
			info, err = os.Lstat(path)
		}
		if err != nil {
			return err
		}
		mask := os.FileMode(0o022)
		if path == s.directory {
			mask = 0o077
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
			runtime.GOOS != platformWindows && info.Mode().Perm()&mask != 0 {
			return problem(codeStateInvalid, phaseBindingRead, nil, []string{}, recoverLocal)
		}
	}
	return nil
}

func regularPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || runtime.GOOS != platformWindows && info.Mode().Perm()&0o077 != 0 {
		return problem(codeStateInvalid, phaseBindingRead, nil, []string{}, recoverLocal)
	}
	return nil
}
