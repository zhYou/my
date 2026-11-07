package datarepo

import (
	"strings"
	"time"
	"unicode"
)

// Config 是数据仓访问的有类型配置，Repository 仅在使用业务时必需。
type Config struct {
	Repository     string        `mapstructure:"repository"`
	Branch         string        `mapstructure:"branch"`
	RequestTimeout time.Duration `mapstructure:"request_timeout" validate:"gt=0"`
}

// Defaults 返回业务默认值，不选择用户的数据仓。
func Defaults() Config {
	return Config{Branch: "main", RequestTimeout: 30 * time.Second}
}

// Validate 校验可执行的数据仓配置。
func (c Config) Validate() error {
	if c.Repository == "" {
		return failure("data.configuration", "configuration", nil, []string{}, recoverConfig)
	}
	return c.ValidateOptional()
}

// ValidateOptional 允许尚未指定数据仓，其他显式配置仍须合法。
func (c Config) ValidateOptional() error {
	parts := strings.Split(c.Repository, "/")
	validRepo := c.Repository == "" || len(parts) == 2 && safeName(parts[0], false) && safeName(parts[1], true)
	if !validRepo || !validBranch(c.Branch) || c.RequestTimeout <= 0 {
		return failure("data.configuration", "configuration", nil, []string{}, recoverConfig)
	}
	return nil
}

func safeName(s string, repo bool) bool {
	if s == "" || s == "." || s == ".." || len(s) > 100 {
		return false
	}
	for _, r := range s {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' ||
			repo && (r == '_' || r == '.')
		if !valid {
			return false
		}
	}
	return true
}

func validBranch(s string) bool {
	if s == "" || s == "@" || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "/") ||
		strings.HasSuffix(s, ".") ||
		strings.Contains(s, "..") ||
		strings.Contains(s, "@{") ||
		strings.Contains(s, "//") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	for part := range strings.SplitSeq(s, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
