package target

import (
	"crypto/sha256"
	"encoding/hex"
)

// UnitKind 决定缺失值和冲突的语义。
type UnitKind string

const (
	// UnitField 表示可独立删除和合并的配置字段。
	UnitField UnitKind = "field"
	// UnitFile 表示不可部分合并的完整文件。
	UnitFile UnitKind = "file"
	// UnitSecret 表示只比较版本摘要的秘密内容。
	UnitSecret UnitKind = "secret"
)

// UnitValue 是一个单位在单侧的可比较状态。
type UnitValue struct {
	Exists bool
	Digest string
	Data   []byte
}

// UnitValueFromBytes 构造带内容摘要的普通值。
func UnitValueFromBytes(data []byte) UnitValue {
	sum := sha256.Sum256(data)
	return UnitValue{
		Exists: true,
		Digest: "sha256:" + hex.EncodeToString(sum[:]),
		Data:   append([]byte(nil), data...),
	}
}

// BaselineValue 是本机上次成功同步的比较值。
type BaselineValue struct {
	Exists bool
	Secret bool
	Digest string
	Data   []byte
}

// Action 是三方比较产生的确定性下一步。
type Action string

const (
	// ActionUnchanged 表示基线、本机和远端一致。
	ActionUnchanged Action = "unchanged"
	// ActionAdopt 表示首次比较相同，可记录基线。
	ActionAdopt Action = "adopt"
	// ActionApplyRemote 表示只应把远端值应用到本机。
	ActionApplyRemote Action = "apply_remote"
	// ActionUploadLocal 表示只应把本机值提交到远端。
	ActionUploadLocal Action = "upload_local"
	// ActionConverged 表示双端已得到相同结果。
	ActionConverged Action = "converged"
	// ActionConflict 表示必须由用户选择来源。
	ActionConflict Action = "conflict"
)

// Comparison 描述单个单位的比较结果。
type Comparison struct {
	Action Action
	Code   string
}

// Compare 对基线、本机和远端值执行三方比较。
func Compare(kind UnitKind, baseline *BaselineValue, local, remote UnitValue) Comparison {
	if baseline == nil {
		if sameUnitValue(local, remote) {
			return Comparison{Action: ActionAdopt}
		}
		return Comparison{Action: ActionConflict, Code: firstTakeoverCode}
	}
	if kind == UnitFile && baseline.Exists && (!local.Exists || !remote.Exists) {
		return Comparison{Action: ActionConflict, Code: fileMissingCode}
	}
	localChanged := !sameBaseline(local, *baseline)
	remoteChanged := !sameBaseline(remote, *baseline)
	switch {
	case !localChanged && !remoteChanged:
		return Comparison{Action: ActionUnchanged}
	case localChanged && !remoteChanged:
		return Comparison{Action: ActionUploadLocal}
	case !localChanged && remoteChanged:
		return Comparison{Action: ActionApplyRemote}
	case sameUnitValue(local, remote):
		return Comparison{Action: ActionConverged}
	default:
		return Comparison{Action: ActionConflict, Code: changedBothCode}
	}
}

func sameBaseline(value UnitValue, baseline BaselineValue) bool {
	return value.Exists == baseline.Exists && sameDigest(value.Exists, baseline.Digest, value.Digest)
}

func sameUnitValue(a, b UnitValue) bool {
	return a.Exists == b.Exists && sameDigest(a.Exists, a.Digest, b.Digest)
}

func sameDigest(exists bool, a, b string) bool {
	if !exists {
		return true
	}
	if a == "" {
		a = digestBytes(nil)
	}
	if b == "" {
		b = digestBytes(nil)
	}
	return a == b
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
