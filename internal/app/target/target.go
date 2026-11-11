package target

import (
	"slices"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/zhYou/my/internal/app/machine"
)

// ID 是配置目标在 CLI 和数据仓中的稳定标识。
type ID string

const (
	// CodexSettings 管理 Codex 主配置中的权限和联网字段。
	CodexSettings ID = "codex-settings"
	// CodexRules 管理 Codex 的固定规则文件集合。
	CodexRules ID = "codex-rules"
	// CodexDeepSeek 管理 DeepSeek provider 配置和模型目录。
	CodexDeepSeek ID = "codex-deepseek"
	// CodexAuth 管理 age 加密的 Codex 登录快照。
	CodexAuth ID = "codex-auth"
	// Mise 管理 mise 工具声明和受控设置。
	Mise ID = "mise"
	// Topgrade 管理 Topgrade 完整配置。
	Topgrade ID = "topgrade"
	// ClashGlobal 管理 Clash 全局扩展脚本。
	ClashGlobal ID = "clash-global"
	// ClashLAN 管理 Clash 单机 LAN 覆写。
	ClashLAN ID = "clash-lan"
	// ShellEnv 管理普通 shell 环境变量。
	ShellEnv ID = "shell-env"
	// ShellEnvSecret 管理 age 加密的 shell 环境变量。
	ShellEnvSecret ID = "shell-env-secret"
)

const (
	fieldUnitPrefix  = "field:"
	fileUnitPrefix   = "file:"
	secretUnitPrefix = "secret:"
)

// Validate 拒绝路径、大小写漂移和不稳定目标 ID。
func (id ID) Validate() error {
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
	return UnitID(fieldUnitPrefix + encodeTOMLPath(parts))
}

// FileUnitID 构造完整文件单位 ID。
func FileUnitID(name string) UnitID {
	return UnitID(fileUnitPrefix + name)
}

// Validate 检查单位前缀和稳定路径字符。
func (id UnitID) Validate() error {
	value := string(id)
	if strings.HasPrefix(value, fieldUnitPrefix) {
		if _, ok := fieldUnitPath(id); !ok {
			return invalidInput()
		}
		return nil
	}
	body := ""
	switch {
	case strings.HasPrefix(value, fileUnitPrefix):
		body = strings.TrimPrefix(value, fileUnitPrefix)
	case strings.HasPrefix(value, secretUnitPrefix):
		body = strings.TrimPrefix(value, secretUnitPrefix)
	default:
		return invalidInput()
	}
	if body == "" || strings.ContainsAny(body, "/\\\x00") || strings.Contains(body, "..") {
		return invalidInput()
	}
	for _, r := range body {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return invalidInput()
		}
	}
	return nil
}

// fieldUnitPath 使用规范 TOML 键路径，避免将工具名中的点号或斜杠当作分隔符。
func fieldUnitPath(id UnitID) ([]string, bool) {
	body, ok := strings.CutPrefix(string(id), fieldUnitPrefix)
	if !ok || body == "" || strings.ContainsAny(body, "\r\n\x00") {
		return nil, false
	}
	parser := unstable.Parser{}
	parser.Reset([]byte(body + " = true\n"))
	if !parser.NextExpression() {
		return nil, false
	}
	expression := parser.Expression()
	if expression.Kind != unstable.KeyValue {
		return nil, false
	}
	parts := nodeKey(expression)
	if len(parts) == 0 || parser.NextExpression() || parser.Error() != nil || encodeTOMLPath(parts) != body {
		return nil, false
	}
	for _, part := range parts {
		if part == "" || strings.Contains(part, "..") {
			return nil, false
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return nil, false
			}
		}
	}
	return parts, true
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
