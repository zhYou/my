package shellenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/base/privatefs"

	"github.com/gofrs/flock"
	"github.com/zhYou/my/internal/datarepo"
)

// CacheState 保存声明和密文缓存，不包含秘密明文。
type CacheState struct {
	MachineID        string            `json:"machine_id"`
	Snapshot         datarepo.Metadata `json:"snapshot"`
	Ordinary         []byte            `json:"ordinary,omitempty"`
	SecretCiphertext []byte            `json:"secret_ciphertext,omitempty"`
	SecretVersion    string            `json:"secret_version,omitempty"`
	SecretRecipient  string            `json:"secret_recipient,omitempty"`
}

type cacheEnvelope struct {
	SchemaVersion int        `json:"schema_version"`
	State         CacheState `json:"state"`
}

// Cache 是本机离线激活和远端更新之间的持久边界。
type Cache interface {
	Lock(context.Context) (func() error, error)
	Read() (CacheState, bool, error)
	Write(CacheState) error
}

// FileCache 将环境缓存写入应用私有状态目录。
type FileCache struct {
	directory string
	machineID string
}

// NewFileCache 不执行 IO。
func NewFileCache(directory, machineID string) *FileCache {
	return &FileCache{directory: directory, machineID: machineID}
}

// Lock 串行化缓存更新。
func (c *FileCache) Lock(ctx context.Context) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.ensureDirectory(true); err != nil {
		return nil, err
	}
	path := filepath.Join(c.cacheDirectory(), "lock")
	if _, err := privatefs.File(path, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, problem(codeState, phaseRead, err)
	}
	lock := flock.New(path, flock.SetPermissions(0o600))
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
func (c *FileCache) Read() (CacheState, bool, error) {
	path := filepath.Join(c.cacheDirectory(), "state.json")
	if err := c.ensureDirectory(false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CacheState{}, false, nil
		}
		return CacheState{}, false, problem(codeState, phaseRead, err)
	}
	data, err := privatefs.ReadFile(path, maxCacheSize)
	if errors.Is(err, os.ErrNotExist) {
		return CacheState{}, false, nil
	}
	if err != nil {
		return CacheState{}, false, problem(codeState, phaseRead, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope cacheEnvelope
	if err := decoder.Decode(&envelope); err != nil ||
		envelope.SchemaVersion != schemaVersion ||
		envelope.State.Snapshot.RepositoryID <= 0 || envelope.State.MachineID != c.machineID {
		return CacheState{}, false, problem(codeState, phaseRead, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return CacheState{}, false, problem(codeState, phaseRead, nil)
	}
	return envelope.State, true, nil
}

// Write 以原子替换发布缓存。
func (c *FileCache) Write(state CacheState) error {
	if state.Snapshot.RepositoryID <= 0 || state.MachineID != c.machineID {
		return invalidInput()
	}
	if err := c.ensureDirectory(true); err != nil {
		return err
	}
	data, err := json.Marshal(cacheEnvelope{SchemaVersion: schemaVersion, State: state})
	if err != nil {
		return problem(codeState, phaseWrite, err)
	}
	path := filepath.Join(c.cacheDirectory(), "state.json")
	if _, err := privatefs.File(path, maxCacheSize); err != nil && !errors.Is(err, os.ErrNotExist) {
		return problem(codeState, phaseWrite, err)
	}
	if len(data)+1 > maxCacheSize {
		return problem(codeState, phaseWrite, privatefs.ErrLimit)
	}
	if err := privatefs.AtomicWrite(path, append(data, '\n'), privatefs.WriteOptions{Mode: 0o600}); err != nil {
		return problem(codeState, phaseWrite, err)
	}
	return nil
}

const maxCacheSize = 8 << 20

func (c *FileCache) ensureDirectory(create bool) error {
	if err := machine.ValidateID(c.machineID); err != nil {
		return err
	}
	if err := privatefs.Directory(c.directory, create); err != nil {
		return err
	}
	return privatefs.Directory(c.cacheDirectory(), create)
}

func (c *FileCache) cacheDirectory() string {
	return filepath.Join(c.directory, c.machineID)
}
