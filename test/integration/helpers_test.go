package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/zhYou/my/internal/base/paths"
)

// isolatedAppEnvironment 保留构建环境，同时隔离应用配置和状态。
func isolatedAppEnvironment(t *testing.T, name string) []string {
	t.Helper()
	prefix := paths.EnvPrefix(name) + "_"
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
			environment = append(environment, entry)
		}
	}
	return append(environment, prefix+"HOME="+t.TempDir())
}
