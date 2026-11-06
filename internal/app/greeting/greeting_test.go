package greeting

import (
	"context"
	"os"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/validation"
	"github.com/zhYou/my/internal/locales"
)

func TestGreetingDirectCallRejectsWhitespace(t *testing.T) {
	for _, name := range []string{"", "  ", "\t\n"} {
		_, err := Run(context.Background(), Options{Name: name})
		require.Error(t, err)
		require.Equal(t, fault.Usage, fault.KindOf(err))
	}
	result, err := New(validation.New()).Run(context.Background(), Options{Name: " Codex "})
	require.NoError(t, err)
	require.Equal(t, "Hello,  Codex !", result.Message)
}

func TestGreetingHonorsInjectedLocalizerAndRejectsIncompleteMessages(t *testing.T) {
	catalog, err := locales.LoadBundle()
	require.NoError(t, err)
	result, err := Run(t.Context(), Options{Name: "Ada", Localizer: catalog.Localizer("zh-CN")})
	require.NoError(t, err)
	require.Equal(t, "你好，Ada！", result.Message)

	cases := []struct {
		name     string
		contents string
		want     string
		fail     bool
	}{
		{"injected", "[\"greeting.message\"]\nother = \"Chosen {{.Name}}\"\n", "Chosen Ada", false},
		{"missing", "[\"unrelated\"]\nother = \"unused\"\n", "", true},
		{"missing parameter", "[\"greeting.message\"]\nother = \"partial {{.Missing}}\"\n", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := messages.NewBundle(fstest.MapFS{"active.en.toml": {Data: []byte(tc.contents)}})
			require.NoError(t, err)
			result, err := Run(t.Context(), Options{Name: "Ada", Localizer: bundle.Localizer("en")})
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, result.Message)
		})
	}
}

func TestGreetingConcurrentLanguagesRemainIsolated(t *testing.T) {
	bundle, err := locales.LoadBundle()
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 30 {
		for _, tc := range []struct {
			lang string
			want string
		}{{"en", "Hello, Ada!"}, {"zh-CN", "你好，Ada！"}} {
			wg.Go(func() {
				result, err := Run(context.Background(), Options{Name: "Ada", Localizer: bundle.Localizer(tc.lang)})
				if err != nil || result.Message != tc.want {
					t.Errorf("%s: result=%q err=%v", tc.lang, result.Message, err)
				}
			})
		}
	}
	wg.Wait()
}

// 由 test/integration 的覆盖构建测试运行，替换真实内嵌目录后验证调用边界。
func TestCorruptCatalogProcess(t *testing.T) {
	if os.Getenv("TEST_CORRUPT_I18N_CATALOG") != "1" {
		t.Skip("仅由损坏目录子进程执行")
	}
	result, err := Run(t.Context(), Options{Name: "Ada"})
	require.Error(t, err)
	require.Empty(t, result.Message)
	bundle, err := messages.NewBundle(
		fstest.MapFS{"active.en.toml": {Data: []byte("[\"greeting.message\"]\nother = \"Injected {{.Name}}\"\n")}},
	)
	require.NoError(t, err)
	result, err = Run(t.Context(), Options{Name: "Ada", Localizer: bundle.Localizer("en")})
	require.NoError(t, err)
	require.Equal(t, "Injected Ada", result.Message)
}
