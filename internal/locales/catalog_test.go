package locales

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/messages"
)

func TestEmbeddedCatalogAndSharedBundle(t *testing.T) {
	const callers = 24
	bundles := make([]*messages.Bundle, callers)
	errors := make([]error, callers)
	var wg sync.WaitGroup
	for i := range bundles {
		wg.Go(func() {
			bundles[i], errors[i] = LoadBundle()
		})
	}
	wg.Wait()
	for i := range bundles {
		require.NoError(t, errors[i])
		require.Same(t, bundles[0], bundles[i])
	}
	translations := make([]string, callers)
	for i := range translations {
		wg.Go(func() {
			lang := "en"
			if i%2 != 0 {
				lang = "zh-CN"
			}
			translations[i], errors[i] = bundles[i].Localizer(lang).
				Format("greeting.message", messages.FormatOptions{Params: map[string]any{"Name": "Ada"}})
		})
	}
	wg.Wait()
	for i, text := range translations {
		require.NoError(t, errors[i])
		if i%2 == 0 {
			require.Equal(t, "Hello, Ada!", text)
		} else {
			require.Equal(t, "你好，Ada！", text)
		}
	}
	en := bundles[0].Localizer("en")
	zh := bundles[0].Localizer("zh-CN")
	greeting, err := en.Format("greeting.message", messages.FormatOptions{Params: map[string]any{"Name": "Ada"}})
	require.NoError(t, err)
	require.Equal(t, "Hello, Ada!", greeting)
	greeting, err = zh.Format("greeting.message", messages.FormatOptions{Params: map[string]any{"Name": "Ada"}})
	require.NoError(t, err)
	require.Equal(t, "你好，Ada！", greeting)
	one, err := en.Format("cli.shell_count", messages.FormatOptions{
		Params:      map[string]any{"Count": 1},
		PluralCount: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "Expected at most one shell name, received 1 argument", one)
	many, err := en.Format("cli.shell_count", messages.FormatOptions{
		Params:      map[string]any{"Count": 2},
		PluralCount: 2,
	})
	require.NoError(t, err)
	require.Equal(t, "Expected at most one shell name, received 2 arguments", many)
}
