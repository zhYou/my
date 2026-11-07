package machine

import (
	"bytes"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/zhYou/my/internal/datarepo"
)

const (
	// Active 表示可绑定的机器记录。
	Active = "active"
	// Retired 保留退役历史并要求显式重新激活。
	Retired = "retired"
)

var idPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// Record 是私有数据仓中的机器资料；Note 不进入诊断出口。
type Record struct {
	SchemaVersion int        `toml:"schema_version" json:"schema_version"`
	MachineID     string     `toml:"machine_id" json:"machine_id"`
	Kind          string     `toml:"kind" json:"kind"`
	OS            string     `toml:"os" json:"os"`
	Arch          string     `toml:"arch" json:"arch"`
	Note          string     `toml:"note" json:"note"`
	RegisteredAt  time.Time  `toml:"registered_at" json:"registered_at"`
	Status        string     `toml:"status" json:"status"`
	RetiredAt     *time.Time `toml:"retired_at,omitempty" json:"retired_at,omitempty"`
}

// RegisterOptions 是当前设备登记的显式输入。
type RegisterOptions struct {
	ID   string
	Kind string
	Note string
}

// Validate 在读取配置及创建资源之前校验登记参数。
func (o RegisterOptions) Validate() error {
	if err := ValidateID(o.ID); err != nil {
		return err
	}
	if o.Kind != "laptop" && o.Kind != "desktop" && o.Kind != "server" && o.Kind != "vm" ||
		strings.TrimSpace(o.Note) == "" ||
		strings.ContainsAny(o.Note, "\r\n\x00") {
		return invalidInput()
	}
	return nil
}

// BindOptions 声明恢复身份时允许的显式状态改变。
type BindOptions struct {
	ID             string
	Reactivate     bool
	UpdatePlatform bool
}

// Validate 只校验本地可确定的输入。
func (o BindOptions) Validate() error {
	return ValidateID(o.ID)
}

// ValidateID 拒绝路径、控制字符及不稳定的机器标识。
func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return invalidInput()
	}
	return nil
}

// Records 严格解码快照中的机器记录，并验证文件名与身份一致。
func Records(s datarepo.Snapshot) (map[string]Record, error) {
	result := map[string]Record{}
	if !s.Initialized {
		return nil, problem("machine.not_initialized", "data.validate", nil, []string{}, "machine.recover_init")
	}
	for name, data := range s.Files {
		if !strings.HasPrefix(name, "machines/") {
			continue
		}
		var record Record
		decoder := toml.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&record)
		if err != nil || record.Validate() != nil || name != "machines/"+record.MachineID+".toml" ||
			path.Base(name) != record.MachineID+".toml" {
			return nil, problem(codeRecordInvalid, phaseRead, err, []string{}, recoverRecord)
		}
		result[record.MachineID] = record
	}
	return result, nil
}

// Validate 检查业务记录，即使不是由 CLI 创建也必须满足同样约束。
func (r Record) Validate() error {
	if err := (RegisterOptions{ID: r.MachineID, Kind: r.Kind, Note: r.Note}).Validate(); err != nil {
		return err
	}
	if r.SchemaVersion != 1 || r.RegisteredAt.IsZero() ||
		r.OS != "linux" && r.OS != "darwin" && r.OS != platformWindows ||
		r.Arch != "amd64" && r.Arch != "arm64" ||
		r.Status != Active && r.Status != Retired ||
		r.Status == Active && r.RetiredAt != nil ||
		r.Status == Retired && (r.RetiredAt == nil || r.RetiredAt.IsZero()) {
		return invalidInput()
	}
	return nil
}

func encodeRecord(r Record) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return toml.Marshal(r)
}
