package config

import (
	"path/filepath"
)

// Secrets 声明秘密相关调用使用的仓外凭据位置。
type Secrets struct {
	IdentityFile string `mapstructure:"identity_file"`
}

// SecretsDefaults 不选择用户身份文件。
func SecretsDefaults() Secrets {
	return Secrets{}
}

// ValidateOptional 允许未配置，已配置时要求绝对路径。
func (s Secrets) ValidateOptional() error {
	if s.IdentityFile != "" && !filepath.IsAbs(s.IdentityFile) {
		return problem("invalid_identity_path", nil, nil)
	}
	return nil
}
