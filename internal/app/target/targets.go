package target

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

const maxTargetFileSize = 1 << 20

// FileFormat 声明目标内容的校验和编辑格式。
type FileFormat string

const (
	// FormatTOMLFields 表示按声明字段读取和修改 TOML。
	FormatTOMLFields FileFormat = "toml-fields"
	// FormatTOML 表示完整 TOML 文件。
	FormatTOML FileFormat = "toml"
	// FormatJSON 表示完整 JSON 文件。
	FormatJSON FileFormat = "json"
	// FormatYAML 表示完整 YAML 文件。
	FormatYAML FileFormat = "yaml"
	// FormatText 表示完整 UTF-8 文本文件。
	FormatText FileFormat = "text"
	// FormatAgeFile 表示完整 age 密文对应的私密文件。
	FormatAgeFile FileFormat = "age-file"
)

const (
	codexPermissionsTable      = "permissions"
	codexDevelopmentPermission = "development"
)

var codexRuleFiles = []string{
	"git.rules",
	"jvm.rules",
	"go.rules",
	"node.rules",
	"python.rules",
	"native.rules",
	"dotnet.rules",
	"mise.rules",
	"rtk.rules",
}

// File 描述目标中的一个本机文件。
type File struct {
	Name         string
	RelativePath string
	Format       FileFormat
	Required     bool
}

// Definition 是 CLI、同步和适配器共享的静态目标声明。
type Definition struct {
	ID                  ID
	Kind                UnitKind
	Secret              bool
	CaptureAll          bool
	RemoteAuthoritative bool
	DefaultCurrent      bool
	Files               []File
}

// Snapshot 是目标在当前机器上的单位和默认范围。
type Snapshot struct {
	Target ID
	Scope  MachineScope
	Units  map[UnitID]UnitValue
}

// DefaultDefinitions 返回稳定的内建目标顺序。
func DefaultDefinitions() []Definition {
	return []Definition{
		{
			ID:         CodexSettings,
			Kind:       UnitField,
			CaptureAll: true,
			Files: []File{
				{Name: "config.toml", RelativePath: ".codex/config.toml", Format: FormatTOMLFields, Required: true},
			},
		},
		{
			ID:         CodexRules,
			Kind:       UnitFile,
			CaptureAll: true,
			Files:      codexRuleTargetFiles(),
		},
		{
			ID:         CodexDeepSeek,
			Kind:       UnitFile,
			CaptureAll: true,
			Files: []File{
				{
					Name:         "deepseek.config.toml",
					RelativePath: ".codex/deepseek.config.toml",
					Format:       FormatTOML,
					Required:     true,
				},
				{
					Name:         "deepseek.models.json",
					RelativePath: ".codex/deepseek.models.json",
					Format:       FormatJSON,
					Required:     true,
				},
			},
		},
		{
			ID:   CodexAuth,
			Kind: UnitSecret,
			Files: []File{
				{Name: "auth.json", RelativePath: ".codex/auth.json", Format: FormatAgeFile, Required: true},
			},
			Secret: true,
		},
		{
			ID:         Mise,
			Kind:       UnitField,
			CaptureAll: true,
			Files: []File{
				{
					Name:         "config.toml",
					RelativePath: ".config/mise/config.toml",
					Format:       FormatTOMLFields,
					Required:     true,
				},
			},
		},
		{
			ID:         Topgrade,
			Kind:       UnitFile,
			CaptureAll: true,
			Files: []File{
				{Name: "topgrade.toml", RelativePath: ".config/topgrade.toml", Format: FormatTOML, Required: true},
			},
		},
		{
			ID:         ClashGlobal,
			Kind:       UnitFile,
			CaptureAll: true,
			Files: []File{
				{
					Name:         "global.js",
					RelativePath: ".config/clash-verge/zeus-global-extend.js",
					Format:       FormatText,
					Required:     true,
				},
			},
		},
		{
			ID:             ClashLAN,
			Kind:           UnitFile,
			CaptureAll:     true,
			DefaultCurrent: true,
			Files: []File{
				{
					Name:         "lan.yaml",
					RelativePath: ".config/clash-verge/zeus-canonical-lan-merge.yaml",
					Format:       FormatYAML,
					Required:     true,
				},
			},
		},
		{
			ID:                  ShellEnv,
			Kind:                UnitField,
			RemoteAuthoritative: true,
		},
		{
			ID:                  ShellEnvSecret,
			Kind:                UnitSecret,
			Secret:              true,
			RemoteAuthoritative: true,
		},
	}
}

func codexRuleTargetFiles() []File {
	result := make([]File, 0, len(codexRuleFiles))
	for _, name := range codexRuleFiles {
		result = append(result, File{
			Name:         name,
			RelativePath: ".codex/rules/" + name,
			Format:       FormatText,
		})
	}
	return result
}

func targetsByID() map[ID]Definition {
	targets := DefaultDefinitions()
	result := make(map[ID]Definition, len(targets))
	for _, target := range targets {
		result[target.ID] = target
	}
	return result
}

// Validate 检查目标声明的内部一致性。
func (d Definition) Validate() error {
	if err := d.ID.Validate(); err != nil {
		return err
	}
	if d.Kind != UnitField && d.Kind != UnitFile && d.Kind != UnitSecret {
		return invalidInput()
	}
	if d.RemoteAuthoritative && len(d.Files) != 0 ||
		!d.RemoteAuthoritative && len(d.Files) == 0 {
		return invalidInput()
	}
	if d.Secret != (d.Kind == UnitSecret) {
		return invalidInput()
	}
	seen := map[string]struct{}{}
	for _, file := range d.Files {
		if file.Name == "" ||
			file.RelativePath == "" ||
			filepath.IsAbs(filepath.FromSlash(file.RelativePath)) ||
			strings.Contains(file.RelativePath, "..") ||
			strings.Contains(file.RelativePath, "\\") {
			return invalidInput()
		}
		if _, duplicate := seen[file.Name]; duplicate {
			return invalidInput()
		}
		seen[file.Name] = struct{}{}
	}
	return nil
}

// DefaultScope 根据目标规则和当前机器构造默认适用范围。
func (d Definition) DefaultScope(machineID string) MachineScope {
	if d.DefaultCurrent {
		return MachineScope{IDs: []string{machineID}}
	}
	return MachineScope{All: true}
}

// ReadLocal 读取当前机器上的目标单位；返回内容只用于本次调用。
func (d Definition) ReadLocal(home, goos string) (Snapshot, error) {
	if err := d.Validate(); err != nil {
		return Snapshot{}, err
	}
	if d.RemoteAuthoritative {
		return Snapshot{}, problem(codeInvalidInput, phaseValidate, nil)
	}
	if !filepath.IsAbs(home) {
		return Snapshot{}, invalidInput()
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	snapshot := Snapshot{
		Target: d.ID,
		Scope:  MachineScope{All: true},
		Units:  map[UnitID]UnitValue{},
	}
	if d.DefaultCurrent {
		snapshot.Scope = MachineScope{}
	}
	if d.ID == CodexRules {
		if err := detectLegacyRules(filepath.Join(home, ".codex", "rules")); err != nil {
			return Snapshot{}, err
		}
	}
	for _, file := range d.Files {
		data, err := readTargetFile(filepath.Join(home, filepath.FromSlash(file.RelativePath)), file.Required)
		if err != nil {
			return Snapshot{}, err
		}
		if data == nil {
			continue
		}
		if err := validateTargetData(file, data); err != nil {
			return Snapshot{}, err
		}
		if file.Format == FormatTOMLFields {
			units, err := d.readFieldUnits(data, goos)
			if err != nil {
				return Snapshot{}, err
			}
			maps.Copy(snapshot.Units, units)
			continue
		}
		unit := FileUnitID(file.Name)
		if d.Kind == UnitSecret {
			unit = SecretUnitID
		}
		snapshot.Units[unit] = UnitValueFromBytes(data)
	}
	return snapshot, nil
}

func (d Definition) readFieldUnits(data []byte, goos string) (map[UnitID]UnitValue, error) {
	switch d.ID {
	case Mise:
		return readMiseUnits(data)
	case CodexSettings:
		return readCodexSettingUnits(data, goos)
	default:
		return nil, invalidInput()
	}
}

func readMiseUnits(data []byte) (map[UnitID]UnitValue, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return nil, invalidTOML(err)
	}
	result := map[UnitID]UnitValue{}
	if tools, ok := document[miseToolsTable].(map[string]any); ok {
		for _, name := range SortedTOMLKeys(tools) {
			value, err := TOMLValue(data, []string{miseToolsTable, name})
			if err != nil {
				return nil, err
			}
			result[FieldUnitID(miseToolsTable, name)] = value
		}
	}
	for _, name := range []string{"http_timeout", "url_replacements"} {
		value, err := TOMLValue(data, []string{miseSettingsTable, name})
		if err != nil {
			return nil, err
		}
		result[FieldUnitID(miseSettingsTable, name)] = value
	}
	return result, nil
}

func readCodexSettingUnits(data []byte, goos string) (map[UnitID]UnitValue, error) {
	result := map[UnitID]UnitValue{}
	for _, path := range [][]string{
		{"default_permissions"},
		{codexPermissionsTable, codexDevelopmentPermission, "extends"},
		{codexPermissionsTable, codexDevelopmentPermission, "network", "enabled"},
		{"features", "network_proxy"},
	} {
		value, err := TOMLValue(data, path)
		if err != nil {
			return nil, err
		}
		result[FieldUnitID(path...)] = value
	}
	for id, localPath := range codexFilesystemUnits(goos) {
		value, err := TOMLValue(
			data,
			[]string{codexPermissionsTable, codexDevelopmentPermission, codexFilesystemTable, localPath},
		)
		if err != nil {
			return nil, err
		}
		result[FieldUnitID("filesystem", id)] = value
	}
	if hasLegacySandbox(data) {
		return nil, problem(codeLegacyConflict, phaseValidate, nil)
	}
	return result, nil
}

func hasLegacySandbox(data []byte) bool {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return false
	}
	_, mode := document["sandbox_mode"]
	_, roots := document["sandbox_workspace_write"]
	return mode || roots
}

func validateTargetData(file File, data []byte) error {
	if !utf8.Valid(data) {
		return invalidInput()
	}
	switch file.Format {
	case FormatTOML, FormatTOMLFields:
		var document map[string]any
		if err := toml.Unmarshal(data, &document); err != nil {
			return invalidTOML(err)
		}
	case FormatJSON, FormatAgeFile:
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil || value == nil {
			return invalidInput()
		}
	case FormatYAML:
		var value any
		if err := yaml.Unmarshal(data, &value); err != nil || value == nil {
			return invalidInput()
		}
	case FormatText:
	default:
		return invalidInput()
	}
	return nil
}

func readTargetFile(path string, required bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return nil, nil
	}
	if err != nil {
		return nil, problem(codeStateInvalid, phaseStateRead, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxTargetFileSize {
		return nil, problem(codeStateInvalid, phaseStateRead, nil)
	}
	file, err := os.Open(path) // #nosec G304 -- 路径来自固定目标声明并已拒绝符号链接。
	if err != nil {
		return nil, problem(codeStateInvalid, phaseStateRead, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxTargetFileSize+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxTargetFileSize {
		return nil, problem(codeStateInvalid, phaseStateRead, errors.Join(readErr, closeErr))
	}
	return data, nil
}

func detectLegacyRules(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return problem(codeStateInvalid, phaseStateRead, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "git-rtk.rules" || strings.HasPrefix(name, "my-") && strings.HasSuffix(name, ".rules") {
			return problem(codeLegacyConflict, phaseValidate, nil)
		}
	}
	return nil
}

//nolint:goconst // 路径映射按平台分组，重复的稳定键用于表达不同平台的同一逻辑目录。
func codexFilesystemUnits(goos string) map[string]string {
	switch goos {
	case platformLinux:
		return map[string]string{
			"gradle": "~/.gradle", "m2": "~/.m2", "ivy2": "~/.ivy2", "sbt": "~/.sbt",
			"coursier": "~/.coursier", "coursier-cache": "~/.cache/coursier", "jbang": "~/.jbang",
			"konan": "~/.konan", "java": "~/.java",
			"go": "~/go", "go-build-cache": "~/.cache/go-build", "gopls-cache": "~/.cache/gopls",
			"golangci-lint-cache": "~/.cache/golangci-lint", "govulncheck-cache": "~/.cache/govulncheck",
			"go-telemetry": "~/.config/go/telemetry",
			"npm-cache":    "~/.npm", "pnpm-data": "~/.local/share/pnpm", "pnpm-cache": "~/.cache/pnpm",
			"yarn-cache": "~/.cache/yarn", "yarn-data": "~/.yarn", "node-cache": "~/.cache/node",
			"node-corepack": "~/.cache/node/corepack",
			"uv-cache":      "~/.cache/uv", "uv-data": "~/.local/share/uv", "pip-cache": "~/.cache/pip",
			"poetry-cache": "~/.cache/pypoetry", "pdm-cache": "~/.cache/pdm", "pipx-data": "~/.local/pipx",
			"mise-bin": "~/.local/bin", "mise-lib": "~/.local/lib", "mise-cache": "~/.cache/mise",
			"mise-data": "~/.local/share/mise", "mise-state": "~/.local/state/mise",
			"serena": "~/.serena", "gitnexus": "~/.gitnexus",
			"rtk-config": "~/.config/rtk", "rtk-data": "~/.local/share/rtk",
			"ccache-cache": "~/.cache/ccache", "sccache-cache": "~/.cache/sccache",
			"conan": "~/.conan", "conan2": "~/.conan2", "vcpkg-cache": "~/.cache/vcpkg",
			"cmake": "~/.cmake", "meson-cache": "~/.cache/meson",
			"dotnet": "~/.dotnet", "nuget": "~/.nuget", "nuget-data": "~/.local/share/NuGet",
			"nuget-cache": "~/.cache/NuGet",
		}
	case platformDarwin:
		return map[string]string{
			"gradle": "~/.gradle", "m2": "~/.m2", "ivy2": "~/.ivy2", "sbt": "~/.sbt",
			"coursier": "~/.coursier", "jbang": "~/.jbang", "konan": "~/.konan", "java": "~/.java",
			"go": "~/go", "npm-cache": "~/.npm", "yarn-data": "~/.yarn", "pipx-data": "~/.local/pipx",
			"mise-bin": "~/.local/bin", "serena": "~/.serena", "gitnexus": "~/.gitnexus",
			"rtk-data": "~/Library/Application Support/rtk", "dotnet": "~/.dotnet", "nuget": "~/.nuget",
			"conan": "~/.conan", "conan2": "~/.conan2", "cmake": "~/.cmake",
			"coursier-cache": "~/Library/Caches/Coursier", "ivy2-cache": "~/Library/Caches/Ivy2",
			"sbt-cache": "~/Library/Caches/sbt", "go-build-cache": "~/Library/Caches/go-build",
			"go-build-cache-local": "~/.cache/go-build", "go-telemetry": "~/.config/go/telemetry",
			"node-cache": "~/Library/Caches/node", "node-corepack": "~/Library/Caches/node/corepack",
			"pnpm-data": "~/Library/pnpm", "pnpm-cache": "~/Library/Caches/pnpm",
			"yarn-cache": "~/Library/Caches/Yarn", "uv-cache": "~/Library/Caches/uv",
			"pip-cache": "~/Library/Caches/pip", "poetry-cache": "~/Library/Caches/pypoetry",
			"pdm-cache": "~/Library/Caches/pdm", "mise-cache": "~/Library/Caches/mise",
			"mise-data": "~/Library/Application Support/mise", "mise-data-local": "~/.local/share/mise",
			"mise-state": "~/.local/state/mise", "ccache-cache": "~/Library/Caches/ccache",
			"sccache-cache": "~/Library/Caches/sccache", "vcpkg-cache": "~/Library/Caches/vcpkg",
			"meson-cache": "~/Library/Caches/meson", "nuget-cache": "~/Library/Caches/NuGet",
			"nuget-data": "~/Library/Application Support/NuGet", "nuget-data-local": "~/.local/share/NuGet",
			"uv-cache-local": "~/.cache/uv", "mise-cache-local": "~/.cache/mise",
		}
	case platformWindows:
		return map[string]string{
			"gradle": "~/.gradle", "m2": "~/.m2", "ivy2": "~/.ivy2", "sbt": "~/.sbt",
			"coursier": "~/.coursier", "jbang": "~/.jbang", "konan": "~/.konan", "java": "~/.java",
			"go": "~/go", "npm-cache": "~/.npm", "yarn-data": "~/.yarn", "dotnet": "~/.dotnet",
			"nuget": "~/.nuget", "pipx-data": "~/pipx", "serena": "~/.serena", "gitnexus": "~/.gitnexus",
			"rtk-config": "~/AppData/Roaming/rtk", "rtk-data": "~/AppData/Local/rtk",
			"conan": "~/.conan", "conan2": "~/.conan2", "cmake": "~/.cmake",
			"go-build-cache": "~/AppData/Local/go-build", "go-telemetry": "~/AppData/Roaming/go/telemetry",
			"npm-cache-local": "~/AppData/Local/npm-cache", "pnpm-cache": "~/AppData/Local/pnpm",
			"yarn-cache": "~/AppData/Local/Yarn/Cache", "node-corepack": "~/AppData/Local/node/corepack",
			"uv-cache": "~/AppData/Local/uv", "pip-cache": "~/AppData/Local/pip/Cache",
			"poetry-cache": "~/AppData/Roaming/pypoetry", "pdm-cache": "~/AppData/Local/pdm",
			"pipx-cache": "~/AppData/Local/pipx", "mise-cache": "~/AppData/Local/mise",
			"ccache-cache": "~/AppData/Local/ccache", "sccache-cache": "~/AppData/Local/sccache",
			"vcpkg-cache": "~/AppData/Local/vcpkg", "meson-cache": "~/AppData/Local/meson",
			"nuget-scratch": "~/AppData/Local/Temp/NuGetScratchroot",
			"nuget-cache":   "~/AppData/Local/NuGet", "nuget-data": "~/AppData/Roaming/NuGet",
			"uv-cache-local": "~/.cache/uv", "pip-cache-local": "~/.cache/pip",
			"mise-cache-local": "~/.cache/mise", "mise-data": "~/.local/share/mise",
		}
	default:
		return nil
	}
}
