package paths

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformDirectories(t *testing.T) {
	for _, tc := range []struct {
		os   string
		env  map[string]string
		want Directories
	}{
		{"linux", map[string]string{"HOME": "/home/u"}, Directories{"/home/u/.config/tool", "/home/u/.local/share/tool", "/home/u/.local/state/tool", "/home/u/.cache/tool"}},
		{"linux", map[string]string{"HOME": "/u", "XDG_CONFIG_HOME": "/cfg", "XDG_DATA_HOME": "/data", "XDG_STATE_HOME": "/state", "XDG_CACHE_HOME": "/cache"}, Directories{"/cfg/tool", "/data/tool", "/state/tool", "/cache/tool"}},
		{"linux", map[string]string{"HOME": "/u", "XDG_CONFIG_HOME": "relative"}, Directories{"/u/.config/tool", "/u/.local/share/tool", "/u/.local/state/tool", "/u/.cache/tool"}},
		{"darwin", map[string]string{"HOME": "/Users/u"}, Directories{"/Users/u/Library/Application Support/tool", "/Users/u/Library/Application Support/tool/data", "/Users/u/Library/Application Support/tool/state", "/Users/u/Library/Caches/tool"}},
		{"windows", map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`, "LOCALAPPDATA": `C:\Users\u\AppData\Local`}, Directories{`C:\Users\u\AppData\Roaming\tool`, `C:\Users\u\AppData\Local\tool\data`, `C:\Users\u\AppData\Local\tool\state`, `C:\Users\u\AppData\Local\tool\cache`}},
		{"linux", map[string]string{"TOOL_HOME": "/isolated"}, Directories{"/isolated", "/isolated/data", "/isolated/state", "/isolated/cache"}},
	} {
		t.Run(tc.os, func(t *testing.T) {
			dirs, err := Resolve(Options{
				AppName: "tool",
				GOOS:    tc.os,
				LookupEnv: func(k string) (string, bool) {
					v, ok := tc.env[k]
					return v, ok
				},
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, dirs)
		})
	}
}

func TestInvalidHomeAndMissingHome(t *testing.T) {
	for _, env := range []map[string]string{{"TOOL_HOME": "relative"}, {"HOME": "relative"}, {}} {
		_, err := Resolve(Options{
			AppName: "tool",
			GOOS:    "linux",
			LookupEnv: func(k string) (string, bool) {
				v, ok := env[k]
				return v, ok
			},
		})
		require.Error(t, err)
	}
}

func TestWindowsUNCAndAbsentEnvironment(t *testing.T) {
	dirs, err := Resolve(Options{
		AppName: "tool",
		GOOS:    "windows",
		LookupEnv: func(k string) (string, bool) {
			if k == "TOOL_HOME" {
				return `\\server\share\tool`, true
			}
			return "ignored", false
		},
	})
	require.NoError(t, err)
	require.Equal(t, `\\server\share\tool`, dirs.Config)
	dirs, err = Resolve(Options{
		AppName: "tool",
		GOOS:    "linux",
		LookupEnv: func(k string) (string, bool) {
			if k == "HOME" {
				return "/home/u", true
			}
			return "ignored", false
		},
	})
	require.NoError(t, err)
	require.Equal(t, "/home/u/.config/tool", dirs.Config)
}
