package configuration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/zhYou/my/internal/datarepo"
)

// Baseline 保存一个目标上次成功同步的单位状态。
type Baseline struct {
	Target TargetID
	Units  map[UnitID]BaselineValue
}

type baselineEnvelope struct {
	SchemaVersion int                      `json:"schema_version"`
	RepositoryID  int64                    `json:"repository_id"`
	Repository    string                   `json:"repository"`
	Branch        string                   `json:"branch"`
	Target        TargetID                 `json:"target"`
	Units         map[UnitID]BaselineValue `json:"units"`
}

// State 是本机配置基线的锁定、读取、原子发布和清理边界。
type State interface {
	Lock(context.Context) (func() error, error)
	Read(datarepo.Metadata, TargetID) (Baseline, bool, error)
	Write(datarepo.Metadata, Baseline) error
	Clear(datarepo.Metadata, TargetID) error
}

// FileState 将配置基线保存在应用私有状态目录。
type FileState struct {
	directory string
}

// NewFileState 不执行 IO；路径在读取或写入时校验。
func NewFileState(directory string) *FileState {
	return &FileState{directory: directory}
}

// Lock 串行化同一应用目录中的配置状态修改。
func (s *FileState) Lock(ctx context.Context) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ensureDirectory(); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(s.configurationDirectory(), stateLockFile), flock.SetPermissions(stateFileMode))
	ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil || !ok {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, problem(codeStateInvalid, phaseStateRead, err)
	}
	return lock.Unlock, nil
}

// Read 读取指定仓库、分支和目标的本机基线。
func (s *FileState) Read(metadata datarepo.Metadata, target TargetID) (Baseline, bool, error) {
	if err := target.Validate(); err != nil {
		return Baseline{}, false, err
	}
	file, err := s.baselinePath(metadata, target)
	if err != nil {
		return Baseline{}, false, err
	}
	data, err := os.ReadFile(file) // #nosec G304 -- 路径由校验后的私有目录、仓库 ID、分支摘要和固定目标 ID 构成。
	if errors.Is(err, os.ErrNotExist) {
		return Baseline{}, false, nil
	}
	if err != nil {
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, err)
	}
	if len(data) > 1<<20 {
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope baselineEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.SchemaVersion != stateSchemaVersion ||
		envelope.RepositoryID != metadata.RepositoryID ||
		envelope.Repository != metadata.Repository ||
		envelope.Branch != metadata.Branch ||
		envelope.Target != target ||
		envelope.Units == nil {
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, nil)
	}
	for id, value := range envelope.Units {
		if err := id.Validate(); err != nil || value.Secret && len(value.Data) != 0 {
			return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, err)
		}
	}
	return Baseline{Target: target, Units: envelope.Units}, true, nil
}

// Write 严格编码并以同目录临时文件原子发布基线。
func (s *FileState) Write(metadata datarepo.Metadata, baseline Baseline) error {
	if err := baseline.Validate(); err != nil {
		return err
	}
	file, err := s.baselinePath(metadata, baseline.Target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), stateDirectoryMode); err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	envelope := baselineEnvelope{
		SchemaVersion: stateSchemaVersion,
		RepositoryID:  metadata.RepositoryID,
		Repository:    metadata.Repository,
		Branch:        metadata.Branch,
		Target:        baseline.Target,
		Units:         baseline.Units,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(file), ".baseline-*")
	if err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	tempName := temp.Name()
	replaced := false
	defer func() {
		if !replaced {
			//nolint:errcheck // 临时文件清理失败不覆盖已有的写入错误。
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(stateFileMode); err != nil {
		//nolint:errcheck // Chmod 已失败，Close 的清理错误不覆盖根因。
		_ = temp.Close()
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
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
	if err := os.Rename(tempName, file); err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	replaced = true
	return nil
}

// Clear 删除指定目标的基线，不修改配置目标文件。
func (s *FileState) Clear(metadata datarepo.Metadata, target TargetID) error {
	file, err := s.baselinePath(metadata, target)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	return nil
}

func (s *FileState) baselinePath(metadata datarepo.Metadata, target TargetID) (string, error) {
	if err := target.Validate(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(s.directory) ||
		metadata.RepositoryID <= 0 ||
		metadata.Repository == "" ||
		metadata.Branch == "" {
		return "", problem(codeStateInvalid, phaseStateRead, nil)
	}
	sum := sha256.Sum256([]byte(metadata.Branch))
	return filepath.Join(
		s.configurationDirectory(),
		strconv.FormatInt(metadata.RepositoryID, 10),
		hex.EncodeToString(sum[:]),
		string(target)+".json",
	), nil
}

func (s *FileState) ensureDirectory() error {
	if strings.TrimSpace(s.directory) == "" || !filepath.IsAbs(s.directory) {
		return problem(codeStateInvalid, phaseStateRead, nil)
	}
	directory := s.configurationDirectory()
	if err := os.MkdirAll(directory, stateDirectoryMode); err != nil {
		return problem(codeStateInvalid, phaseStateRead, err)
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return problem(codeStateInvalid, phaseStateRead, err)
	}
	return nil
}

// Validate 检查目标、单位和秘密基线的持久化边界。
func (b Baseline) Validate() error {
	if err := b.Target.Validate(); err != nil {
		return err
	}
	if b.Units == nil {
		return invalidInput()
	}
	for id, value := range b.Units {
		if err := id.Validate(); err != nil || value.Secret && len(value.Data) != 0 {
			return invalidInput()
		}
	}
	if b.Target == TargetCodexAuth || b.Target == TargetShellEnvSecret {
		for _, value := range b.Units {
			if !value.Secret {
				return invalidInput()
			}
		}
	}
	return nil
}

func (s *FileState) directoryModeForTest() os.FileMode {
	info, err := os.Stat(s.configurationDirectory())
	if err != nil {
		return 0
	}
	return info.Mode().Perm()
}

func (s *FileState) configurationDirectory() string {
	return filepath.Join(s.directory, "configuration")
}
