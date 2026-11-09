package configuration

import (
	"slices"
	"strings"
	"unicode"

	"github.com/zhYou/my/internal/app/machine"
)

// TargetID 是配置目标在 CLI 和数据仓中的稳定标识。
type TargetID string

const (
	// TargetCodexSettings 管理 Codex 主配置中的权限和联网字段。
	TargetCodexSettings TargetID = "codex-settings"
	// TargetCodexRules 管理 Codex 的固定规则文件集合。
	TargetCodexRules TargetID = "codex-rules"
	// TargetCodexDeepSeek 管理 DeepSeek provider 配置和模型目录。
	TargetCodexDeepSeek TargetID = "codex-deepseek"
	// TargetCodexAuth 管理 age 加密的 Codex 登录快照。
	TargetCodexAuth TargetID = "codex-auth"
	// TargetMise 管理 mise 工具声明和受控设置。
	TargetMise TargetID = "mise"
	// TargetTopgrade 管理 Topgrade 完整配置。
	TargetTopgrade TargetID = "topgrade"
	// TargetClashGlobal 管理 Clash 全局扩展脚本。
	TargetClashGlobal TargetID = "clash-global"
	// TargetClashLAN 管理 Clash 单机 LAN 覆写。
	TargetClashLAN TargetID = "clash-lan"
	// TargetShellEnv 管理普通 shell 环境变量。
	TargetShellEnv TargetID = "shell-env"
	// TargetShellEnvSecret 管理 age 加密的 shell 环境变量。
	TargetShellEnvSecret TargetID = "shell-env-secret"
)

// Validate 拒绝路径、大小写漂移和不稳定目标 ID。
func (id TargetID) Validate() error {
	value := string(id)
	if value == "" || value[0] == '-' || value[len(value)-1] == '-' {
		return invalidInput()
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return invalidInput()
	}
	return nil
}

// UnitID 标识配置目标内部可独立比较和解决的单位。
type UnitID string

// SecretUnitID 是秘密文件当前分发版本的比较单位。
const SecretUnitID UnitID = "secret:current"

// FieldUnitID 构造结构化字段单位 ID。
func FieldUnitID(parts ...string) UnitID {
	return UnitID("field:" + strings.Join(parts, "."))
}

// FileUnitID 构造完整文件单位 ID。
func FileUnitID(name string) UnitID {
	return UnitID("file:" + name)
}

// Validate 检查单位前缀和稳定路径字符。
func (id UnitID) Validate() error {
	value := string(id)
	body := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(value, "field:"), "file:"), "secret:")
	if body == "" || body != value {
		if body == "" {
			return invalidInput()
		}
	}
	validPrefix := strings.HasPrefix(value, "field:") ||
		strings.HasPrefix(value, "file:") ||
		strings.HasPrefix(value, "secret:")
	if !validPrefix || strings.ContainsAny(body, "/\\\x00") || strings.Contains(body, "..") {
		return invalidInput()
	}
	for _, r := range body {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return invalidInput()
		}
	}
	return nil
}

// MachineScope 描述目标对全部 active 机器或指定机器生效。
type MachineScope struct {
	All bool
	IDs []string
}

// Validate 校验范围与当前已登记机器的一致性。
func (s MachineScope) Validate(known []string) error {
	if s.All {
		if len(s.IDs) != 0 {
			return invalidInput()
		}
		return nil
	}
	if len(s.IDs) == 0 {
		return invalidInput()
	}
	seen := make(map[string]struct{}, len(s.IDs))
	for _, id := range s.IDs {
		if err := machine.ValidateID(id); err != nil {
			return invalidInput()
		}
		if _, duplicate := seen[id]; duplicate || !slices.Contains(known, id) {
			return invalidInput()
		}
		seen[id] = struct{}{}
	}
	return nil
}

// Applies 报告范围是否包含指定机器。
func (s MachineScope) Applies(machineID string) bool {
	if s.All {
		return len(s.IDs) == 0
	}
	return slices.Contains(s.IDs, machineID)
}
