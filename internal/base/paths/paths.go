package paths

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zhYou/my/internal/base/fault"
)

// Directories 保存应用私有配置、数据、状态与缓存目录。
type Directories struct {
	Config string
	Data   string
	State  string
	Cache  string
}

// Options 声明本次调用的显式依赖与覆盖选项。
type Options struct {
	Layout    Layout
	AppName   string
	GOOS      string
	LookupEnv func(string) (string, bool)
}

// EnvPrefix 从应用名生成稳定的环境变量前缀。
func EnvPrefix(app string) string {
	return strings.ToUpper(strings.ReplaceAll(app, "-", "_"))
}

// Resolve 解析应用目录，不创建文件或修改环境。
func Resolve(opts Options) (Directories, error) {
	platform := opts.GOOS
	if platform == "" {
		platform = runtime.GOOS
	}
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	env := func(k string) string {
		v, ok := lookup(k)
		if !ok {
			return ""
		}
		return v
	}
	join := path.Join
	absolute := func(s string) bool {
		return strings.HasPrefix(s, "/")
	}
	if platform == platformWindows {
		join = func(parts ...string) string {
			for i := range parts {
				parts[i] = strings.ReplaceAll(parts[i], string(rune(92)), "/")
			}
			joined := path.Join(parts...)
			if strings.HasPrefix(parts[0], "//") {
				joined = "/" + joined
			}
			return strings.ReplaceAll(joined, "/", string(rune(92)))
		}
		absolute = func(s string) bool {
			normalized := strings.ReplaceAll(s, string(rune(92)), "/")
			if len(normalized) > 2 && normalized[1] == ':' && normalized[2] == '/' {
				return normalized[0] >= 'A' && normalized[0] <= 'Z' || normalized[0] >= 'a' && normalized[0] <= 'z'
			}
			if after, ok := strings.CutPrefix(normalized, "//"); ok {
				parts := strings.Split(after, "/")
				return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
			}
			return false
		}
	}
	fail := func(field string) (Directories, error) {
		return Directories{}, (fault.Definition{Code: "paths.absolute", Kind: fault.Configuration}).New(
			fault.WithParams(fault.Params{"Field": field}),
		)
	}
	if opts.AppName == "" || opts.AppName == "." || opts.AppName == ".." ||
		strings.ContainsAny(opts.AppName, "/"+string(rune(92))) {
		return fail("AppName")
	}
	homeKey := EnvPrefix(opts.AppName) + "_HOME"
	if home := env(homeKey); home != "" {
		if !absolute(home) {
			return fail(homeKey)
		}
		home = join(home)
		return Directories{home, join(home, dataDirectory), join(home, stateDirectory), join(home, cacheDirectory)}, nil
	}
	if opts.Layout == HomeLayout {
		key := homeEnvironment
		if platform == platformWindows {
			key = "USERPROFILE"
		}
		home := env(key)
		if !absolute(home) {
			return fail(key)
		}
		root := join(home, "."+opts.AppName)
		return Directories{root, join(root, dataDirectory), join(root, stateDirectory), join(root, cacheDirectory)}, nil
	}
	if opts.Layout != "" && opts.Layout != PlatformLayout {
		return Directories{}, (fault.Definition{Code: "paths.layout", Kind: fault.Configuration}).New()
	}
	if platform == platformWindows {
		roaming, local := env("APPDATA"), env("LOCALAPPDATA")
		if !absolute(roaming) {
			return fail("APPDATA")
		}
		if !absolute(local) {
			return fail("LOCALAPPDATA")
		}
		return Directories{
			join(roaming, opts.AppName),
			join(local, opts.AppName, dataDirectory),
			join(local, opts.AppName, stateDirectory),
			join(local, opts.AppName, cacheDirectory),
		}, nil
	}
	home := env(homeEnvironment)
	if platform == "darwin" {
		if !absolute(home) {
			return fail(homeEnvironment)
		}
		support := join(home, "Library", "Application Support", opts.AppName)
		return Directories{
			support,
			join(support, dataDirectory),
			join(support, stateDirectory),
			join(home, "Library", "Caches", opts.AppName),
		}, nil
	}
	roots := []struct {
		env    string
		suffix string
	}{{"XDG_CONFIG_HOME", ".config"}, {"XDG_DATA_HOME", ".local/share"}, {"XDG_STATE_HOME", ".local/state"}, {"XDG_CACHE_HOME", ".cache"}}
	values := make([]string, 4)
	for i, root := range roots {
		value := env(root.env)
		// XDG 规范要求忽略相对路径，回退到 HOME。
		if !absolute(value) {
			if !absolute(home) {
				return fail(homeEnvironment)
			}
			value = join(home, root.suffix)
		}
		values[i] = join(value, opts.AppName)
	}
	return Directories{values[0], values[1], values[2], values[3]}, nil
}

// ConfigFile 返回私有配置文件的完整路径。
func (d Directories) ConfigFile() string {
	return filepath.Join(d.Config, "config.toml")
}
