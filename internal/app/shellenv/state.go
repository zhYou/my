package shellenv

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

type cacheState struct {
	Snapshot         datarepo.Metadata `json:"snapshot"`
	Ordinary         []byte            `json:"ordinary,omitempty"`
	SecretCiphertext []byte            `json:"secret_ciphertext,omitempty"`
	SecretVersion    string            `json:"secret_version,omitempty"`
	SecretRecipient  string            `json:"secret_recipient,omitempty"`
}

type cacheEnvelope struct {
	SchemaVersion int        `json:"schema_version"`
	State         cacheState `json:"state"`
}

// Cache 是本机离线激活和远端更新之间的持久边界。
type Cache interface {
	Lock(context.Context) (func() error, error)
	Read() (cacheState, bool, error)
	Write(cacheState) error
}

// FileCache 将环境缓存写入应用私有状态目录。
type FileCache struct {
	directory string
}

// NewFileCache 不执行 IO。
func NewFileCache(directory string) *FileCache {
	return &FileCache{directory: directory}
}

// Lock 串行化缓存更新。
func (c *FileCache) Lock(ctx context.Context) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.ensureDirectory(); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(c.directory, "lock"), flock.SetPermissions(0o600))
	ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil || !ok {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, problem(codeState, phaseRead, err)
	}
	return lock.Unlock, nil
}

// Read 读取当前本机环境缓存。
func (c *FileCache) Read() (cacheState, bool, error) {
	path := filepath.Join(c.directory, "state.json")
	data, err := os.ReadFile(path) // #nosec G304 -- 固定私有缓存路径。
	if errors.Is(err, os.ErrNotExist) {
		return cacheState{}, false, nil
	}
	if err != nil {
		return cacheState{}, false, problem(codeState, phaseRead, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope cacheEnvelope
	if err := decoder.Decode(&envelope); err != nil ||
		envelope.SchemaVersion != schemaVersion ||
		envelope.State.Snapshot.RepositoryID <= 0 {
		return cacheState{}, false, problem(codeState, phaseRead, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return cacheState{}, false, problem(codeState, phaseRead, nil)
	}
	return envelope.State, true, nil
}

// Write 以原子替换发布缓存。
func (c *FileCache) Write(state cacheState) error {
	if state.Snapshot.RepositoryID <= 0 {
		return invalidInput()
	}
	if err := c.ensureDirectory(); err != nil {
		return err
	}
	data, err := json.Marshal(cacheEnvelope{SchemaVersion: schemaVersion, State: state})
	if err != nil {
		return problem(codeState, phaseWrite, err)
	}
	path := filepath.Join(c.directory, "state.json")
	temp, err := os.CreateTemp(c.directory, ".state-*")
	if err != nil {
		return problem(codeState, phaseWrite, err)
	}
	tempPath := temp.Name()
	replaced := false
	defer func() {
		if !replaced {
			//nolint:errcheck // 清理失败不覆盖写入根因。
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		//nolint:errcheck // Chmod 已失败。
		_ = temp.Close()
		return problem(codeState, phaseWrite, err)
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		//nolint:errcheck // Write 已失败。
		_ = temp.Close()
		return problem(codeState, phaseWrite, err)
	}
	if err := temp.Sync(); err != nil {
		//nolint:errcheck // Sync 已失败。
		_ = temp.Close()
		return problem(codeState, phaseWrite, err)
	}
	if err := temp.Close(); err != nil {
		return problem(codeState, phaseWrite, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return problem(codeState, phaseWrite, err)
	}
	replaced = true
	return nil
}

func (c *FileCache) ensureDirectory() error {
	if !filepath.IsAbs(c.directory) {
		return problem(codeState, phaseRead, nil)
	}
	if err := os.MkdirAll(c.directory, 0o700); err != nil {
		return problem(codeState, phaseRead, err)
	}
	info, err := os.Stat(c.directory)
	if err != nil || !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return problem(codeState, phaseRead, err)
	}
	return nil
}
