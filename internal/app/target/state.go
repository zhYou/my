package target

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
	"strconv"
	"strings"
	"time"

	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/base/privatefs"

	"github.com/gofrs/flock"
	"github.com/zhYou/my/internal/datarepo"
)

// Baseline 保存一个目标上次成功同步的单位状态。
type Baseline struct {
	Target  ID
	Units   map[UnitID]BaselineValue
	Pending *PendingSecret
}

// PendingSecret 记录认证文件原子替换前后的摘要，供中断恢复判断。
type PendingSecret struct {
	Version        string `json:"version"`
	BeforeExists   bool   `json:"before_exists"`
	BeforeDigest   string `json:"before_digest"`
	SnapshotDigest string `json:"snapshot_digest"`
}

type baselineEnvelope struct {
	SchemaVersion int                      `json:"schema_version"`
	MachineID     string                   `json:"machine_id"`
	RepositoryID  int64                    `json:"repository_id"`
	Repository    string                   `json:"repository"`
	Branch        string                   `json:"branch"`
	Target        ID                       `json:"target"`
	Units         map[UnitID]BaselineValue `json:"units"`
	Pending       *PendingSecret           `json:"pending,omitempty"`
}

// State 是本机配置基线的锁定、读取、原子发布和清理边界。
type State interface {
	Lock(context.Context) (func() error, error)
	Read(datarepo.Metadata, ID) (Baseline, bool, error)
	Write(datarepo.Metadata, Baseline) error
	Clear(datarepo.Metadata, ID) error
}

// FileState 将配置基线保存在应用私有状态目录。
type FileState struct {
	directory string
	machineID string
}

// NewFileState 不执行 IO；路径在读取或写入时校验。
func NewFileState(directory, machineID string) *FileState {
	return &FileState{directory: directory, machineID: machineID}
}

// Lock 串行化同一应用目录中的配置状态修改。
func (s *FileState) Lock(ctx context.Context) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ensureDirectory(true); err != nil {
		return nil, err
	}
	path := filepath.Join(s.targetDirectory(), stateLockFile)
	if _, err := privatefs.File(path, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, problem(codeStateInvalid, phaseStateRead, err)
	}
	lock := flock.New(path, flock.SetPermissions(stateFileMode))
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
func (s *FileState) Read(metadata datarepo.Metadata, target ID) (Baseline, bool, error) {
	if err := target.Validate(); err != nil {
		return Baseline{}, false, err
	}
	file, err := s.baselinePath(metadata, target)
	if err != nil {
		return Baseline{}, false, err
	}
	if err := s.prepareParents(file, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Baseline{}, false, nil
		}
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, err)
	}
	data, err := privatefs.ReadFile(file, maxBaselineSize)
	if errors.Is(err, os.ErrNotExist) {
		return Baseline{}, false, nil
	}
	if err != nil {
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, err)
	}
	if len(data) > maxBaselineSize {
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
		envelope.MachineID != s.machineID ||
		!strings.EqualFold(envelope.Repository, metadata.Repository) ||
		envelope.Branch != metadata.Branch ||
		envelope.Target != target ||
		envelope.Units == nil {
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, nil)
	}
	baseline := Baseline{Target: target, Units: envelope.Units, Pending: envelope.Pending}
	if err := baseline.Validate(); err != nil {
		return Baseline{}, false, problem(codeStateInvalid, phaseStateRead, err)
	}
	return baseline, true, nil
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
	if err := s.prepareParents(file, true); err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if _, err := privatefs.File(file, maxBaselineSize); err != nil && !errors.Is(err, os.ErrNotExist) {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	envelope := baselineEnvelope{
		SchemaVersion: stateSchemaVersion, RepositoryID: metadata.RepositoryID, MachineID: s.machineID,
		Repository: metadata.Repository, Branch: metadata.Branch, Target: baseline.Target,
		Units: baseline.Units, Pending: baseline.Pending,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if len(data)+1 > maxBaselineSize {
		return problem(codeStateInvalid, phaseStateWrite, privatefs.ErrLimit)
	}
	if err := privatefs.AtomicWrite(file, append(data, '\n'), privatefs.WriteOptions{Mode: stateFileMode}); err != nil {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	return nil
}

// Clear 删除指定目标的基线，不修改配置目标文件。
func (s *FileState) Clear(metadata datarepo.Metadata, target ID) error {
	file, err := s.baselinePath(metadata, target)
	if err != nil {
		return err
	}
	if err := s.prepareParents(file, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if _, err := privatefs.File(file, maxBaselineSize); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return problem(codeStateInvalid, phaseStateWrite, err)
	}
	return nil
}

func (s *FileState) baselinePath(metadata datarepo.Metadata, target ID) (string, error) {
	if err := target.Validate(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(s.directory) ||
		metadata.RepositoryID <= 0 ||
		metadata.Repository == "" ||
		metadata.Branch == "" || machine.ValidateID(s.machineID) != nil {
		return "", problem(codeStateInvalid, phaseStateRead, nil)
	}
	sum := sha256.Sum256([]byte(metadata.Branch))
	return filepath.Join(
		s.targetDirectory(),
		strconv.FormatInt(metadata.RepositoryID, 10),
		hex.EncodeToString(sum[:]),
		s.machineID,
		string(target)+".json",
	), nil
}

const maxBaselineSize = 16 << 20

func (s *FileState) ensureDirectory(create bool) error {
	for _, directory := range []string{s.directory, s.targetDirectory()} {
		if err := privatefs.Directory(directory, create); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileState) prepareParents(file string, create bool) error {
	if err := s.ensureDirectory(create); err != nil {
		return err
	}
	directory := s.targetDirectory()
	relative, err := filepath.Rel(directory, filepath.Dir(file))
	if err != nil {
		return err
	}
	for part := range strings.SplitSeq(relative, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		directory = filepath.Join(directory, part)
		if err := privatefs.Directory(directory, create); err != nil {
			return err
		}
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
	if b.Target == CodexAuth || b.Target == ShellEnvSecret {
		for _, value := range b.Units {
			if !value.Secret {
				return invalidInput()
			}
		}
	}
	if b.Pending != nil &&
		(b.Target != CodexAuth && b.Target != ShellEnvSecret ||
			b.Pending.Version == "" ||
			b.Pending.BeforeExists && b.Pending.BeforeDigest == "" ||
			!b.Pending.BeforeExists && b.Pending.BeforeDigest != "" ||
			b.Pending.SnapshotDigest == "") {
		return invalidInput()
	}
	return nil
}

func (s *FileState) targetDirectory() string {
	return filepath.Join(s.directory, "target")
}
