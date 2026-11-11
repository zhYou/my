package scaffold

import (
	"context"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/afero"
)

// TemplateModule 是原始模板的模块路径。
const TemplateModule = "github.com/zhYou/go-cli-starter"

// TemplateName 是原始模板的应用标识。
const TemplateName = "go-cli-starter"

// 模板只管理这些文件中的应用标识。
// 初始化项目时保留本实现及其测试夹具，避免改动初始化基准。
var managedFiles = []string{
	"internal/app/cli/output.go",
	"internal/app/cli/diagnostic.go",
	"internal/app/cli/diagnostic_test.go",
	"internal/app/config/load_test.go",
	"internal/base/telemetry/diagnostic.go",
	".serena/project.yml",
	"tools/mcp/client.go",
	"tools/mcp/fixture.go",
	"tools/mcp/graphify_fixture.go",
	".golangci.yml",
	"cmd/" + TemplateName + "/doc.go",
	"internal/app/cli/completion_protocol.go",
	"internal/app/cli/completion_protocol_test.go",
	"internal/app/cli/doctor.go",
	"internal/app/cli/doctor_test.go",
	".github/workflows/ci.yml",
	".github/workflows/release.yml",
	"tools/internal/acceptance/acceptance.go",
	"tools/release/constants.go",
	"tools/release/release_install.go",
	"tools/doctor/doctor.go",
	"tools/doctor/doctor_test.go",
	"tools/lint/rules.go",
	"internal/app/cli/fuzz_test.go",
	"internal/app/cli/bindings.go",
	"internal/app/cli/version.go",
	"internal/locales/catalog.go",
	"internal/locales/catalog_test.go",
	"tools/lint/config.go",
	"tools/lint/config_test.go",
	"tools/lint/i18n.go",
	"tools/lint/i18n_test.go",
	"internal/command/batch.go",
	"internal/command/errors.go",
	"internal/command/process.go",
	"internal/command/io_windows.go",
	"internal/command/observability.go",
	"internal/command/observability_test.go",
	"internal/base/logging/logger_external_test.go",
	"internal/base/telemetry/environment_linux_test.go",
	"internal/base/diagnostics/retention_test.go",
	"internal/app/cli/cli_test.go",
	"internal/app/session/session.go",
	"internal/app/session/session_test.go",
	"internal/base/diagnostics/errors.go",
	"internal/base/logging/errors.go",
	"internal/base/logging/logging.go",
	"internal/base/telemetry/errors.go",
	"internal/base/telemetry/session.go",
	"internal/app/cli/language.go",
	"internal/app/cli/language_test.go",
	"internal/app/cli/help.go",
	"internal/app/cli/discovery.go",
	"internal/base/messages/messages.go",
	"internal/base/messages/messages_test.go",
	"internal/base/paths/paths.go",
	"internal/base/paths/paths_test.go",

	moduleFile,
	"cmd/" + TemplateName + "/main.go",
	"cmd/" + TemplateName + "/main_test.go",
	"internal/app/cli/cli.go",
	"internal/app/cli/errors.go",
	"test/integration/integration_test.go",
	"test/integration/testdata/process/language.txtar",
	"test/integration/testdata/process/configuration.txtar",
	"test/integration/testdata/process/discovery.txtar",
	"test/integration/testdata/process/usage.txtar",
	"tools/contract/contracts_test.go",
	"tools/contract/contracts.go",
	"tools/template/template_test.go",
	"tools/mcp/mcp.go",
	"internal/app/cli/results.go",
	"internal/app/cli/dependencies.go",
	"internal/app/cli/greet.go",
	"internal/app/cli/dependencies_test.go",
	"internal/app/config/load.go",
	"internal/app/greeting/greeting.go",
	"internal/app/greeting/greeting_test.go",
	"internal/base/validation/validation.go",
	"internal/app/cli/output_test.go",
	"internal/app/cli/errors_test.go",
	"internal/base/fault/error_test.go",
	"tools/template/init.go",
	"tools/build/build.go",
	"tools/template/template.go",
	"mise.toml",
	".goreleaser.yaml",
	"README.md",
	"AGENTS.md",
	"configs/config.example.toml",
	"docs/development.md",
	"docs/template-upgrades.md",
	"docs/design-guidelines.md",
	"internal/app/cli/benchmark_test.go",
	"internal/app/cli/bindings_test.go",
	"internal/app/cli/help_test.go",
	"internal/app/cli/helpers_test.go",
	"internal/app/cli/schema_test.go",
	"internal/app/cli/version_test.go",
	"internal/base/logging/events_test.go",
	"internal/base/logging/logger_test.go",
	"internal/base/logging/logging_test.go",
	"internal/base/telemetry/session_test.go",
	"internal/command/errors_test.go",
	"internal/command/helpers_test.go",
	"test/integration/helpers_test.go",
	"tools/internal/acceptance/acceptance_test.go",
	"tools/internal/fixture/git.go",
	"tools/lint/main.go",
	"tools/lint/versions.go",
	"tools/lint/versions_test.go",
	"tools/lint/text_test.go",
	"tools/release/release_install_test.go",
}

var (
	validName   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	validModule = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
)

type change struct {
	path string
	data []byte
	mode os.FileMode
}

// Initialize 修改原始模板的标识；标识已匹配时不做修改。
func Initialize(root, module, name string) ([]string, error) {
	return (&Initializer{}).Initialize(context.Background(), root, module, name)
}

// Identity 读取模块路径和可执行文件名，不执行项目代码。
func Identity(root string) (string, string, error) {
	return (&Initializer{FS: afero.NewOsFs()}).identity(root)
}

// Initialize 以事务方式重命名模板，并返回变更文件列表。
func (i *Initializer) Initialize(ctx context.Context, root, module, name string) (_ []string, resultErr error) {
	working := i.defaults()
	i = &working
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateCommandName(name); err != nil {
		return nil, err
	}
	if !validModule.MatchString(module) || strings.HasSuffix(module, "/") {
		return nil, errors.New("无效的 Go 模块路径")
	}
	for part := range strings.SplitSeq(module, "/") {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("无效的 Go 模块路径片段")
		}
	}
	if err := i.validateModule(ctx, module); err != nil {
		return nil, err
	}
	var err error
	if _, ok := i.FS.(*afero.OsFs); ok {
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return nil, err
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lockPath, err := i.safePath(root, ".template-init.lock")
	if err != nil {
		return nil, err
	}
	unlock, err := i.Lock(lockPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, unlock())
	}()
	if err := i.recover(root, module, name); err != nil {
		return nil, err
	}
	currentModule, currentName, err := i.identity(root)
	if err != nil {
		return nil, err
	}
	if currentModule == module && currentName == name {
		return []string{}, nil
	}
	if currentModule != TemplateModule || currentName != TemplateName {
		return nil, errors.New("项目已初始化，拒绝替换现有标识")
	}
	replacer := strings.NewReplacer(
		TemplateModule,
		module,
		TemplateName,
		name,
		"GO_CLI_STARTER",
		strings.ToUpper(strings.ReplaceAll(name, "-", "_")),
	)
	var changes []change
	// 写入文件前先检查所有目标路径。
	for _, relative := range managedFiles {
		path, err := i.safePath(root, relative)
		if err != nil {
			return nil, err
		}
		info, err := i.FS.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("拒绝修改非常规文件 %q", relative)
		}
		data, err := afero.ReadFile(i.FS, path)
		if err != nil {
			return nil, err
		}
		updated := replacer.Replace(string(data))
		if updated != string(data) {
			updatedData := []byte(updated)
			if filepath.Ext(path) == ".go" {
				updatedData, err = format.Source(updatedData)
				if err != nil {
					return nil, fmt.Errorf("格式化重命名后的文件 %q 失败：%w", relative, err)
				}
			}
			changes = append(changes, change{path, updatedData, info.Mode().Perm()})
		}
	}
	origin, err := i.originChange(ctx, root, module, name)
	if err != nil {
		return nil, err
	}
	if origin != nil {
		changes = append(changes, *origin)
	}
	if len(changes) == 0 {
		return []string{}, nil
	}
	var move *commandMove
	if name != TemplateName {
		move = &commandMove{From: "cmd/" + TemplateName, To: "cmd/" + name}
	}
	return i.apply(ctx, root, module, name, changes, move)
}

func (i *Initializer) identity(root string) (string, string, error) {
	project, err := i.inspect(root)
	return project.Module, project.Name, err
}
